// fake-codex is an offline native-protocol fixture for the two-build upgrade
// acceptance test. Optional handoff mode scripts local fixture work and loopback
// AO calls; it never invokes a real model or external network API.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type object = map[string]any

func main() {
	if strings.HasPrefix(filepath.Base(os.Args[0]), "opencode") {
		runOpenCode()
		return
	}
	for len(os.Args) > 2 && os.Args[1] == "-c" {
		os.Args = append(os.Args[:1], os.Args[3:]...)
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("codex-cli 0.153.0")
		return
	}
	if len(os.Args) > 2 && os.Args[1] == "login" && (os.Args[2] == "status" || os.Args[2] == "--help") {
		fmt.Println("Logged in using an API key")
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "app-server" {
		fmt.Fprintln(os.Stderr, "upgrade fixture: unsupported command")
		os.Exit(2)
	}
	if len(os.Args) > 2 && os.Args[2] == "generate-json-schema" {
		for i, arg := range os.Args {
			if arg == "--out" && i+1 < len(os.Args) {
				must(os.WriteFile(filepath.Join(os.Args[i+1], "fixture.json"), []byte(`{"methods":["account/read","account/rateLimits/read"]}`), 0o600))
				return
			}
		}
		os.Exit(2)
	}
	root := os.Getenv("AO_UPGRADE_FIXTURE_DIR")
	if root == "" {
		fmt.Fprintln(os.Stderr, "upgrade fixture requires isolated state")
		os.Exit(2)
	}
	output := json.NewEncoder(os.Stdout)
	log, err := os.OpenFile(filepath.Join(root, fmt.Sprintf("provider-%d.jsonl", os.Getpid())), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	must(err)
	defer log.Close()
	journal := json.NewEncoder(log)
	must(journal.Encode(object{"pid": os.Getpid(), "method": "fixture/start"}))
	must(log.Sync())
	threadID := ""
	var turns []object
	var heldTurn object
	save := func() {
		b, e := json.Marshal(turns)
		must(e)
		must(os.WriteFile(filepath.Join(root, threadID+".json"), b, 0o600))
	}
	emit := func(method string, params any) {
		must(output.Encode(object{"method": method, "params": params}))
	}
	complete := func(turn object) {
		turn["status"] = "completed"
		items := turn["items"].([]object)
		answer := object{"id": turn["id"].(string) + "-answer", "type": "agentMessage", "text": "UPGRADE_FIXTURE_ACK " + turn["id"].(string)}
		turn["items"] = append(items, answer)
		save()
		emit("item/completed", object{"threadId": threadID, "turnId": turn["id"], "item": answer})
		emit("turn/completed", object{"threadId": threadID, "turn": turn})
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 65536), 4<<20)
	for scanner.Scan() {
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params object          `json:"params"`
		}
		must(json.Unmarshal(scanner.Bytes(), &frame))
		must(journal.Encode(object{"pid": os.Getpid(), "threadId": threadID, "method": frame.Method, "params": frame.Params}))
		must(log.Sync())
		if len(frame.ID) == 0 {
			continue
		}
		var result any = object{}
		switch frame.Method {
		case "initialize":
			result = object{"userAgent": "ao-upgrade-fixture"}
		case "account/read":
			result = object{"account": object{"type": "apiKey"}, "requiresOpenaiAuth": true}
		case "account/rateLimits/read":
			result = object{"rateLimits": object{"planType": "pro", "primary": object{"usedPercent": 0, "windowDurationMins": 300, "resetsAt": 2000000000}}}
		case "model/list":
			result = object{"data": []object{{"id": "upgrade-model", "model": "upgrade-model", "displayName": "Offline upgrade fixture", "isDefault": true, "defaultReasoningEffort": "high", "supportedReasoningEfforts": []object{{"reasoningEffort": "high"}}}}}
		case "thread/start":
			threadID = fmt.Sprintf("00000000-0000-4000-8000-%012d", os.Getpid())
			turns = []object{}
			save()
			result = object{"thread": object{"id": threadID}, "model": "upgrade-model", "reasoningEffort": "high"}
		case "thread/resume":
			threadID, _ = frame.Params["threadId"].(string)
			data, e := os.ReadFile(filepath.Join(root, threadID+".json"))
			must(e)
			must(json.Unmarshal(data, &turns))
			result = object{"thread": object{"id": threadID}, "model": "upgrade-model", "reasoningEffort": "high"}
		case "thread/read":
			result = object{"thread": object{"id": threadID, "turns": turns}}
		case "turn/start":
			input, _ := frame.Params["input"].([]any)
			text := ""
			for _, part := range input {
				if content, ok := part.(map[string]any); ok {
					if value, ok := content["text"].(string); ok {
						text += value
					}
				}
			}
			turn := object{"id": fmt.Sprintf("turn-%d", len(turns)+1), "status": "inProgress", "items": []object{{"id": fmt.Sprintf("input-%d", len(turns)+1), "type": "userMessage", "content": []object{{"type": "text", "text": text}}}}}
			turns = append(turns, turn)
			save()
			must(output.Encode(object{"id": frame.ID, "result": object{"turn": turn}}))
			emit("turn/started", object{"threadId": threadID, "turn": turn})
			if strings.Contains(text, "UPGRADE_HOLD_FOR_APPROVAL") {
				heldTurn = turn
				must(output.Encode(object{"id": "upgrade-permission", "method": "item/commandExecution/requestApproval", "params": object{"threadId": threadID, "turnId": turn["id"], "itemId": "upgrade-command", "command": "fixture-noop", "cwd": root, "availableDecisions": []string{"accept", "cancel"}}}))
			} else {
				complete(turn)
				runHandoff(text, threadID)
			}
			continue
		case "":
			if string(frame.ID) == `"upgrade-permission"` && heldTurn != nil {
				complete(heldTurn)
				heldTurn = nil
			}
			continue
		default:
			must(output.Encode(object{"id": frame.ID, "error": object{"code": -32601, "message": "offline upgrade fixture: unsupported " + frame.Method}}))
			continue
		}
		must(output.Encode(object{"id": frame.ID, "result": result}))
	}
	must(scanner.Err())
	// The offline provider has no descendants and exits on host pipe EOF. The
	// harness independently verifies that this PID is dead before replacement.
	must(journal.Encode(object{"pid": os.Getpid(), "threadId": threadID, "method": "fixture/eof"}))
	must(log.Sync())
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "upgrade fixture:", err)
		os.Exit(1)
	}
}

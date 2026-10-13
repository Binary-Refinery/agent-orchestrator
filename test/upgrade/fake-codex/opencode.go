package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runOpenCode is the actual OpenCode ACP wire boundary, with a tiny offline
// provider catalogue. Native AO model/effort validation still runs normally.
func runOpenCode() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	switch os.Args[1] {
	case "--version":
		fmt.Println("1.2.0")
		return
	case "auth":
		fmt.Println("1 credential (offline fixture)")
		return
	case "models":
		fmt.Println("fixture/worker\n{\"name\":\"Offline worker\",\"variants\":{\"high\":{}}}")
		return
	case "debug":
		fmt.Println(`{"model":"fixture/worker"}`)
		return
	case "acp":
	default:
		fmt.Fprintln(os.Stderr, "unsupported OpenCode fixture command")
		os.Exit(2)
	}
	root := os.Getenv("AO_UPGRADE_FIXTURE_DIR")
	if root == "" {
		os.Exit(2)
	}
	log, err := os.OpenFile(filepath.Join(root, fmt.Sprintf("provider-%d.jsonl", os.Getpid())), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	must(err)
	defer log.Close()
	journal, output := json.NewEncoder(log), json.NewEncoder(os.Stdout)
	sessionID := fmt.Sprintf("offline-acp-%d", os.Getpid())
	options := []object{
		{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": "fixture/worker", "options": []object{{"value": "fixture/worker", "name": "Offline worker"}}},
		{"id": "effort", "name": "Effort", "category": "thought_level", "type": "select", "currentValue": "high", "options": []object{{"value": "high", "name": "High"}}},
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
		must(journal.Encode(object{"pid": os.Getpid(), "threadId": sessionID, "method": frame.Method, "params": frame.Params}))
		must(log.Sync())
		if len(frame.ID) == 0 {
			continue
		}
		result := object{}
		switch frame.Method {
		case "initialize":
			result = object{"protocolVersion": 1, "agentInfo": object{"name": "opencode", "version": "1.2.0"}, "agentCapabilities": object{}, "authMethods": []any{}}
		case "session/new":
			result = object{"sessionId": sessionID, "configOptions": options}
		case "session/set_config_option":
			result = object{"configOptions": options}
		case "session/prompt":
			parts, _ := frame.Params["prompt"].([]any)
			var texts []string
			for _, part := range parts {
				if p, ok := part.(map[string]any); ok {
					if text, ok := p["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
			prompt := strings.Join(texts, "\n")
			must(output.Encode(object{"jsonrpc": "2.0", "method": "session/update", "params": object{"sessionId": sessionID, "update": object{"sessionUpdate": "agent_message_chunk", "content": object{"type": "text", "text": "OFFLINE_WORKER_ACCEPTED"}}}}))
			must(output.Encode(object{"jsonrpc": "2.0", "id": frame.ID, "result": object{"stopReason": "end_turn"}}))
			runHandoff(prompt, sessionID)
			continue
		default:
			must(output.Encode(object{"jsonrpc": "2.0", "id": frame.ID, "error": object{"code": -32601, "message": "unsupported offline ACP method"}}))
			continue
		}
		must(output.Encode(object{"jsonrpc": "2.0", "id": frame.ID, "result": result}))
	}
	must(scanner.Err())
}

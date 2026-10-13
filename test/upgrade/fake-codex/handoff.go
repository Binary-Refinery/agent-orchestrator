package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type handoffPlan struct{ URL, Root, ProjectID, WorkID, OrchestratorID, WorkerPrompt string }

// runHandoff is an explicit scripted actor, not an LLM. Each process can act
// only in its isolated fixture workspace and on this test daemon's loopback API.
func runHandoff(prompt, conversationID string) {
	planPath := os.Getenv("AO_UPGRADE_HANDOFF_PLAN")
	if planPath == "" {
		return
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		return
	}
	var plan handoffPlan
	must(json.Unmarshal(data, &plan))
	base, err := url.Parse(plan.URL)
	must(err)
	if base.Hostname() != "127.0.0.1" || base.Scheme != "http" {
		panic("handoff must use loopback")
	}
	sessionID := os.Getenv("AO_SESSION_ID")
	nativePrompt := prompt
	if os.Getenv("AO_REVIEW_SESSION_ID") != "" && strings.HasPrefix(prompt, "Read and follow the AO review task in `") {
		match := regexp.MustCompile("`([^`]+)`").FindStringSubmatch(prompt)
		if len(match) == 2 {
			rel, err := filepath.Rel(plan.Root, match[1])
			must(err)
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				panic("review prompt escaped isolated fixture")
			}
			body, err := os.ReadFile(match[1])
			must(err)
			if len(body) > 256*1024 {
				panic("review fixture prompt too large")
			}
			prompt = string(body)
		}
	}
	var role string
	if sessionID == plan.OrchestratorID && strings.Contains(prompt, "HANDOFF_CHAIN") {
		role = "orchestrator"
	}
	if sessionID != "" && sessionID != plan.OrchestratorID && prompt == plan.WorkerPrompt {
		role = "worker"
	}
	if os.Getenv("AO_REVIEW_SESSION_ID") != "" && strings.HasPrefix(prompt, "Independent local review for worker ") {
		role = "reviewer"
	}
	if role == "" {
		return
	}
	root := os.Getenv("AO_UPGRADE_FIXTURE_DIR")
	claim, err := os.OpenFile(filepath.Join(root, "handoff-"+role+".claim"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		return
	}
	must(err)
	must(claim.Close())
	if err := performHandoff(plan, role, sessionID, conversationID, prompt, nativePrompt); err != nil {
		_ = os.WriteFile(filepath.Join(root, fmt.Sprintf("handoff-error-%d.txt", os.Getpid())), []byte(role+": "+err.Error()), 0600)
	}
}

func handoffAPI(plan handoffPlan, method, path string, body any, target any) error {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, plan.URL+path, input)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AO-Session-ID", os.Getenv("AO_SESSION_ID"))
	client := http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, response.StatusCode, data)
	}
	if target != nil {
		return json.Unmarshal(data, target)
	}
	return nil
}
func handoffReceipt(role string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	root := os.Getenv("AO_UPGRADE_FIXTURE_DIR")
	file, err := os.CreateTemp(root, "handoff-receipt-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(root, "handoff-"+role+".json"))
}
func fixtureGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}
func performHandoff(plan handoffPlan, role, sessionID, conversationID, prompt, nativePrompt string) error {
	if role == "orchestrator" {
		var spawned object
		err := handoffAPI(plan, "POST", "/api/v1/sessions", object{"projectId": plan.ProjectID, "parentSessionId": sessionID, "workId": plan.WorkID, "workRole": "worker", "mode": "chat", "approvalMode": "accept-edits", "prompt": plan.WorkerPrompt}, &spawned)
		if err != nil {
			return err
		}
		return handoffReceipt(role, object{"sessionId": sessionID, "conversationId": conversationID, "prompt": prompt, "spawn": spawned})
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(plan.Root, cwd)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("fixture workspace escaped root: %s", cwd)
	}
	if role == "worker" {
		// Native initial-turn handling can finish before the spawn launch receipt.
		deadline := time.Now().Add(20 * time.Second)
		for {
			var details struct {
				Work struct {
					Launches []struct{ SessionID, Status string }
				}
			}
			if err = handoffAPI(plan, "GET", "/api/v1/instructor/works/"+plan.WorkID, nil, &details); err != nil {
				return err
			}
			admitted := false
			for _, launch := range details.Work.Launches {
				if launch.SessionID == sessionID && (launch.Status == "launched" || launch.Status == "native-config-reported") {
					admitted = true
				}
			}
			if admitted {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("worker launch receipt never committed")
			}
			time.Sleep(50 * time.Millisecond)
		}
		if err = os.WriteFile("handoff-result.txt", []byte("HANDOFF_IMPLEMENTED\n"), 0600); err != nil {
			return err
		}
		if _, err = fixtureGit("add", "handoff-result.txt"); err != nil {
			return err
		}
		if _, err = fixtureGit("-c", "user.name=Offline Worker", "-c", "user.email=worker@example.invalid", "commit", "-m", "Implement offline handoff fixture\n\nCo-authored-by: Offline Fixture <fixture@example.invalid>\nAO-session: "+sessionID); err != nil {
			return err
		}
		sha, err := fixtureGit("rev-parse", "HEAD")
		if err != nil {
			return err
		}
		if err = handoffAPI(plan, "POST", "/api/v1/instructor/reports", object{"workId": plan.WorkID, "sessionId": sessionID, "kind": "checkpoint", "sha": sha, "summary": "Offline worker implemented its explicit brief"}, nil); err != nil {
			return err
		}
		var receipt object
		if err = handoffAPI(plan, "POST", "/api/v1/reports", object{"sessionId": sessionID, "state": "done", "note": "HANDOFF_IMPLEMENTED; local commit " + sha}, &receipt); err != nil {
			return err
		}
		return handoffReceipt(role, object{"sessionId": sessionID, "conversationId": conversationID, "prompt": prompt, "sha": sha, "workspace": cwd, "report": receipt})
	}
	match := regexp.MustCompile(`Independent local review for worker ([^,]+), work phase scope ([^,]+), exact commit ([0-9a-f]{40}), run ([^.\s]+)\.`).FindStringSubmatch(prompt)
	if len(match) != 5 || !strings.Contains(match[2], plan.WorkID) {
		return fmt.Errorf("independent review lacks exact worker/work/SHA/run scope: %s", prompt)
	}
	sha, err := fixtureGit("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if sha != match[3] {
		return fmt.Errorf("review SHA mismatch")
	}
	content, err := os.ReadFile("handoff-result.txt")
	if err != nil {
		return err
	}
	if string(content) != "HANDOFF_IMPLEMENTED\n" {
		return fmt.Errorf("review content mismatch")
	}
	// Only read-only Git/file checks plus the existing review result API occur.
	if err = handoffAPI(plan, "POST", "/api/v1/sessions/"+match[1]+"/reviews/submit", object{"runId": match[4], "verdict": "approved", "body": "OFFLINE_INDEPENDENT_REVIEW: exact commit and expected fixture content inspected; no findings."}, nil); err != nil {
		return err
	}
	return handoffReceipt(role, object{"workerSessionId": match[1], "reviewSessionId": os.Getenv("AO_REVIEW_SESSION_ID"), "conversationId": conversationID, "prompt": prompt, "nativePrompt": nativePrompt, "sha": sha, "scope": match[2], "runId": match[4]})
}

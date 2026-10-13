//go:build upgrade

package upgrade_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/persistenthost"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

// TestNativeInstructorHandoff crosses real daemon, Codex, OpenCode ACP and
// independent review process boundaries. Only agent decisions and provider
// responses are scripted; all Work/outbox/check/review facts enter production
// HTTP/services/SQLite. No fixture writes the database or signs a human gate.
func TestNativeInstructorHandoff(t *testing.T) {
	binary := os.Getenv("AO_UPGRADE_NEW_BINARY")
	if binary == "" {
		t.Fatal("set AO_UPGRADE_NEW_BINARY to the candidate daemon")
	}
	h := newHarness(t)
	// Normal evidence discovery has bounded asynchronous 30s deferrals; the
	// complete checks/review chain spans several production scheduler ticks.
	h.waitLimit = 120 * time.Second
	planPath := filepath.Join(h.fixture, "handoff-plan.json")
	h.env = append(h.env, "AO_UPGRADE_HANDOFF_PLAN="+planPath)
	h.start(binary)
	t.Cleanup(func() {
		paths, _ := filepath.Glob(filepath.Join(h.data, "chat-hosts", "*", "host.json"))
		for _, path := range paths {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = persistenthost.Shutdown(ctx, h.data, filepath.Base(filepath.Dir(path)))
			cancel()
		}
	})
	var policy domain.InstructorPolicy
	h.api("GET", "/api/v1/instructor/policy", nil, &policy)
	policy.InitialPhase = "implement"
	for name, model := range map[string]string{"worker": "fixture/worker", "reviewer": "upgrade-model", "orchestrator": "upgrade-model"} {
		role := policy.Roles[name]
		role.SelectionSource = ""
		role.Model = model
		if name == "worker" {
			role.Harness = "opencode"
		} else {
			role.Harness = "codex"
		}
		if name == "orchestrator" {
			role.Effort = "configured"
		} else {
			role.EffortByModel = map[string]string{model: "high"}
		}
		policy.Roles[name] = role
	}
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "diff", "--check", "HEAD^", "HEAD"}, TimeoutSeconds: 30}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "show", "HEAD:handoff-result.txt"}, TimeoutSeconds: 30}
	h.api("PUT", "/api/v1/instructor/policy", policy, &policy)
	config := object{"defaultBranch": "main", "governanceManaged": true,
		"worker":       object{"agent": "opencode", "agentConfig": object{"model": "fixture/worker", "effort": "high"}},
		"orchestrator": object{"agent": "codex", "agentConfig": object{"model": "upgrade-model", "effort": "high"}},
		"reviewers":    []object{{"harness": "codex", "agentConfig": object{"model": "upgrade-model", "effort": "high"}}},
	}
	h.api("POST", "/api/v1/projects", object{"projectId": "handoff", "path": h.repo, "name": "Native handoff acceptance", "config": config}, nil)
	goal := "HANDOFF_CHAIN: create one local fixture artifact, independently review its exact commit, and preserve all human gates."
	var work domain.InstructorWork
	h.api("POST", "/api/v1/instructor/works", object{"projectId": "handoff", "name": "Native handoff", "goal": goal, "source": "manual", "sourceRef": "native-handoff-fixture"}, &work)
	var spawned controllers.SpawnOrchestratorResponse
	h.api("POST", "/api/v1/orchestrators", object{"projectId": "handoff", "mode": "chat", "approvalMode": "accept-edits"}, &spawned)
	h.session = string(spawned.Orchestrator.ID)
	h.await("orchestrator ready", func() bool { return h.conversation().Controller == "ready" })
	var sessionsBefore, sessionsAfter controllers.ListSessionsResponse
	h.api("GET", "/api/v1/sessions?projectId=handoff", nil, &sessionsBefore)
	for _, blank := range []string{"", " \t\n"} {
		status, body := h.request("POST", "/api/v1/sessions", object{"projectId": "handoff", "parentSessionId": h.session, "workId": work.ID, "workRole": "worker", "mode": "chat", "prompt": blank})
		var failure struct{ Code string }
		must(t, json.Unmarshal(body, &failure))
		if status != 400 || failure.Code != "GOVERNED_TASK_REQUIRED" {
			t.Fatalf("empty worker task: HTTP %d: %s", status, body)
		}
	}
	h.api("GET", "/api/v1/sessions?projectId=handoff", nil, &sessionsAfter)
	if len(sessionsAfter.Sessions) != len(sessionsBefore.Sessions) || h.details(work.ID).Work.Revision != work.Revision || h.providerCount("session/prompt") != 0 {
		t.Fatal("rejected empty task created a session/native turn or changed the Work")
	}
	task := "Implement only the offline acceptance subtask: create handoff-result.txt containing HANDOFF_IMPLEMENTED followed by a newline, commit it locally, and report the immutable SHA. Do not publish, approve gates, or create unrelated work."
	plan := object{"URL": h.url, "Root": h.root, "ProjectID": "handoff", "WorkID": work.ID, "OrchestratorID": h.session, "WorkerPrompt": task}
	data, err := json.Marshal(plan)
	must(t, err)
	must(t, os.WriteFile(planPath, data, 0600))
	var snapshot domain.InstructorSnapshot
	h.api("GET", "/api/v1/instructor/status", nil, &snapshot)
	command := object{"command": "weiter", "snapshotId": snapshot.ID, "workIds": []string{work.ID}, "idempotencyKey": "native-handoff-command"}
	var receipt domain.InstructorCommandResult
	h.api("POST", "/api/v1/instructor/commands", command, &receipt)
	if len(receipt.Dispatches) != 1 {
		t.Fatalf("expected one initial directive: %+v", receipt)
	}
	var worker struct {
		SessionID, ConversationID, Prompt, SHA, Workspace string
		Report                                            object
	}
	h.await("OpenCode worker report through real API", func() bool { return readReceipt(h, "worker", &worker) })
	var orchestrator struct{ SessionID, ConversationID, Prompt string }
	h.await("orchestrator native spawn receipt", func() bool { return readReceipt(h, "orchestrator", &orchestrator) })
	if worker.Prompt != task || worker.SessionID == h.session || worker.ConversationID == orchestrator.ConversationID {
		t.Fatalf("worker task/conversation boundary invalid: %+v", worker)
	}
	if !strings.Contains(orchestrator.Prompt, goal) || !strings.Contains(orchestrator.Prompt, work.ID) {
		t.Fatal("Instructor directive lost user goal/work context")
	}
	if nativePromptCount(h, "session/prompt", task) != 1 {
		t.Fatal("explicit worker task did not reach native OpenCode exactly once")
	}
	var nativeWorker controllers.ConversationSnapshotResponse
	h.api("GET", "/api/v1/sessions/"+worker.SessionID+"/conversation", nil, &nativeWorker)
	found := 0
	for _, message := range nativeWorker.Messages {
		if message.Text == task {
			found++
		}
	}
	if found != 1 || len(nativeWorker.Turns) != 1 || nativeWorker.Turns[0].State != "completed" {
		t.Fatalf("worker conversation is empty, duplicated or unfinished: %+v", nativeWorker)
	}
	var reviewer struct{ WorkerSessionID, ReviewSessionID, ConversationID, Prompt, NativePrompt, SHA, Scope, RunID string }
	var lastRevision int64
	h.await("independent native reviewer completion", func() bool {
		current := h.details(work.ID)
		if current.Work.Revision != lastRevision {
			lastRevision = current.Work.Revision
			t.Logf("handoff progress: phase=%s state=%s summary=%s wait=%s evidence=%d actions=%+v", current.Work.Phase, current.Work.State, current.Work.Summary, current.Work.WaitCondition, len(current.Work.Evidence), current.Work.PhaseActions)
		}
		return readReceipt(h, "reviewer", &reviewer)
	})
	if reviewer.WorkerSessionID != worker.SessionID || reviewer.ConversationID == worker.ConversationID || reviewer.ConversationID == orchestrator.ConversationID || reviewer.SHA != worker.SHA || !strings.Contains(reviewer.Scope, work.ID) || !strings.Contains(reviewer.Scope, "/review/") {
		t.Fatalf("review lost worker/commit/independence scope: %+v", reviewer)
	}
	if nativePromptCount(h, "turn/start", reviewer.NativePrompt) != 1 {
		t.Fatal("independent review briefing was not delivered exactly once")
	}
	if h.providerCount("thread/start") != 2 {
		t.Fatal("expected exactly one Codex orchestrator and one independent reviewer conversation")
	}
	h.await("verified review returned to Instructor", func() bool {
		details := h.details(work.ID)
		for _, e := range details.Work.Evidence {
			if e.Kind == "review-report" && e.Verified && e.SHA == worker.SHA {
				return details.Work.Phase == "triage"
			}
		}
		return false
	})
	// An explicit operator decision is still needed; provider 'approved' is not
	// authority to complete the Work, publish, or sign the triage/fix gate.
	current := h.details(work.ID)
	if current.Work.State == "done" {
		t.Fatal("native reviewer approval completed Work without the locked closeout chain")
	}
	h.await("unapproved native workflow triage gate", func() bool {
		w := h.details(work.ID).Work
		return w.Phase == "triage" && w.State == "waiting_human" && w.Gate != nil && w.Gate.Kind == "transition" && w.Gate.TransitionTo == "fix"
	})
	before := h.details(work.ID)
	h.stop()
	h.start(binary)
	h.await("existing orchestrator reattached", func() bool { return h.conversation().Controller == "ready" })
	var resumedWorker controllers.ConversationSnapshotResponse
	h.await("existing OpenCode worker reattached", func() bool {
		h.api("GET", "/api/v1/sessions/"+worker.SessionID+"/conversation", nil, &resumedWorker)
		return resumedWorker.Controller == "ready"
	})
	assertHistoryRetained(t, nativeWorker, resumedWorker)
	after := h.details(work.ID)
	if after.Work.Gate == nil || after.Work.Gate.ID != before.Work.Gate.ID || after.Work.State != "waiting_human" || after.Work.ID != work.ID {
		t.Fatal("restart silently advanced or replaced the pending human gate")
	}
	var retried domain.InstructorCommandResult
	h.api("POST", "/api/v1/instructor/commands", command, &retried)
	if len(retried.Dispatches) != 1 || retried.Dispatches[0].ID != receipt.Dispatches[0].ID || nativePromptCount(h, "session/prompt", task) != 1 || nativePromptCount(h, "turn/start", reviewer.NativePrompt) != 1 {
		t.Fatal("restart/retry duplicated a handoff or native task")
	}
	if h.providerCount("thread/start") != 2 || h.providerCount("session/prompt") != 1 {
		t.Fatal("restart spawned an unrelated native context or duplicated worker execution")
	}
	for _, kind := range []string{"push", "full", "review-report"} {
		found := false
		for _, e := range after.Work.Evidence {
			if (e.Kind == kind || e.Kind == "check" && e.Profile == kind) && e.Verified && e.SHA == worker.SHA {
				found = true
			}
		}
		if !found {
			t.Fatalf("lost verified %s evidence", kind)
		}
	}
	t.Logf("PASS: Instructor %s -> Codex orchestrator %s -> OpenCode worker %s (one exact task) -> independent Codex review %s -> verified push/full/review -> retained human gate", work.ID, h.session, worker.SessionID, reviewer.RunID)
	t.Log("BOUNDARY: protected publication, external CI/traceability and final done remain unapproved and are not claimed by this offline process test")
}

func readReceipt(h *harness, role string, target any) bool {
	h.t.Helper()
	errors, _ := filepath.Glob(filepath.Join(h.fixture, "handoff-error-*.txt"))
	if len(errors) > 0 {
		h.t.Fatalf("scripted fixture failure\n%s", h.logs())
	}
	data, err := os.ReadFile(filepath.Join(h.fixture, "handoff-"+role+".json"))
	if os.IsNotExist(err) {
		return false
	}
	must(h.t, err)
	must(h.t, json.Unmarshal(data, target))
	return true
}
func nativePromptCount(h *harness, method, prompt string) int {
	paths, err := filepath.Glob(filepath.Join(h.fixture, "provider-*.jsonl"))
	must(h.t, err)
	count := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		must(h.t, err)
		for _, line := range strings.Split(string(data), "\n") {
			var frame struct {
				Method string
				Params struct{ Input, Prompt []struct{ Text string } }
			}
			if json.Unmarshal([]byte(line), &frame) != nil || frame.Method != method {
				continue
			}
			parts := frame.Params.Input
			if method == "session/prompt" {
				parts = frame.Params.Prompt
			}
			for _, part := range parts {
				if part.Text == prompt {
					count++
				}
			}
		}
	}
	return count
}

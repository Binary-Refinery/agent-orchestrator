//go:build upgrade

// Package upgrade_test runs two real daemon builds over isolated durable state.
// Only the native Codex executable is a deterministic offline protocol fixture.
package upgrade_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/persistenthost"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/processalive"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type object = map[string]any

// TestUpgradeContinuity intentionally requires separately built binaries. A
// same-binary daemon restart does not prove compatibility with the last release.
func TestUpgradeContinuity(t *testing.T) {
	oldBinary, nextBinary := os.Getenv("AO_UPGRADE_OLD_BINARY"), os.Getenv("AO_UPGRADE_NEW_BINARY")
	if oldBinary == "" || nextBinary == "" {
		t.Fatal("set AO_UPGRADE_OLD_BINARY and AO_UPGRADE_NEW_BINARY to distinct N and N+1 builds; see docs/upgrade-continuity.md")
	}
	oldHash, nextHash := binaryHash(t, oldBinary), binaryHash(t, nextBinary)
	if oldHash == nextHash {
		t.Fatal("upgrade acceptance requires different binaries, not a same-build restart")
	}
	t.Logf("N sha256=%s; N+1 sha256=%s; OS=%s", oldHash, nextHash, runtime.GOOS)
	h := newHarness(t)
	h.start(oldBinary)

	// Preserve the N policy document verbatim. Its canonical hash must remain
	// valid after decoding in N+1, including newly added optional schema fields.
	var policy domain.InstructorPolicy
	h.api("GET", "/api/v1/instructor/policy", nil, &policy)
	role := policy.Roles["orchestrator"]
	role.Harness, role.Model, role.Effort = "codex", "upgrade-model", "configured"
	policy.Roles["orchestrator"] = role
	// Hold autonomous phase advancement while the test exercises delivery. This
	// is an ordinary additional evidence prerequisite, not a runtime test hook.
	policy.Transitions[0].Requires = []string{"upgrade-hold"}
	policy.Version = ""
	h.api("PUT", "/api/v1/instructor/policy", policy, &policy)
	h.api("POST", "/api/v1/projects", object{
		"projectId": "upgrade", "path": h.repo, "name": "Upgrade acceptance",
		"config": object{"defaultBranch": "main", "governanceManaged": true, "orchestrator": object{"agent": "codex", "agentConfig": object{"model": "upgrade-model", "effort": "high"}}},
	}, nil)
	var work domain.InstructorWork
	h.api("POST", "/api/v1/instructor/works", object{"projectId": "upgrade", "name": "Retained work", "goal": "UPGRADE_RETAIN_WORK", "source": "manual", "sourceRef": "upgrade-fixture"}, &work)
	if work.PolicyVersion != instructorsvc.PolicyVersion(work.Policy) {
		t.Fatal("candidate cannot read the legacy policy hash before migration")
	}
	var spawned controllers.SpawnOrchestratorResponse
	h.api("POST", "/api/v1/orchestrators", object{"projectId": "upgrade", "mode": "chat", "approvalMode": "accept-edits"}, &spawned)
	h.session = string(spawned.Orchestrator.ID)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = persistenthost.Shutdown(ctx, h.data, h.session)
	})
	h.await("initial controller", func() bool { return h.conversation().Controller == "ready" })

	// Reach the provider through the real durable Instructor outbox and semantic
	// delivery ledger, not a direct fake Delivery implementation.
	var snapshot domain.InstructorSnapshot
	h.api("GET", "/api/v1/instructor/status", nil, &snapshot)
	command := object{"command": "weiter", "snapshotId": snapshot.ID, "workIds": []string{work.ID}, "idempotencyKey": "upgrade-dispatch"}
	var receipt domain.InstructorCommandResult
	h.api("POST", "/api/v1/instructor/commands", command, &receipt)
	if len(receipt.Dispatches) != 1 {
		t.Fatalf("expected one Instructor dispatch: %+v", receipt)
	}
	dispatchID := receipt.Dispatches[0].ID
	h.await("Instructor native delivery", func() bool {
		return len(h.conversation().Turns) == 1 && h.conversation().Turns[0].State == "completed"
	})
	h.await("durable dispatch target", func() bool {
		d := h.details(work.ID)
		return len(d.Dispatches) == 1 && d.Dispatches[0].TargetSessionID == domain.SessionID(h.session) && (d.Dispatches[0].State == "delivered" || d.Dispatches[0].State == "started")
	})
	h.api("POST", "/api/v1/instructor/reports", object{"workId": work.ID, "sessionId": h.session, "kind": "accepted", "dispatchId": dispatchID}, nil)
	h.api("POST", "/api/v1/instructor/reports", object{
		"workId": work.ID, "sessionId": h.session, "kind": "needs_input", "dispatchId": dispatchID,
		"gate": object{"id": "upgrade-human-gate", "kind": "question", "title": "Keep the human decision pending", "exactApproval": "UPGRADE-GO"},
	}, nil)
	beforeWork := h.details(work.ID)
	if beforeWork.Work.Gate == nil || beforeWork.Work.State != "waiting_human" {
		t.Fatalf("fixture did not reach a real pending human gate: %+v", beforeWork.Work)
	}
	if beforeWork.BudgetUsage == nil || beforeWork.BudgetUsage.Turns < 1 || beforeWork.BudgetUsage.Sessions < 1 {
		t.Fatal("fixture did not consume real governed session and turn budgets")
	}

	h.message("UPGRADE_HOLD_FOR_APPROVAL", "upgrade-held-turn")
	h.await("provider approval", func() bool { return pendingApproval(h.conversation()) != "" })
	h.message("UPGRADE_QUEUED_USER_MESSAGE", "upgrade-queued-turn")
	before := h.conversation()
	approvalID := pendingApproval(before)
	hostBefore := h.host()
	providerBefore := h.providerCount("thread/start")
	if providerBefore != 1 || h.providerCount("turn/start") != 2 {
		t.Fatalf("unexpected fixture setup: threads=%d turns=%d", providerBefore, h.providerCount("turn/start"))
	}
	providerPID := h.providerPIDs("thread/start")[0]
	if !processalive.Alive(providerPID) {
		t.Fatalf("native provider %d is not alive before upgrade", providerPID)
	}

	h.stop()
	h.start(nextBinary)
	h.await("live host adoption with pending approval", func() bool {
		return pendingApproval(h.conversation()) == approvalID && h.conversation().Controller != "stopped"
	})
	after := h.conversation()
	if after.ConversationID != before.ConversationID || after.SessionID != before.SessionID || h.host().PID != hostBefore.PID || !processalive.Alive(providerPID) {
		t.Fatal("update replaced a surviving native host or durable conversation identity")
	}
	assertHistoryRetained(t, before, after)
	assertWorkRetained(t, beforeWork, h.details(work.ID))
	if h.providerCount("thread/start") != 1 || h.providerCount("thread/resume") != 0 || h.providerCount("turn/start") != 2 {
		t.Fatal("reattachment repeated native initialization/resume/dispatch or ran queued work before approval")
	}
	var retried domain.InstructorCommandResult
	h.api("POST", "/api/v1/instructor/commands", command, &retried)
	if len(retried.Dispatches) != 1 || retried.Dispatches[0].ID != dispatchID {
		t.Fatalf("upgrade lost Instructor retry identity: %+v", retried)
	}
	t.Log("PASS: N -> N+1 retained the live host, legacy policy hash, Work gate, history, provider approval, queue and dispatch retry identity")

	// The provider approval is independent of the Instructor's pending human
	// gate. Explicitly answering it may drain the already-authorized user queue;
	// it must not approve or execute the work's business decision.
	h.api("POST", "/api/v1/sessions/"+h.session+"/conversation/approvals/"+url.PathEscape(approvalID)+"/resolve", object{"decisionId": "accept"}, nil)
	h.await("authorized queue drain", func() bool {
		c := h.conversation()
		return len(c.Turns) == 3 && c.Turns[2].State == "completed"
	})
	assertWorkRetained(t, beforeWork, h.details(work.ID))
	settled := h.conversation()
	t.Log("PASS: explicit native approval drained the authorized queue without approving the Work gate")

	// A conclusive dead host requires a new process with the SAME native thread.
	// Kill only processes proven by this isolated fixture's descriptor/journal.
	h.stop()
	h.killHost(providerPID)
	h.start(nextBinary)
	h.await("native recovery or actionable recovery state", func() bool {
		var recovery domain.InstructorRecoveryStatus
		h.api("GET", "/api/v1/instructor/works/"+work.ID+"/recovery", nil, &recovery)
		if recovery.Ready {
			return true
		}
		if recovery.Action == "resume" || recovery.Action == "restore" {
			status, body := h.request("POST", "/api/v1/instructor/works/"+work.ID+"/recovery", domain.InstructorRecoveryInput{
				WorkID: recovery.WorkID, WorkRevision: recovery.WorkRevision, PolicyVersion: recovery.PolicyVersion,
				SessionID: recovery.SessionID, SessionCreatedAt: recovery.SessionCreatedAt,
				ProviderConversationID: recovery.ProviderConversationID, ControllerGeneration: recovery.ControllerGeneration,
			})
			if status == http.StatusConflict {
				var failure struct {
					Code string `json:"code"`
				}
				must(t, json.Unmarshal(body, &failure))
				if failure.Code == "SESSION_RECOVERY_IN_PROGRESS" {
					return false // Startup won the race; refresh the exact observation.
				}
			}
			if status != http.StatusOK {
				t.Fatalf("guided recovery: HTTP %d: %s\n%s", status, body, h.logs())
			}
			return true
		}
		return false
	})
	h.await("recovered controller", func() bool { return h.conversation().Controller == "ready" })
	recovered := h.conversation()
	if recovered.ConversationID != settled.ConversationID || recovered.SessionID != settled.SessionID || h.host().PID == hostBefore.PID {
		t.Fatal("dead-host recovery did not retain identity and replace the dead owner")
	}
	assertHistoryRetained(t, settled, recovered)
	assertWorkRetained(t, beforeWork, h.details(work.ID))
	if h.providerCount("thread/start") != 1 || h.providerCount("thread/resume") != 1 || h.providerCount("turn/start") != 3 {
		t.Fatalf("native recovery created unrelated context or duplicated execution: start=%d resume=%d turns=%d", h.providerCount("thread/start"), h.providerCount("thread/resume"), h.providerCount("turn/start"))
	}
	replacementPID := h.providerPIDs("thread/resume")[0]
	if replacementPID == providerPID || !processalive.Alive(replacementPID) || processalive.Alive(providerPID) {
		t.Fatalf("native provider ownership changed incorrectly: old=%d replacement=%d", providerPID, replacementPID)
	}
	for _, message := range []string{"UPGRADE_HOLD_FOR_APPROVAL", "UPGRADE_QUEUED_USER_MESSAGE"} {
		count := 0
		for _, m := range recovered.Messages {
			if m.Text == message {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("message %q has %d copies after recovery", message, count)
		}
	}
	t.Log("PASS: a dead host resumed the same native conversation without duplicate turns or lost Work decisions")
}

type harness struct {
	t                         *testing.T
	root, data, repo, fixture string
	env                       []string
	url, session              string
	cmd                       *exec.Cmd
	done                      chan error
	log                       *os.File
	client                    *http.Client
	waitLimit                 time.Duration
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{t: t, root: root, data: filepath.Join(root, "data"), repo: filepath.Join(root, "repo"), fixture: filepath.Join(root, "provider-state"), client: &http.Client{Timeout: 30 * time.Second}}
	binDir, home := filepath.Join(root, "bin"), filepath.Join(root, "home")
	for _, dir := range []string{h.data, h.repo, h.fixture, binDir, filepath.Join(home, ".codex"), filepath.Join(home, "AppData", "Roaming"), filepath.Join(home, "AppData", "Local")} {
		must(t, os.MkdirAll(dir, 0o700))
	}
	// This sentinel has no authority outside the offline fixture.
	must(t, os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(`{"OPENAI_API_KEY":"offline-upgrade-fixture-not-a-real-key"}`), 0o600))
	fixtureBinary := filepath.Join(binDir, "codex")
	if runtime.GOOS == "windows" {
		fixtureBinary += ".exe"
	}
	build := exec.Command("go", "build", "-o", fixtureBinary, "../../../../test/upgrade/fake-codex/main.go", "../../../../test/upgrade/fake-codex/handoff.go", "../../../../test/upgrade/fake-codex/opencode.go")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native fixture: %v\n%s", err, out)
	}
	fixtureData, err := os.ReadFile(fixtureBinary)
	must(t, err)
	openCodeBinary := filepath.Join(binDir, "opencode")
	if runtime.GOOS == "windows" {
		openCodeBinary += ".exe"
	}
	must(t, os.WriteFile(openCodeBinary, fixtureData, 0o700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	must(t, listener.Close())
	h.url = fmt.Sprintf("http://127.0.0.1:%d", port)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "AO_") || strings.HasPrefix(upper, "CODEX_") || upper == "HOME" || upper == "USERPROFILE" || upper == "APPDATA" || upper == "LOCALAPPDATA" || upper == "PATH" || upper == "OPENAI_API_KEY" || upper == "GH_TOKEN" || upper == "GITHUB_TOKEN" || upper == "GH_CONFIG_DIR" {
			continue
		}
		h.env = append(h.env, kv)
	}
	h.env = append(h.env, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "HOME="+home, "USERPROFILE="+home, "APPDATA="+filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"), "CODEX_HOME="+filepath.Join(home, ".codex"), "GH_CONFIG_DIR="+filepath.Join(home, "gh"), "AO_DATA_DIR="+h.data, "AO_RUN_FILE="+filepath.Join(root, "running.json"), fmt.Sprintf("AO_PORT=%d", port), "AO_INSTRUCTOR=1", "AO_AGENT=codex", "AO_TELEMETRY_REMOTE=off", "AO_UPGRADE_FIXTURE_DIR="+h.fixture)
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=Upgrade Fixture", "-c", "user.email=upgrade@example.invalid", "commit", "--allow-empty", "-m", "fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = h.repo, h.env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v\n%s", err, out)
		}
	}
	t.Cleanup(h.stop)
	return h
}

func (h *harness) start(binary string) {
	h.t.Helper()
	log, err := os.CreateTemp(h.root, "daemon-*.log")
	must(h.t, err)
	h.log = log
	h.cmd = exec.Command(binary, "daemon")
	h.cmd.Dir, h.cmd.Env, h.cmd.Stdout, h.cmd.Stderr = h.data, h.env, log, log
	must(h.t, h.cmd.Start())
	h.done = make(chan error, 1)
	cmd := h.cmd
	go func() { h.done <- cmd.Wait() }()
	h.await("daemon health", func() bool {
		resp, err := h.client.Get(h.url + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

func (h *harness) stop() {
	if h.cmd == nil {
		return
	}
	request, _ := http.NewRequest("POST", h.url+"/shutdown", nil)
	if response, err := h.client.Do(request); err == nil {
		_ = response.Body.Close()
	}
	select {
	case err := <-h.done:
		if err != nil {
			h.t.Errorf("daemon exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		_ = h.cmd.Process.Kill()
		<-h.done
		h.t.Error("daemon did not stop gracefully")
	}
	_ = h.log.Close()
	h.cmd = nil
}

func (h *harness) api(method, path string, input, output any) {
	h.t.Helper()
	status, data := h.request(method, path, input)
	if status < 200 || status >= 300 {
		h.t.Fatalf("%s %s: HTTP %d: %s\n%s", method, path, status, data, h.logs())
	}
	if output != nil {
		must(h.t, json.Unmarshal(data, output))
	}
}

func (h *harness) request(method, path string, input any) (int, []byte) {
	h.t.Helper()
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		must(h.t, err)
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, h.url+path, body)
	must(h.t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(request)
	must(h.t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	must(h.t, err)
	return response.StatusCode, data
}

func (h *harness) conversation() controllers.ConversationSnapshotResponse {
	var result controllers.ConversationSnapshotResponse
	h.api("GET", "/api/v1/sessions/"+h.session+"/conversation", nil, &result)
	return result
}

func (h *harness) details(id string) domain.InstructorDetails {
	var result domain.InstructorDetails
	h.api("GET", "/api/v1/instructor/works/"+id, nil, &result)
	return result
}

func (h *harness) message(text, key string) {
	h.api("POST", "/api/v1/sessions/"+h.session+"/conversation/messages", object{"text": text, "clientMessageId": key}, nil)
}

func (h *harness) host() persistenthost.Descriptor {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.data, "chat-hosts", h.session, "host.json"))
	must(h.t, err)
	var result persistenthost.Descriptor
	must(h.t, json.Unmarshal(data, &result))
	return result
}

func (h *harness) killHost(providerPID int) {
	h.t.Helper()
	host := h.host()
	process, err := os.FindProcess(host.PID)
	must(h.t, err)
	defer process.Release()
	must(h.t, process.Kill())
	// This fixture exits when its host-owned stdin pipe closes. Verify both
	// known processes are dead before starting a replacement; this does not
	// establish real vendor process-tree cleanup on an installer update.
	h.await("dead host and native provider processes", func() bool {
		return !processalive.Alive(host.PID) && !processalive.Alive(providerPID)
	})
	h.await("dead host endpoint", func() bool {
		conn, err := net.DialTimeout("tcp", host.Address, 100*time.Millisecond)
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	})
	h.t.Logf("PASS: host PID %d and native provider PID %d exited before replacement", host.PID, providerPID)
}

func (h *harness) providerCount(method string) int {
	return len(h.providerPIDs(method))
}

func (h *harness) providerPIDs(method string) []int {
	h.t.Helper()
	paths, err := filepath.Glob(filepath.Join(h.fixture, "provider-*.jsonl"))
	must(h.t, err)
	var pids []int
	for _, path := range paths {
		data, err := os.ReadFile(path)
		must(h.t, err)
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			var frame struct {
				Method string `json:"method"`
				PID    int    `json:"pid"`
			}
			if json.Unmarshal(line, &frame) == nil && frame.Method == method {
				if frame.PID <= 0 {
					h.t.Fatalf("native fixture journal has an invalid PID: %s", line)
				}
				pids = append(pids, frame.PID)
			}
		}
	}
	return pids
}

func (h *harness) await(description string, predicate func() bool) {
	h.t.Helper()
	limit := h.waitLimit
	if limit == 0 {
		limit = 40 * time.Second
	}
	deadline := time.Now().Add(limit)
	for !predicate() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s\n%s", description, h.logs())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *harness) logs() string {
	paths, _ := filepath.Glob(filepath.Join(h.root, "daemon-*.log"))
	var result strings.Builder
	for _, path := range paths {
		data, _ := os.ReadFile(path)
		if len(data) > 20000 {
			data = data[len(data)-20000:]
		}
		result.Write(data)
	}
	errors, _ := filepath.Glob(filepath.Join(h.fixture, "handoff-error-*.txt"))
	for _, path := range errors {
		data, _ := os.ReadFile(path)
		result.Write(data)
	}
	return result.String()
}

func pendingApproval(c controllers.ConversationSnapshotResponse) string {
	for _, activity := range c.Activities {
		if activity.Status == "pending" && activity.RequestID != "" {
			return activity.RequestID
		}
	}
	return ""
}

func assertHistoryRetained(t *testing.T, before, after controllers.ConversationSnapshotResponse) {
	t.Helper()
	for _, expected := range before.Messages {
		found := 0
		for _, actual := range after.Messages {
			if actual.ID == expected.ID && actual.Text == expected.Text {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("durable message %s changed/disappeared/duplicated: copies=%d", expected.ID, found)
		}
	}
}

func assertWorkRetained(t *testing.T, before, after domain.InstructorDetails) {
	t.Helper()
	if after.Work.ID != before.Work.ID || after.Work.PolicyVersion != before.Work.PolicyVersion || after.Work.PolicyVersion != instructorsvc.PolicyVersion(after.Work.Policy) || after.Work.State != "waiting_human" || after.Work.Gate == nil || after.Work.Gate.ID != before.Work.Gate.ID {
		t.Fatalf("update changed durable work, pinned policy, or human gate: %+v", after.Work)
	}
	if !reflect.DeepEqual(after.Work.Gate, before.Work.Gate) || !reflect.DeepEqual(after.BudgetUsage, before.BudgetUsage) {
		t.Fatal("recovery changed the exact human decision or reset consumed work budgets")
	}
	if len(after.Dispatches) != len(before.Dispatches) {
		t.Fatalf("update duplicated dispatches: before=%d after=%d", len(before.Dispatches), len(after.Dispatches))
	}
	for _, event := range before.Events {
		found := false
		for _, current := range after.Events {
			if current.ID == event.ID {
				found = true
			}
		}
		if !found {
			t.Fatalf("update lost work event %s", event.ID)
		}
	}
}

func binaryHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	must(t, err)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

package instructor_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/backbone"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func producerJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func producerDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func producerWorkspace(t *testing.T) (string, backbone.Binding) {
	t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "policy", "backbone", "testdata", "producer"))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile(filepath.Join(fixture, "workspace.bundle"))
	if err != nil {
		t.Fatal(err)
	}
	if producerDigest(bundle) != "69cb616afcc14efc7c25a667a797d4e17a3b1ec6e4e26ea9df75ac896e7709ad" {
		t.Fatal("producer bundle changed")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit := func(cwd string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		argv := []string{"--no-replace-objects", "--no-lazy-fetch", "-c", "core.hooksPath=" + os.DevNull, "-c", "init.templateDir=", "-C", cwd}
		command := exec.CommandContext(ctx, "git", append(argv, args...)...)
		for _, entry := range os.Environ() {
			name, _, _ := strings.Cut(entry, "=")
			if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
				command.Env = append(command.Env, entry)
			}
		}
		command.Env = append(command.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("local producer fixture Git: %v: %s", err, output)
		}
	}
	runGit(root, "clone", "--no-checkout", "-q", filepath.Join(fixture, "workspace.bundle"), "workspace")
	workspace := filepath.Join(root, "workspace")
	runGit(workspace, "-c", "core.autocrlf=false", "checkout", "-q", "--detach", "c77601c5fcd96188ddeca71b3cc9429a2d692a51")
	raw, err := os.ReadFile(filepath.Join(fixture, "context.json"))
	if err != nil {
		t.Fatal(err)
	}
	requestRaw, err := os.ReadFile(filepath.Join(fixture, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		TaskPacket string `json:"task_packet"`
	}
	if err := json.Unmarshal(requestRaw, &request); err != nil {
		t.Fatal(err)
	}
	companion, err := os.ReadFile(filepath.Join(workspace, ".engineering-backbone", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]any{}
	for _, name := range []string{"context_loading", "stale_context", "tool_boundaries", "failed_check_reporting", "delegation_context", "resume_context"} {
		cases[name] = map[string]string{"result": "pass", "evidenceRef": "synthetic-admission-fixture:" + name}
	}
	// This synthetic record tests admission only; it is never presented as a
	// real model qualification or written to a user's project policy.
	qualification := producerJSON(t, map[string]any{"schemaVersion": "1", "harness": "opencode", "runtimeVersion": "1.19.0", "model": "provider/worker", "role": "implementer", "platform": runtime.GOOS, "interface": "chat", "configurationContract": "ao-opencode-backbone-v1", "observedAt": "2026-10-16T00:00:00Z", "status": "qualified", "evidenceRef": "synthetic-admission-fixture", "evidenceSha256": strings.Repeat("a", 64), "cases": cases})
	return workspace, backbone.Binding{SchemaVersion: "1", AdapterSHA256: producerDigest(companion), Contexts: map[string]backbone.RoleBinding{"worker": {Role: "implementer", TaskPacket: request.TaskPacket, Context: raw, Qualification: qualification}}}
}

func producerManagedWorker(t *testing.T, binding *backbone.Binding, workspace string) (*instructorFixture, domain.SessionRecord, domain.InstructorWork) {
	t.Helper()
	f := newInstructorFixture(t)
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.Model = "provider/worker"
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	if binding != nil {
		policy.Backbone = producerJSON(t, binding)
	}
	work := f.register(t, "producer-backed-work", policy)
	record, err := f.store.CreateSession(context.Background(), domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, Mode: domain.SessionModeChat, Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: f.now}, Metadata: domain.SessionMetadata{Model: "provider/worker", Effort: "high", WorkspacePath: workspace, Permissions: ports.PermissionModeDefault}, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.AttachLaunchingSession(context.Background(), work.ID, record.ID, ""); err != nil {
		t.Fatal(err)
	}
	return f, record, work
}

func TestBackboneProducerAdmissionPromptDispatchAndBinding(t *testing.T) {
	workspace, binding := producerWorkspace(t)
	f, worker, work := producerManagedWorker(t, &binding, workspace)
	ctx := context.Background()
	launch, prompt, err := f.service.PrepareBackboneController(ctx, worker.ID, workspace, domain.SessionModeChat, "provider/worker", ports.PermissionModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	if launch == nil || launch.InstructorRole != "worker" || launch.PolicyVersion != work.PolicyVersion || !strings.Contains(prompt, "Task Packet") || !strings.Contains(prompt, ".opencode/agents/backbone-implementer.md") {
		t.Fatal("actual producer context was not bound to standing prompt")
	}
	if err := f.service.ValidateBackboneControllerBinding(ctx, worker.ID, launch); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ValidateResume(ctx, worker.ID); err != nil {
		t.Fatalf("current producer receipt cannot resume through admission: %v", err)
	}
	if err := f.service.ValidateDispatchAdmission(ctx, worker.ID, "ordinary-queued-turn"); err != nil {
		t.Fatalf("current producer receipt not admitted at dispatch: %v", err)
	}
	if err := f.service.ValidateBackboneControllerBinding(ctx, worker.ID, nil); err == nil {
		t.Fatal("live unbound controller adopted governed work")
	}
	changed := *launch
	changed.PolicyVersion = "different-policy"
	if err := f.service.ValidateBackboneControllerBinding(ctx, worker.ID, &changed); err == nil {
		t.Fatal("old policy controller binding accepted")
	}
	changed = *launch
	changed.Model = "provider/other"
	if err := f.service.ValidateBackboneControllerBinding(ctx, worker.ID, &changed); err == nil {
		t.Fatal("changed native model binding accepted")
	}
	if err := f.service.ValidateAgentChange(ctx, worker.ID, domain.InstructorAgentSelection{Harness: "opencode", Model: "provider/other", Effort: "high"}); err == nil {
		t.Fatal("live governed model change accepted")
	}
	path := filepath.Join(workspace, "AGENTS.md")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(original, []byte("\nChanged after enqueue.\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ValidateDispatchAdmission(ctx, worker.ID, "ordinary-queued-turn"); err == nil {
		t.Fatal("queued turn accepted stale document bytes")
	}
	if err := f.service.ValidateResume(ctx, worker.ID); err == nil {
		t.Fatal("resume accepted stale document bytes")
	}
}

func TestBackboneProducerAdmissionRejectsUnsupportedExecution(t *testing.T) {
	workspace, binding := producerWorkspace(t)
	f, worker, work := producerManagedWorker(t, &binding, workspace)
	ctx := context.Background()
	for _, test := range []struct {
		name       string
		mode       domain.SessionMode
		permission ports.PermissionMode
	}{{"TUI", domain.SessionModeTUI, ports.PermissionModeDefault}, {"bypass", domain.SessionModeChat, ports.PermissionModeBypassPermissions}} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := f.service.PrepareBackboneController(ctx, worker.ID, workspace, test.mode, "provider/worker", test.permission); err == nil {
				t.Fatal("unsupported execution admitted")
			}
			governed, err := f.service.BackbonePreflight(ctx, instructorsvc.SpawnInput{ProjectID: "governed", WorkID: work.ID, Role: "worker", Harness: "opencode", Model: "provider/worker", Effort: "high"}, domain.ProjectRecord{ID: "governed"}, test.mode, test.permission)
			if !governed || err == nil {
				t.Fatal("unsupported preflight did not fail closed")
			}
		})
	}
}

func TestBackboneProducerAdmissionRequiresExplicitQualification(t *testing.T) {
	workspace, binding := producerWorkspace(t)
	bound := binding.Contexts["worker"]
	bound.Qualification = nil
	binding.Contexts["worker"] = bound
	f, worker, _ := producerManagedWorker(t, &binding, workspace)
	if _, _, err := f.service.PrepareBackboneController(context.Background(), worker.ID, workspace, domain.SessionModeChat, "provider/worker", ports.PermissionModeDefault); err == nil {
		t.Fatal("producer receipt substituted for missing model qualification")
	}
}

func TestBackboneUnboundControllerPreservesLegacyBehavior(t *testing.T) {
	f, worker, _ := producerManagedWorker(t, nil, "")
	ctx := context.Background()
	launch, prompt, err := f.service.PrepareBackboneController(ctx, worker.ID, "", domain.SessionModeTUI, "provider/worker", ports.PermissionModeBypassPermissions)
	if err != nil || launch != nil || prompt != "" {
		t.Fatalf("absent binding changed legacy behavior: launch=%v prompt=%q error=%v", launch, prompt, err)
	}
	if err := f.service.ValidateBackboneControllerBinding(ctx, worker.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ValidateBackboneControllerBinding(ctx, worker.ID, &ports.ChatBackboneConfig{}); err == nil {
		t.Fatal("governed controller accepted after binding removal")
	}
}

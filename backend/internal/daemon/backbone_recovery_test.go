package daemon

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
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func recoveryJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func recoveryDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func recoveryProducerFixture(t *testing.T) (string, json.RawMessage) {
	t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "policy", "backbone", "testdata", "producer"))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile(filepath.Join(fixture, "workspace.bundle"))
	if err != nil {
		t.Fatal(err)
	}
	if recoveryDigest(bundle) != "69cb616afcc14efc7c25a667a797d4e17a3b1ec6e4e26ea9df75ac896e7709ad" {
		t.Fatal("producer bundle changed")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit := func(cwd string, arguments ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		args := []string{"--no-replace-objects", "--no-lazy-fetch", "-c", "core.hooksPath=" + os.DevNull, "-c", "init.templateDir=", "-C", cwd}
		command := exec.CommandContext(ctx, "git", append(args, arguments...)...)
		for _, entry := range os.Environ() {
			name, _, _ := strings.Cut(entry, "=")
			if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
				command.Env = append(command.Env, entry)
			}
		}
		command.Env = append(command.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
		if data, err := command.CombinedOutput(); err != nil {
			t.Fatalf("local producer Git: %v: %s", err, data)
		}
	}
	runGit(root, "clone", "--no-checkout", "-q", filepath.Join(fixture, "workspace.bundle"), "workspace")
	workspace := filepath.Join(root, "workspace")
	runGit(workspace, "-c", "core.autocrlf=false", "checkout", "-q", "--detach", "c77601c5fcd96188ddeca71b3cc9429a2d692a51")
	receipt, err := os.ReadFile(filepath.Join(fixture, "context.json"))
	if err != nil {
		t.Fatal(err)
	}
	requestData, err := os.ReadFile(filepath.Join(fixture, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		TaskPacket string `json:"task_packet"`
	}
	if err := json.Unmarshal(requestData, &request); err != nil {
		t.Fatal(err)
	}
	adapter, err := os.ReadFile(filepath.Join(workspace, ".engineering-backbone", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]any{}
	for _, name := range []string{"context_loading", "stale_context", "tool_boundaries", "failed_check_reporting", "delegation_context", "resume_context"} {
		cases[name] = map[string]string{"result": "pass", "evidenceRef": "synthetic-recovery-fixture:" + name}
	}
	// Synthetic qualification exercises admission only, never real model claims.
	qualification := recoveryJSON(t, map[string]any{"schemaVersion": "1", "harness": "opencode", "runtimeVersion": "1.19.0", "model": "provider/worker", "role": "implementer", "platform": runtime.GOOS, "interface": "chat", "configurationContract": "ao-opencode-backbone-v1", "observedAt": "2026-10-16T00:00:00Z", "status": "qualified", "evidenceRef": "synthetic-recovery-fixture", "evidenceSha256": strings.Repeat("a", 64), "cases": cases})
	binding := backbone.Binding{SchemaVersion: "1", AdapterSHA256: recoveryDigest(adapter), Contexts: map[string]backbone.RoleBinding{"worker": {Role: "implementer", TaskPacket: request.TaskPacket, Context: receipt, Qualification: qualification}}}
	return workspace, recoveryJSON(t, binding)
}

func TestBackboneControllerRecoveryDefersNativeDiscoveryOnlyForGovernedRole(t *testing.T) {
	workspace, binding := recoveryProducerFixture(t)
	for _, scenario := range []string{"governed-worker", "unmapped-codex-role", "legacy-unbound-worker"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			store := sqlitetest.MustOpen(t)
			validator := &instructorParentValidator{}
			service := instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: validator})
			project := domain.ProjectRecord{ID: "backbone-recovery", Path: workspace, RegisteredAt: time.Now()}
			if err := store.UpsertProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			policy := instructorsvc.DefaultPolicy()
			worker := policy.Roles["worker"]
			worker.Model = "provider/worker"
			worker.EffortByModel = map[string]string{"provider/worker": "high"}
			policy.Roles["worker"] = worker
			if scenario != "legacy-unbound-worker" {
				policy.Backbone = binding
			}
			work, err := service.Register(ctx, instructorsvc.RegisterInput{ProjectID: domain.ProjectID(project.ID), Name: "Backbone recovery", Goal: "Validate controlled recovery", Source: "manual", SourceRef: scenario, Policy: policy})
			if err != nil {
				t.Fatal(err)
			}
			kind, harness, model := domain.KindWorker, domain.HarnessOpenCode, "provider/worker"
			if scenario == "unmapped-codex-role" {
				kind, harness, model = domain.KindOrchestrator, domain.HarnessCodex, "provider/control"
			}
			record, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: domain.ProjectID(project.ID), Kind: kind, Harness: harness, Mode: domain.SessionModeChat, Metadata: domain.SessionMetadata{Model: model, Effort: "high", WorkspacePath: workspace, Permissions: ports.PermissionModeDefault}, CreatedAt: time.Now(), UpdatedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.AttachLaunchingSession(ctx, work.ID, record.ID, ""); err != nil {
				t.Fatal(err)
			}
			guard := instructorGovernance{svc: service, store: store, validator: validator}
			validator.calls = 0
			if err := guard.ValidateControllerRecovery(ctx, record.ID); err != nil {
				t.Fatal(err)
			}
			if scenario == "governed-worker" && validator.calls != 0 {
				t.Fatalf("governed recovery probed native config before controlled boundary: %d calls", validator.calls)
			}
			if scenario != "governed-worker" && validator.calls == 0 {
				t.Fatal("unmapped or unbound role lost its native validation")
			}
			validator.calls = 0
			if err := guard.ValidateResume(ctx, record.ID); err != nil {
				t.Fatal(err)
			}
			if scenario == "governed-worker" && validator.calls != 0 {
				t.Fatalf("governed resume probed native config before controlled boundary: %d calls", validator.calls)
			}
			if scenario != "governed-worker" && validator.calls == 0 {
				t.Fatal("unmapped or unbound resume lost native validation")
			}
		})
	}
}

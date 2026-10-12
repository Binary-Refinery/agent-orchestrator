package sessionmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestSpawnOrchestratorRulesFileReloadsWithoutConfigUpdate(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "orchestrator.md")
	if err := os.WriteFile(file, []byte("FIRST_ORCHESTRATOR_SENTINEL"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := newFakeStore()
	cfg := testRoleAgents()
	cfg.OrchestratorRules = "Inline coordination rule."
	cfg.OrchestratorRulesFile = "orchestrator.md"
	cfg.AgentRulesFile = "worker-file-does-not-exist.md"
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Path: dir, Config: cfg}
	agent := &recordingAgent{}
	m := New(Deps{Runtime: &fakeRuntime{}, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{}, Store: st, Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st}, LookPath: func(string) (string, error) { return "/bin/true", nil }})

	first, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(agent.lastLaunch.SystemPrompt, "Inline coordination rule.\n\nFIRST_ORCHESTRATOR_SENTINEL") {
		t.Fatalf("first launch missing inline + file rules: %s", agent.lastLaunch.SystemPrompt)
	}
	// Change only the repository file. There is deliberately no config PUT or
	// in-memory project update between the two fresh launches.
	if err := os.WriteFile(file, []byte("SECOND_ORCHESTRATOR_SENTINEL"), 0o644); err != nil {
		t.Fatal(err)
	}
	first.IsTerminated = true
	st.sessions[first.ID] = first
	if _, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(agent.lastLaunch.SystemPrompt, "SECOND_ORCHESTRATOR_SENTINEL") || strings.Contains(agent.lastLaunch.SystemPrompt, "FIRST_ORCHESTRATOR_SENTINEL") {
		t.Fatalf("fresh launch did not reload file: %s", agent.lastLaunch.SystemPrompt)
	}
	if agent.lastLaunch.Prompt != "" {
		t.Fatalf("rules became a user task: %q", agent.lastLaunch.Prompt)
	}
}

func TestSpawnWorkerDoesNotConsumeOrchestratorConfiguration(t *testing.T) {
	st := newFakeStore()
	cfg := testRoleAgents()
	cfg.AgentRules = "Worker-only sentinel."
	cfg.OrchestratorRules = "Orchestrator-only sentinel."
	cfg.OrchestratorRulesFile = "missing-orchestrator.md"
	cfg.OrchestratorPrompt = "Initial orchestrator task."
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: cfg}
	agent := &recordingAgent{}
	m := New(Deps{Runtime: &fakeRuntime{}, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{}, Store: st, Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st}, LookPath: func(string) (string, error) { return "/bin/true", nil }})
	if _, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Prompt: "Worker task."}); err != nil {
		t.Fatal(err)
	}
	if agent.lastLaunch.Prompt != "Worker task." || !strings.Contains(agent.lastLaunch.SystemPrompt, "Worker-only sentinel.") || strings.Contains(agent.lastLaunch.SystemPrompt, "Orchestrator-only sentinel.") {
		t.Fatalf("orchestrator config leaked into worker launch: %#v", agent.lastLaunch)
	}
}

func TestSpawnOrchestratorMissingRulesFailsBeforeSessionCreation(t *testing.T) {
	st := newFakeStore()
	cfg := testRoleAgents()
	cfg.OrchestratorRules = "Inline cannot mask a missing file."
	cfg.OrchestratorRulesFile = "missing.md"
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Path: t.TempDir(), Config: cfg}
	agent := &recordingAgent{}
	m := New(Deps{Runtime: &fakeRuntime{}, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{}, Store: st, Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st}, LookPath: func(string) (string, error) { return "/bin/true", nil }})
	_, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator})
	if err == nil || !strings.Contains(err.Error(), "orchestratorRulesFile") || !strings.Contains(err.Error(), "missing.md") {
		t.Fatalf("spawn error = %v, want named missing-rules failure", err)
	}
	if len(st.sessions) != 0 || agent.launchCalls != 0 {
		t.Fatalf("failed rules config created state: sessions=%d launches=%d", len(st.sessions), agent.launchCalls)
	}
}

func TestOrchestratorConfiguredInitialPromptIsCreationOnly(t *testing.T) {
	for _, explicit := range []string{"", "One-off explicit task."} {
		t.Run(explicit, func(t *testing.T) {
			st := newFakeStore()
			cfg := testRoleAgents()
			cfg.OrchestratorPrompt = "Configured startup task."
			cfg.OrchestratorRules = "Standing coordination rule."
			st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: cfg}
			agent := &recordingAgent{}
			m := New(Deps{Runtime: &fakeRuntime{}, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{}, Store: st, Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st}, DataDir: t.TempDir(), LookPath: func(string) (string, error) { return "/bin/true", nil }})
			rec, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator, Prompt: explicit})
			if err != nil {
				t.Fatal(err)
			}
			want := explicit
			if want == "" {
				want = cfg.OrchestratorPrompt
			}
			if agent.lastLaunch.Prompt != want || rec.Metadata.LatestUserPrompt != want {
				t.Fatalf("initial launch prompt = %q, latest task = %q, want %q", agent.lastLaunch.Prompt, rec.Metadata.LatestUserPrompt, want)
			}
			if strings.Contains(agent.lastLaunch.SystemPrompt, want) {
				t.Fatal("initial user task was added to standing rules")
			}
			if rec.Metadata.Prompt != explicit {
				t.Fatalf("fallback replay task = %q, want only explicit task %q", rec.Metadata.Prompt, explicit)
			}
			// An edited config must not inject a new startup task into restore.
			project := st.projects["mer"]
			project.Config.OrchestratorPrompt = "New startup task must not replay."
			st.projects["mer"] = project
			rec.IsTerminated = true
			st.sessions[rec.ID] = rec
			if _, err := m.RestoreWithMode(context.Background(), rec.ID); err != nil {
				t.Fatal(err)
			}
			if agent.lastLaunch.Prompt != explicit {
				t.Fatalf("fallback restore prompt = %q, want legacy explicit task %q", agent.lastLaunch.Prompt, explicit)
			}
		})
	}
}

func TestRestoreOrchestratorReloadsStandingRulesFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.md"), []byte("RESTORED_FILE_SENTINEL"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := newFakeStore()
	cfg := testRoleAgents()
	cfg.OrchestratorRulesFile = "rules.md"
	cfg.OrchestratorPrompt = "Creation task must not appear on restore."
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Path: dir, Config: cfg}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator, IsTerminated: true, Metadata: domain.SessionMetadata{WorkspacePath: "/ws/mer-1", Branch: "b", AgentSessionID: "native-1"}}
	agent := &recordingAgent{}
	m := New(Deps{Runtime: &fakeRuntime{}, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{}, Store: st, Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st}, DataDir: t.TempDir(), LookPath: func(string) (string, error) { return "/bin/true", nil }})
	if _, err := m.RestoreWithMode(context.Background(), "mer-1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(agent.lastRestore.SystemPrompt, "RESTORED_FILE_SENTINEL") || strings.Contains(agent.lastRestore.SystemPrompt, cfg.OrchestratorPrompt) || agent.launchCalls != 0 {
		t.Fatalf("native restore did not use current standing rules exclusively: %#v", agent.lastRestore)
	}
}

func TestChatOrchestratorConfiguredInitialPromptDeliveredOnce(t *testing.T) {
	launcher := &recordingLauncher{}
	m, st, _ := newChatManager(launcher)
	project := st.projects["mer"]
	project.Config.OrchestratorPrompt = "CHAT_CREATION_SENTINEL"
	st.projects["mer"] = project
	rec, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, RequestedMode: domain.SessionModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.turns) != 1 || launcher.turns[0] != "CHAT_CREATION_SENTINEL" || rec.Metadata.Prompt != "" || rec.Metadata.LatestUserPrompt != "CHAT_CREATION_SENTINEL" {
		t.Fatalf("configured Chat startup: turns=%v replay=%q latest=%q", launcher.turns, rec.Metadata.Prompt, rec.Metadata.LatestUserPrompt)
	}
	if err := m.Send(context.Background(), rec.ID, "Only this follow-up.", nil); err != nil {
		t.Fatal(err)
	}
	if len(launcher.relayed) != 1 || launcher.relayed[0] != "Only this follow-up." || len(launcher.turns) != 1 {
		t.Fatalf("send replayed initial configuration: initial=%v sent=%v", launcher.turns, launcher.relayed)
	}
}

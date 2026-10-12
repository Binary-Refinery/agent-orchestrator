package sessionmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type rejectingExecutionBudget struct {
	*launchGovernanceFake
	sessionError error
	turnError    error
	sessionCalls int
	turnCalls    int
	states       map[string]string
}

func (b *rejectingExecutionBudget) ReserveManagedSession(context.Context, domain.ConversationOwner, domain.SessionID) (string, error) {
	b.sessionCalls++
	return "session-token", b.sessionError
}
func (b *rejectingExecutionBudget) ReserveManagedTurn(context.Context, domain.ConversationOwner, string, string) (string, error) {
	b.turnCalls++
	return "turn-token", b.turnError
}
func (b *rejectingExecutionBudget) SetManagedBudgetState(_ context.Context, token, state string) error {
	if b.states == nil {
		b.states = map[string]string{}
	}
	b.states[token] = state
	return nil
}
func (*rejectingExecutionBudget) FinishManagedSession(context.Context, domain.ConversationOwner) error {
	return nil
}
func (b *rejectingExecutionBudget) CancelManagedBudgetBeforeAdmission(_ context.Context, token string) error {
	return b.SetManagedBudgetState(context.Background(), token, "cancelled")
}

func TestInstructorBudgetManagedSpawnRejectsBeforeNativeController(t *testing.T) {
	for _, source := range []string{"manual", "automation", "delegation"} {
		t.Run(source, func(t *testing.T) {
			manager, store, runtime, workspace := newManager()
			guard := &rejectingExecutionBudget{launchGovernanceFake: &launchGovernanceFake{runtime: runtime}, sessionError: domain.ErrInstructorBudgetExceeded}
			manager.SetInstructorGovernance(guard)
			cfg := ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode, Prompt: "Work"}
			if source == "automation" {
				id := domain.AutomationRunID("budget-run")
				cfg.AutomationRunID = &id
			}
			if source == "delegation" {
				cfg.ParentSessionID = "parent"
				cfg.AgentConfig.Permissions = ports.PermissionModeDefault
			}
			if _, _, _, err := manager.Spawn(context.Background(), cfg); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
				t.Fatalf("budget admission=%v", err)
			}
			if runtime.created != 0 || workspace.createCount != 0 || len(store.sessions) != 0 || guard.sessionCalls == 0 {
				t.Fatalf("budget bypass: runtime=%d workspace=%d sessions=%d admissions=%d", runtime.created, workspace.createCount, len(store.sessions), guard.sessionCalls)
			}
		})
	}
}

func TestInstructorBudgetOpeningTUITurnRejectsBeforeRuntimeCreate(t *testing.T) {
	manager, _, runtime, _ := newManager()
	guard := &rejectingExecutionBudget{launchGovernanceFake: &launchGovernanceFake{runtime: runtime}, turnError: domain.ErrInstructorBudgetExceeded}
	manager.SetInstructorGovernance(guard)
	if _, _, _, err := manager.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode, Prompt: "Work"}); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("turn admission=%v", err)
	}
	if runtime.created != 0 || guard.turnCalls != 1 {
		t.Fatalf("turn bypass: runtime=%d admissions=%d", runtime.created, guard.turnCalls)
	}
}

func TestInstructorBudgetTUIPromptRejectsBeforeMessenger(t *testing.T) {
	store := newFakeStore()
	store.sessions["mer-1"] = pastStartupGate(domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessCopilot})
	messenger := &fakeMessenger{}
	manager := New(Deps{Store: store, Messenger: messenger})
	guard := &rejectingExecutionBudget{launchGovernanceFake: &launchGovernanceFake{}, turnError: domain.ErrInstructorBudgetExceeded}
	manager.SetInstructorGovernance(guard)
	if err := manager.Send(context.Background(), "mer-1", "Continue", nil); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("turn admission=%v", err)
	}
	if len(messenger.msgs) != 0 || guard.turnCalls != 1 {
		t.Fatalf("turn bypass: messages=%d admissions=%d", len(messenger.msgs), guard.turnCalls)
	}
}

func TestInstructorBudgetTUISwitchRejectsBeforeSourceStopOrTargetLaunch(t *testing.T) {
	for _, denied := range []string{"session", "turn"} {
		t.Run(denied, func(t *testing.T) {
			runtime := &fakeRestartRuntime{fakeRuntime: &fakeRuntime{}}
			manager, _, messenger := newSwitchTestManager(t, runtime)
			guard := &rejectingExecutionBudget{launchGovernanceFake: &launchGovernanceFake{runtime: runtime.fakeRuntime}}
			if denied == "session" {
				guard.sessionError = domain.ErrInstructorBudgetExceeded
			} else {
				guard.turnError = domain.ErrInstructorBudgetExceeded
			}
			manager.SetInstructorGovernance(guard)
			_, err := switchAgentSynchronously(context.Background(), manager, "proj-1", SwitchAgentConfig{TargetHarness: domain.HarnessCodex, IdempotencyKey: "budget-exhausted-switch"})
			if !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
				t.Fatalf("switch admission=%v", err)
			}
			if runtime.created != 0 || runtime.destroyed != 0 || len(messenger.msgs) != 0 || guard.sessionCalls != 1 {
				t.Fatalf("refusal had effects: creates=%d destroys=%d sends=%d session admissions=%d", runtime.created, runtime.destroyed, len(messenger.msgs), guard.sessionCalls)
			}
			if denied == "turn" && (guard.turnCalls != 1 || guard.states["session-token"] != "cancelled") {
				t.Fatalf("turn admission=%d states=%v", guard.turnCalls, guard.states)
			}
		})
	}
}

func TestInstructorBudgetTUISwitchChargesContinuationOnceAndReplayCreatesNothing(t *testing.T) {
	for _, afterStart := range []bool{false, true} {
		name := "inline"
		if afterStart {
			name = "after_start"
		}
		t.Run(name, func(t *testing.T) {
			runtime := &fakeRestartRuntime{fakeRuntime: &fakeRuntime{}}
			manager, _, messenger := newSwitchTestManager(t, runtime)
			if afterStart {
				target := manager.agents.(switchTestAgents)[domain.HarnessCodex].(*switchTestAgent)
				manager.agents.(switchTestAgents)[domain.HarnessCodex] = &switchAfterStartAgent{switchTestAgent: target}
			}
			guard := &rejectingExecutionBudget{launchGovernanceFake: &launchGovernanceFake{runtime: runtime.fakeRuntime}}
			manager.SetInstructorGovernance(guard)
			cfg := SwitchAgentConfig{TargetHarness: domain.HarnessCodex, IdempotencyKey: "budgeted-switch"}
			first, err := switchAgentSynchronously(context.Background(), manager, "proj-1", cfg)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.created != 1 || guard.sessionCalls != 1 || guard.turnCalls != 1 || guard.states["session-token"] != "started" || guard.states["turn-token"] != "started" {
				t.Fatalf("switch effects: creates=%d session admissions=%d turn admissions=%d states=%v", runtime.created, guard.sessionCalls, guard.turnCalls, guard.states)
			}
			if afterStart && len(messenger.msgs) != 1 {
				t.Fatalf("after-start continuation deliveries=%d", len(messenger.msgs))
			}
			replay, err := switchAgentSynchronously(context.Background(), manager, "proj-1", cfg)
			if err != nil || replay.ID != first.ID || runtime.created != 1 || guard.turnCalls != 1 {
				t.Fatalf("replay=%+v error=%v creates=%d turns=%d", replay, err, runtime.created, guard.turnCalls)
			}
		})
	}
}

func TestInstructorBudgetTUISwitchFencesReplacementBeforeSourceEffects(t *testing.T) {
	runtime := &fakeRestartRuntime{fakeRuntime: &fakeRuntime{}}
	manager, _, _ := newSwitchTestManager(t, runtime)
	guard := &rejectingExecutionBudget{launchGovernanceFake: &launchGovernanceFake{runtime: runtime.fakeRuntime}, states: map[string]string{"session-token": "started"}}
	manager.SetInstructorGovernance(guard)
	runtime.onDestroy = func(call int, _ ports.RuntimeHandle) {
		if call == 0 && (guard.states["session-token"] != "uncertain" || guard.states["turn-token"] != "uncertain") {
			t.Errorf("source stopped before replacement claims: %v", guard.states)
		}
	}
	if _, err := switchAgentSynchronously(context.Background(), manager, "proj-1", SwitchAgentConfig{TargetHarness: domain.HarnessCodex, IdempotencyKey: "source-stop-fence"}); err != nil {
		t.Fatal(err)
	}
}

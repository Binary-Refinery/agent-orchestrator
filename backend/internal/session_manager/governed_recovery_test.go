package sessionmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type controllerRecoveryGovernance struct {
	launchGovernanceFake
	recoveryErr error
	recoveries  int
}

func (g *controllerRecoveryGovernance) ValidateControllerRecovery(context.Context, domain.SessionID) error {
	g.recoveries++
	return g.recoveryErr
}
func recoveryRecord() domain.SessionRecord {
	return domain.SessionRecord{ID: "mer-1", ProjectID: chatTestProject, Kind: domain.KindOrchestrator,
		Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, CreatedAt: time.Unix(50, 0),
		Activity: domain.Activity{State: domain.ActivityExited},
		Metadata: domain.SessionMetadata{Branch: "ao/mer-1/root", WorkspacePath: "/ws/mer-1", ProviderConversationID: "existing-thread", ControllerGeneration: "old-owner", Prompt: "original task"}}
}
func TestGovernedChatRecoveryPreservesNativeIdentityWithoutReplayingPrompt(t *testing.T) {
	for _, operation := range []string{"restart", "resume", "restore"} {
		t.Run(operation, func(t *testing.T) {
			launcher := &recordingLauncher{liveReconnect: operation != "restore"}
			m, st, rt := newChatManager(launcher)
			project := st.projects["mer"]
			project.Config.GovernanceManaged = true
			project.Config.OrchestratorPrompt = "initial task must not replay"
			st.projects["mer"] = project
			rec := recoveryRecord()
			rec.IsTerminated = operation == "restore"
			st.sessions[rec.ID] = rec
			guard := &controllerRecoveryGovernance{launchGovernanceFake: launchGovernanceFake{runtime: rt, resumeError: errors.New("work awaits human gate")}}
			m.SetInstructorGovernance(guard)
			var err error
			switch operation {
			case "restart":
				err = m.reconcileLive(context.Background(), rec)
			case "resume":
				_, err = m.ResumeAgentWithMode(context.Background(), rec.ID)
			case "restore":
				_, err = m.RestoreWithMode(context.Background(), rec.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if guard.recoveries == 0 || len(launcher.started) != 1 || rt.created != 0 {
				t.Fatalf("recovery=%d chat starts=%d native starts=%d", guard.recoveries, len(launcher.started), rt.created)
			}
			start := launcher.started[0]
			if start.HistoryMode != ports.ChatHistoryContinuity || start.ProviderConversationID != rec.Metadata.ProviderConversationID || len(launcher.turns) != 0 || len(launcher.queued) != 0 {
				t.Fatalf("lost native continuity or replayed initial task: mode=%v native=%s turns=%v queued=%v", start.HistoryMode, start.ProviderConversationID, launcher.turns, launcher.queued)
			}
			if got := st.sessions[rec.ID]; got.ID != rec.ID || !got.CreatedAt.Equal(rec.CreatedAt) || got.IsTerminated {
				t.Fatalf("recovery replaced session: %+v", got)
			}
		})
	}
}
func TestGovernedChatRecoveryStillRejectsPolicyConflictBeforeProviderIO(t *testing.T) {
	launcher := &recordingLauncher{}
	m, st, rt := newChatManager(launcher)
	rec := recoveryRecord()
	st.sessions[rec.ID] = rec
	guard := &controllerRecoveryGovernance{launchGovernanceFake: launchGovernanceFake{runtime: rt}, recoveryErr: errors.New("role requires harness opencode, got codex")}
	m.SetInstructorGovernance(guard)
	if _, err := m.ResumeAgentWithMode(context.Background(), rec.ID); !errors.Is(err, ErrInstructorResumeBlocked) || !errors.Is(err, guard.recoveryErr) {
		t.Fatalf("want actionable wrapped governance conflict, got %v", err)
	}
	if len(launcher.started) != 0 || rt.created != 0 || st.sessions[rec.ID].Metadata.ControllerGeneration != rec.Metadata.ControllerGeneration {
		t.Fatal("conflicting recovery reached provider or changed owner")
	}
}

func TestGuidedChatRecoveryRequiresNativeContinuityWithoutManagedProjectFlag(t *testing.T) {
	for _, operation := range []string{"resume", "restore"} {
		t.Run(operation, func(t *testing.T) {
			launcher := &recordingLauncher{}
			m, st, rt := newChatManager(launcher)
			rec := recoveryRecord()
			rec.IsTerminated = operation == "restore"
			st.sessions[rec.ID] = rec
			if st.projects["mer"].Config.GovernanceManaged {
				t.Fatal("fixture must exercise legacy project configuration")
			}
			guard := &controllerRecoveryGovernance{launchGovernanceFake: launchGovernanceFake{runtime: rt, resumeError: errors.New("work awaits human gate")}}
			m.SetInstructorGovernance(guard)
			ctx := domain.WithSessionRecoveryExpectation(context.Background(), rec)
			var err error
			if operation == "restore" {
				_, err = m.RestoreWithMode(ctx, rec.ID)
			} else {
				_, err = m.ResumeAgentWithMode(ctx, rec.ID)
			}
			if err != nil || len(launcher.started) != 1 {
				t.Fatalf("guided recovery failed: starts=%d err=%v", len(launcher.started), err)
			}
			if launcher.started[0].HistoryMode != ports.ChatHistoryContinuity || launcher.started[0].ProviderConversationID != rec.Metadata.ProviderConversationID || rt.created != 0 || len(launcher.turns) != 0 || len(launcher.queued) != 0 {
				t.Fatalf("guided recovery weakened native continuity: %+v", launcher.started[0])
			}
		})
	}
}
func TestGuidedRecoveryRejectsChangedSessionAfterOperationGate(t *testing.T) {
	for _, operation := range []string{"resume", "restore"} {
		for _, change := range []string{"instance", "generation", "provider"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				launcher := &recordingLauncher{}
				m, st, rt := newChatManager(launcher)
				rec := recoveryRecord()
				rec.IsTerminated = operation == "restore"
				ctx := domain.WithSessionRecoveryExpectation(context.Background(), rec)
				switch change {
				case "instance":
					rec.CreatedAt = rec.CreatedAt.Add(time.Second)
				case "generation":
					rec.Metadata.ControllerGeneration = "successor"
				case "provider":
					rec.Metadata.ProviderConversationID = "another-thread"
				}
				st.sessions[rec.ID] = rec
				var err error
				if operation == "restore" {
					_, err = m.RestoreWithMode(ctx, rec.ID)
				} else {
					_, err = m.ResumeAgentWithMode(ctx, rec.ID)
				}
				if !errors.Is(err, domain.ErrSessionRecoveryChanged) || len(launcher.started) != 0 || rt.created != 0 {
					t.Fatalf("stale recovery escaped owner fence: %v", err)
				}
			})
		}
	}
}
func TestControllerRecoveryDoesNotRelaxNativeHandoffExecutionGate(t *testing.T) {
	launcher := &recordingLauncher{}
	m, st, rt := newChatManager(launcher)
	rec := recoveryRecord()
	st.sessions[rec.ID] = rec
	guard := &controllerRecoveryGovernance{launchGovernanceFake: launchGovernanceFake{runtime: rt, resumeError: errors.New("work awaits human gate")}}
	m.SetInstructorGovernance(guard)
	if _, err := m.resumeAgentRecordWithPolicy(context.Background(), "handoff", rec, false, true); !errors.Is(err, guard.resumeError) {
		t.Fatalf("native handoff bypassed execution gate: %v", err)
	}
	if guard.recoveries != 0 || len(launcher.started) != 0 {
		t.Fatal("native handoff used observation-only recovery")
	}
}

package sessionmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type backboneGovernanceFake struct {
	*launchGovernanceFake
	preflightErr      error
	workspaceErr      error
	preparedWorkspace string
	preparedBinding   *ports.ChatBackboneConfig
}

func (f *backboneGovernanceFake) BackbonePreflight(context.Context, ports.SpawnConfig, domain.ProjectRecord, domain.SessionMode) (bool, error) {
	return true, f.preflightErr
}
func (f *backboneGovernanceFake) PrepareBackboneController(_ context.Context, _ domain.SessionID, workspace string, _ domain.SessionMode, _ string, _ ports.PermissionMode) (*ports.ChatBackboneConfig, string, error) {
	f.preparedWorkspace = workspace
	return f.preparedBinding, "", f.workspaceErr
}
func (f *backboneGovernanceFake) ControllerStarted(context.Context, string, domain.SessionID, ports.SpawnConfig) error {
	return nil
}

func TestBackboneRejectedPreflightPrecedesNativeDiscoveryAndDurableState(t *testing.T) {
	chat := &recordingLauncher{}
	manager, store, rt := newChatManager(chat)
	guard := &backboneGovernanceFake{launchGovernanceFake: &launchGovernanceFake{runtime: rt}, preflightErr: errors.New("qualification is missing")}
	manager.SetInstructorGovernance(guard)
	_, _, _, err := manager.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, RequestedMode: domain.SessionModeChat, AgentConfig: ports.AgentConfig{Model: "test/model", Effort: "high"}, Prompt: "approved task"})
	if err == nil || len(chat.preflighted) != 0 || len(chat.started) != 0 || len(store.sessions) != 0 || rt.created != 0 {
		t.Fatalf("preflight escaped: err=%v probes=%v starts=%d rows=%d runtime=%d", err, chat.preflighted, len(chat.started), len(store.sessions), rt.created)
	}
}

func TestBackboneActualWorkspaceFailurePrecedesNativeChatStart(t *testing.T) {
	chat := &recordingLauncher{}
	manager, _, rt := newChatManager(chat)
	manager.dataDir = t.TempDir()
	guard := &backboneGovernanceFake{launchGovernanceFake: &launchGovernanceFake{runtime: rt}, workspaceErr: errors.New("receipt is stale in created workspace")}
	manager.SetInstructorGovernance(guard)
	_, _, _, err := manager.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, RequestedMode: domain.SessionModeChat, AgentConfig: ports.AgentConfig{Model: "test/model", Effort: "high"}, Prompt: "approved task"})
	if err == nil || guard.preparedWorkspace == "" || len(chat.preflighted) != 0 || len(chat.started) != 0 || rt.created != 0 {
		t.Fatalf("actual workspace escaped: err=%v checked=%q probes=%v starts=%d runtime=%d", err, guard.preparedWorkspace, chat.preflighted, len(chat.started), rt.created)
	}
}

func TestBackboneDefaultChatCannotFallBackToTUI(t *testing.T) {
	manager, store, rt := newChatManager(nil)
	manager.defaults = fixedSessionModeDefaults(domain.SessionModeChat)
	guard := &backboneGovernanceFake{launchGovernanceFake: &launchGovernanceFake{runtime: rt}}
	manager.SetInstructorGovernance(guard)
	_, _, _, err := manager.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, AgentConfig: ports.AgentConfig{Model: "test/model", Effort: "high"}, Prompt: "approved task"})
	if !errors.Is(err, ports.ErrChatUnsupported) || len(store.sessions) != 0 || rt.created != 0 || guard.preparedWorkspace != "" {
		t.Fatalf("governed default Chat escaped into TUI: err=%v rows=%d runtime=%d workspace=%q", err, len(store.sessions), rt.created, guard.preparedWorkspace)
	}
}

func TestBackboneTUIToChatHandoffCannotAdoptUncontrolledHistory(t *testing.T) {
	manager, store, rt, _, events := newTransitionManager(t, domain.SessionModeTUI)
	guard := &backboneGovernanceFake{launchGovernanceFake: &launchGovernanceFake{runtime: rt.fakeRuntime}, preparedBinding: &ports.ChatBackboneConfig{}}
	manager.SetInstructorGovernance(guard)
	_, err := manager.StartInterfaceTransition(context.Background(), "session-1", domain.SessionModeChat, domain.SessionInterfaceTransitionDrain, domain.SessionInterfaceTransitionHistoryStrict)
	if !errors.Is(err, ErrInterfaceHandoffUnsupported) || len(store.transitions) != 0 || len(*events) != 0 || rt.created != 0 {
		t.Fatalf("governed TUI handoff escaped: err=%v transitions=%d events=%v runtime=%d", err, len(store.transitions), *events, rt.created)
	}
}

package chat_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestProviderFailureSurvivesNativeHistoryRestart(t *testing.T) {
	const originalError = "ACP session/prompt: invalid parameters (HTTP 400)"
	for _, scenario := range []struct {
		name        string
		state       domain.TurnState
		replayError error
		wantState   domain.TurnState
		wantError   string
	}{
		{name: "recovered preserves failure", state: domain.TurnStateRecovered, wantState: domain.TurnStateFailed, wantError: originalError},
		{name: "explicit replay failure wins", state: domain.TurnStateRecovered, replayError: errors.New("new provider detail"), wantState: domain.TurnStateFailed, wantError: "new provider detail"},
		{name: "known completion upgrades failure", state: domain.TurnStateCompleted, wantState: domain.TurnStateCompleted},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t)
			turn, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "read-only diagnostic", ClientMessageID: "failed-before-restart"})
			if err != nil {
				t.Fatal(err)
			}
			h.conv.emit(ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderEventID: "live-provider-failure", ProviderTurnID: turn.ProviderTurnID, TurnState: domain.TurnStateFailed, Err: errors.New(originalError)})
			h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
				return len(s.Turns) == 1 && s.Turns[0].ErrorMessage == originalError
			})
			if err := h.svc.Stop(ctx, testSession); err != nil {
				t.Fatal(err)
			}

			provider := &nativeHistoryConversation{fakeConversation: newFakeConversation(), events: []ports.ChatEvent{{Kind: ports.ChatEventTurnCompleted, ProviderEventID: "native-history-completion", ProviderTurnID: turn.ProviderTurnID, TurnState: scenario.state, Err: scenario.replayError}}}
			restarted := chatsvc.New(chatsvc.Options{Store: h.st, Sessions: h.st, Reader: fullSnapshotReader(h.st), Drivers: fakeRegistry{driver: fakeDriver{conv: provider}}, Log: slog.New(slog.DiscardHandler), NewID: uuid.NewString})
			t.Cleanup(func() { restarted.StopAll(ctx) })
			ctrl, err := restarted.Start(ctx, chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(), ProviderConversationID: h.conv.ProviderConversationID(), HistoryMode: ports.ChatHistoryContinuity})
			if err != nil {
				t.Fatal(err)
			}
			if ctrl.ConversationID() != h.ctrl.ConversationID() || provider.historyReads() == 0 {
				t.Fatal("restart did not replay the existing conversation")
			}
			snapshot, err := h.st.LoadConversationSnapshot(ctx, ctrl.ConversationID())
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Turns) != 1 || snapshot.Turns[0].ID != turn.ID || snapshot.Turns[0].State != scenario.wantState || snapshot.Turns[0].ErrorMessage != scenario.wantError {
				t.Fatalf("replayed turns = %+v, want original identity and %s/%q", snapshot.Turns, scenario.wantState, scenario.wantError)
			}
			if provider.sendCallCount() != 0 {
				t.Fatal("history recovery re-executed the failed prompt")
			}
		})
	}
}

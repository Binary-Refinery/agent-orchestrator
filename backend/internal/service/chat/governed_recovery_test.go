package chat_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

type pausedRecoveryBudget struct {
	paused   atomic.Bool
	deferred chan struct{}
}

func (*pausedRecoveryBudget) ReserveManagedSession(context.Context, domain.ConversationOwner, domain.SessionID) (string, error) {
	return "", nil
}
func (g *pausedRecoveryBudget) ReserveManagedTurn(context.Context, domain.ConversationOwner, string, string) (string, error) {
	if g.paused.Load() {
		select {
		case g.deferred <- struct{}{}:
		default:
		}
		return "", ports.ErrSemanticDeliveryDeferred
	}
	return "", nil
}
func (*pausedRecoveryBudget) SetManagedBudgetState(context.Context, string, string) error { return nil }
func (*pausedRecoveryBudget) FinishManagedSession(context.Context, domain.ConversationOwner) error {
	return nil
}
func (*pausedRecoveryBudget) CancelManagedBudgetBeforeAdmission(context.Context, string) error {
	return nil
}

// A restart may replay a completion event from a still-running provider. It
// restores observation, but must not turn a paused work's queue into execution.
func TestGovernedLiveRecoveryKeepsQueuePausedAcrossCompletionReplay(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	lcm := lifecycle.New(st, nil)
	firstProvider := &terminatingConversation{fakeConversation: newFakeConversation()}
	first := chatsvc.New(chatsvc.Options{Store: st, Reader: fullSnapshotReader(st), Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: firstProvider}}, NewID: uuid.NewString, Log: slog.New(slog.DiscardHandler)})
	firstController, err := first.Start(ctx, chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex,
		ControllerReady: func(r chatsvc.StartResult) (chatsvc.ControllerCommit, error) {
			return chatsvc.ControllerCommit{}, lcm.MarkSpawned(ctx, testSession, domain.SessionMetadata{ProviderConversationID: r.ProviderConversationID, ControllerGeneration: r.ControllerGeneration})
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.StopAll(ctx) })
	running, err := firstController.Send(ctx, ports.ChatUserMessage{Text: "already admitted work"})
	if err != nil {
		t.Fatal(err)
	}
	firstProvider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: running.ProviderTurnID, ProviderConversationID: "thread-1"})
	h := &harness{st: st, ctrl: firstController}
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		for _, turn := range s.Turns {
			if turn.ID == running.ID {
				return turn.State == domain.TurnStateRunning
			}
		}
		return false
	})
	queued, err := firstController.Send(ctx, ports.ChatUserMessage{Text: "work queued before pause"})
	if err != nil || queued.State != domain.TurnStateQueued {
		t.Fatalf("queue=%+v err=%v", queued, err)
	}
	first.StopAll(ctx)
	before, _, err := st.GetSession(ctx, testSession)
	if err != nil {
		t.Fatal(err)
	}
	// A prior native Stop checkpoint is irrelevant to reconnecting this exact
	// active provider. Its replay verifier may be unavailable during an update.
	before.Metadata.NativeCheckpointEvidence = "previous-native-stop-proof"
	if err := st.UpdateSession(ctx, before); err != nil {
		t.Fatal(err)
	}
	native := &nativeHistoryConversation{fakeConversation: newFakeConversation(), err: ports.ErrChatHistoryUnsettled}
	native.turnSeq = 1 // same persistent provider already accepted the first turn
	provider := &liveReconnectedConversation{nativeHistoryConversation: native}
	budget := &pausedRecoveryBudget{deferred: make(chan struct{}, 2)}
	budget.paused.Store(true)
	second := chatsvc.New(chatsvc.Options{Store: st, Reader: fullSnapshotReader(st), Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: provider}}, NewID: uuid.NewString, Log: slog.New(slog.DiscardHandler)})
	second.SetExecutionBudgetGuard(budget)
	t.Cleanup(func() { second.StopAll(ctx) })
	controller, err := second.Start(ctx, chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, ProviderConversationID: "thread-1", HistoryMode: ports.ChatHistoryContinuity,
		ControllerReady: func(r chatsvc.StartResult) (chatsvc.ControllerCommit, error) {
			if !r.LiveReconnect {
				t.Fatal("recovery lost live ownership")
			}
			return chatsvc.ControllerCommit{}, lcm.MarkChatReconnected(ctx, testSession, domain.SessionMetadata{ProviderConversationID: r.ProviderConversationID, ControllerGeneration: r.ControllerGeneration})
		}})
	if err != nil {
		t.Fatal(err)
	}
	if controller.ConversationID() != firstController.ConversationID() || native.historyReads() != 0 {
		t.Fatal("live recovery replaced history or waited for settled replay")
	}
	after, _, err := st.GetSession(ctx, testSession)
	if err != nil {
		t.Fatal(err)
	}
	if after.CreatedAt != before.CreatedAt || after.Metadata.ProviderConversationID != before.Metadata.ProviderConversationID || after.Metadata.ControllerGeneration == before.Metadata.ControllerGeneration {
		t.Fatalf("recovery identity/fence invalid: before=%+v after=%+v", before, after)
	}
	provider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderEventID: "completion-during-restart", ProviderTurnID: running.ProviderTurnID, TurnState: domain.TurnStateCompleted})
	select {
	case <-budget.deferred:
	case <-time.After(3 * time.Second):
		t.Fatal("replayed completion did not reach queued admission fence")
	}
	kept, err := st.TurnByID(ctx, queued.ID)
	if err != nil || kept.State != domain.TurnStateQueued || kept.ProviderTurnID != "" {
		t.Fatalf("pause lost queued work: %+v err=%v", kept, err)
	}
	provider.mu.Lock()
	calls := provider.sendCalls
	provider.mu.Unlock()
	if calls != 0 {
		t.Fatalf("paused recovery sent %d native turns", calls)
	}
	budget.paused.Store(false) // stands for a subsequent valid human grant
	if err := second.DrainQueued(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	if err := second.DrainQueued(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	calls = provider.sendCalls
	provider.mu.Unlock()
	if calls != 1 {
		t.Fatalf("admitted queue sent %d times, want exactly once", calls)
	}
}

func TestGovernedRecoveryDoesNotAdoptUncommittedLiveOwner(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	provider := &liveReconnectedConversation{nativeHistoryConversation: &nativeHistoryConversation{fakeConversation: newFakeConversation()}}
	service := chatsvc.New(chatsvc.Options{Store: st, Reader: fullSnapshotReader(st), Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: provider}}, NewID: uuid.NewString})
	controller, err := service.Start(ctx, chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, ProviderConversationID: "thread-1", HistoryMode: ports.ChatHistoryContinuity})
	if !errors.Is(err, ports.ErrChatRecoveryInconclusive) || controller != nil || service.HasLiveChatController(testSession) {
		t.Fatalf("uncommitted owner adopted: controller=%v err=%v", controller, err)
	}
	rec, _, err := st.GetSession(ctx, testSession)
	if err != nil || rec.Metadata.ControllerGeneration != "" {
		t.Fatalf("uncertain recovery changed owner: %+v %v", rec, err)
	}
}

func TestGovernedRecoveryRequiresNativeHistoryAfterProviderDeath(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	provider := &nativeHistoryConversation{fakeConversation: newFakeConversation(), err: ports.ErrChatHistoryUnavailable}
	var freshStarts atomic.Int32
	service := chatsvc.New(chatsvc.Options{Store: st, Reader: fullSnapshotReader(st), Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{
		start: func(ports.ChatStartConfig) (ports.ChatConversation, error) {
			freshStarts.Add(1)
			return nil, errors.New("must not start fresh")
		},
		resume: func(ports.ChatResumeConfig) (ports.ChatConversation, error) { return provider, nil }}}, NewID: uuid.NewString})
	controller, err := service.Start(ctx, chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, ProviderConversationID: "thread-1", HistoryMode: ports.ChatHistoryContinuity})
	if !errors.Is(err, ports.ErrChatHistoryUnavailable) || controller != nil || freshStarts.Load() != 0 || service.HasLiveChatController(testSession) {
		t.Fatalf("lost-history recovery launched: controller=%v fresh=%d err=%v", controller, freshStarts.Load(), err)
	}
	if provider.historyReads() != 1 {
		t.Fatalf("required native history reads=%d", provider.historyReads())
	}
}

func TestGovernedRecoveryRequiresSavedNativeIdentityBeforeProviderIO(t *testing.T) {
	st := openStore(t)
	var calls atomic.Int32
	driver := fakeDriver{start: func(ports.ChatStartConfig) (ports.ChatConversation, error) {
		calls.Add(1)
		return nil, errors.New("fresh start forbidden")
	}}
	svc := chatsvc.New(chatsvc.Options{Store: st, Reader: fullSnapshotReader(st), Sessions: st, Drivers: fakeRegistry{driver: driver}, NewID: uuid.NewString})
	controller, err := svc.Start(context.Background(), chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, HistoryMode: ports.ChatHistoryContinuity})
	if !errors.Is(err, ports.ErrChatResumeFailed) || controller != nil || calls.Load() != 0 {
		t.Fatalf("recovery fell back to fresh conversation: controller=%v calls=%d err=%v", controller, calls.Load(), err)
	}
}

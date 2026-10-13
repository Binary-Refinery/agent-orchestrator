package chat_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

type recoveryQueueFixture struct {
	st              *sqlite.Store
	conversationID  string
	running, queued domain.ConversationTurn
	before          store.ConversationSnapshot
}

func newRecoveryQueueFixture(t *testing.T) recoveryQueueFixture {
	t.Helper()
	ctx := context.Background()
	st := openStore(t)
	lcm := lifecycle.New(st, nil)
	provider := &terminatingConversation{fakeConversation: newFakeConversation()}
	service := chatsvc.New(chatsvc.Options{Store: st, Reader: fullSnapshotReader(st), Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: provider}}, NewID: uuid.NewString, Log: slog.New(slog.DiscardHandler)})
	t.Cleanup(func() { service.StopAll(ctx) })
	controller, err := service.Start(ctx, chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex,
		ControllerReady: func(r chatsvc.StartResult) (chatsvc.ControllerCommit, error) {
			return chatsvc.ControllerCommit{}, lcm.MarkSpawned(ctx, testSession, domain.SessionMetadata{ProviderConversationID: r.ProviderConversationID, ControllerGeneration: r.ControllerGeneration})
		}})
	if err != nil {
		t.Fatal(err)
	}
	running, err := controller.Send(ctx, ports.ChatUserMessage{Text: "admitted before provider loss", ClientMessageID: "recovery-running"})
	if err != nil {
		t.Fatal(err)
	}
	provider.emit(
		ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: running.ProviderTurnID, ProviderConversationID: "thread-1"},
		ports.ChatEvent{Kind: ports.ChatEventApprovalRequested, ProviderTurnID: running.ProviderTurnID, ProviderItemID: "old-approval-item", RequestID: "old-approval", Summary: "Native approval still pending", Decisions: []ports.ChatDecisionOption{{ID: "accept", Label: "Approve", Kind: ports.ChatDecisionAllowOnce}}},
		ports.ChatEvent{Kind: ports.ChatEventInputRequested, ProviderTurnID: running.ProviderTurnID, ProviderItemID: "old-input-item", RequestID: "old-input", Input: &ports.ChatInputRequest{Mode: ports.ChatInputModeForm, Message: "Native input still pending", Schema: map[string]any{"type": "object"}}},
	)
	h := &harness{st: st, ctrl: controller}
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return len(s.Turns) == 1 && s.Turns[0].State == domain.TurnStateRunning && len(s.Activities) == 2 && s.Activities[0].Status == domain.ActivityStatusPending && s.Activities[1].Status == domain.ActivityStatusPending
	})
	queued, err := controller.Send(ctx, ports.ChatUserMessage{Text: "never dispatched before provider loss", ClientMessageID: "recovery-queued"})
	if err != nil || queued.State != domain.TurnStateQueued {
		t.Fatalf("queue=%+v err=%v", queued, err)
	}
	service.StopAll(ctx) // Detach preserves prior durable work; replacement is not a live reconnect.
	before, err := st.LoadConversationSnapshot(ctx, controller.ConversationID())
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Turns) != 2 || before.Turns[0].State != domain.TurnStateRunning || before.Turns[1].State != domain.TurnStateQueued {
		t.Fatalf("fixture lost running/queued rows on detach: %+v", before.Turns)
	}
	return recoveryQueueFixture{st: st, conversationID: controller.ConversationID(), running: running, queued: queued, before: before}
}

func (f recoveryQueueFixture) service(t *testing.T, provider *nativeHistoryConversation) *chatsvc.Service {
	t.Helper()
	service := chatsvc.New(chatsvc.Options{Store: f.st, Reader: fullSnapshotReader(f.st), Sessions: f.st, Drivers: fakeRegistry{driver: fakeDriver{conv: provider}}, NewID: uuid.NewString, Log: slog.New(slog.DiscardHandler)})
	t.Cleanup(func() { service.StopAll(context.Background()) })
	return service
}

func (f recoveryQueueFixture) startConfig() chatsvc.StartConfig {
	return chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, ProviderConversationID: "thread-1", HistoryMode: ports.ChatHistoryContinuity,
		ControllerReady: func(r chatsvc.StartResult) (chatsvc.ControllerCommit, error) {
			return chatsvc.ControllerCommit{}, lifecycle.New(f.st, nil).MarkSpawned(context.Background(), testSession, domain.SessionMetadata{ProviderConversationID: r.ProviderConversationID, ControllerGeneration: r.ControllerGeneration})
		}}
}

func TestGovernedRecoveryFailurePreservesQueueAndNativeRequests(t *testing.T) {
	commitErr := errors.New("ownership commit refused")
	for _, tc := range []struct {
		name                  string
		historyErr, commitErr error
	}{
		{"missing history", ports.ErrChatHistoryUnavailable, nil},
		{"controller commit failure", nil, commitErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRecoveryQueueFixture(t)
			provider := &nativeHistoryConversation{fakeConversation: newFakeConversation(), err: tc.historyErr}
			service := f.service(t, provider)
			cfg := f.startConfig()
			if tc.commitErr != nil {
				cfg.ControllerReady = func(chatsvc.StartResult) (chatsvc.ControllerCommit, error) {
					return chatsvc.ControllerCommit{}, tc.commitErr
				}
			}
			controller, err := service.Start(context.Background(), cfg)
			wantErr := tc.historyErr
			if wantErr == nil {
				wantErr = tc.commitErr
			}
			if !errors.Is(err, wantErr) || controller != nil || service.HasLiveChatController(testSession) {
				t.Fatalf("failed recovery published controller=%v error=%v", controller, err)
			}
			after, err := f.st.LoadConversationSnapshot(context.Background(), f.conversationID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after.Turns, f.before.Turns) || !reflect.DeepEqual(after.Messages, f.before.Messages) || !reflect.DeepEqual(after.Activities, f.before.Activities) {
				t.Fatalf("failed recovery changed durable queue/running work or pending requests: before=%+v after=%+v", f.before, after)
			}
			if provider.sendCalls != 0 {
				t.Fatalf("failed recovery sent %d native turns", provider.sendCalls)
			}
		})
	}
}

func TestGovernedDeadProviderRecoveryKeepsUnsentQueueUntilAdmission(t *testing.T) {
	ctx := context.Background()
	f := newRecoveryQueueFixture(t)
	provider := &nativeHistoryConversation{fakeConversation: newFakeConversation()}
	provider.turnSeq = 1
	budget := &pausedRecoveryBudget{deferred: make(chan struct{}, 1)}
	budget.paused.Store(true)
	service := f.service(t, provider)
	service.SetExecutionBudgetGuard(budget)
	controller, err := service.Start(ctx, f.startConfig())
	if err != nil {
		t.Fatal(err)
	}
	if controller.ConversationID() != f.conversationID || controller.ProviderConversationID() != "thread-1" || provider.historyReads() != 1 {
		t.Fatal("recovery did not verify the existing native conversation")
	}
	after, err := f.st.LoadConversationSnapshot(ctx, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Messages, f.before.Messages) {
		t.Fatal("recovery lost or replaced prior prompts")
	}
	for _, turn := range after.Turns {
		switch turn.ID {
		case f.running.ID:
			if turn.State != domain.TurnStateFailed {
				t.Fatalf("dead native turn became %s", turn.State)
			}
		case f.queued.ID:
			if turn.State != domain.TurnStateQueued || turn.ProviderTurnID != "" {
				t.Fatalf("unsent turn changed: %+v", turn)
			}
		default:
			t.Fatalf("recovery manufactured a turn: %+v", turn)
		}
	}
	if len(after.Turns) != 2 || len(after.Activities) != 2 {
		t.Fatal("recovery discarded retained work")
	}
	for _, activity := range after.Activities {
		if activity.Status != domain.ActivityStatusFailed || (activity.RequestID != "old-approval" && activity.RequestID != "old-input") {
			t.Fatalf("dead provider request not retired safely: %+v", activity)
		}
	}
	if err := service.DrainQueued(ctx, testSession); !errors.Is(err, ports.ErrSemanticDeliveryDeferred) {
		t.Fatalf("paused drain error=%v", err)
	}
	kept, err := f.st.TurnByID(ctx, f.queued.ID)
	if err != nil || kept.State != domain.TurnStateQueued || kept.ProviderTurnID != "" {
		t.Fatalf("paused queue changed: %+v error=%v", kept, err)
	}
	provider.mu.Lock()
	calls := provider.sendCalls
	provider.mu.Unlock()
	if calls != 0 {
		t.Fatalf("recovery sent %d initial/queued prompts without admission", calls)
	}
	budget.paused.Store(false)
	if err := service.DrainQueued(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	if err := service.DrainQueued(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	calls = provider.sendCalls
	sent := append([]ports.ChatUserMessage(nil), provider.sent...)
	provider.mu.Unlock()
	if calls != 1 || len(sent) != 1 || sent[0].Text != "never dispatched before provider loss" || sent[0].ClientMessageID != "recovery-queued" {
		t.Fatalf("queue was replayed/replaced: calls=%d sent=%+v", calls, sent)
	}
}

func TestGovernedDeadProviderRecoveryNeverReplaysUncertainBudgetClaim(t *testing.T) {
	ctx := context.Background()
	f := newRecoveryQueueFixture(t)
	instructor := instructorsvc.New(instructorsvc.Deps{Store: f.st})
	work, err := instructor.Register(ctx, instructorsvc.RegisterInput{ProjectID: testProject, Name: "Uncertain queued delivery", Goal: "Retain native ambiguity", Source: "test", SourceRef: "recovery-uncertain-queue", Policy: instructorsvc.DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	if err := instructor.AttachSession(ctx, work.ID, testSession); err != nil {
		t.Fatal(err)
	}
	instance, found, err := f.st.InstructorSessionInstance(ctx, testSession)
	if err != nil || !found {
		t.Fatalf("session instance=%q found=%t error=%v", instance, found, err)
	}
	budget := persistedChatBudget{store: f.st, service: instructor, workID: work.ID, resource: "session:" + instance}
	token, err := budget.ReserveManagedTurn(ctx, domain.SessionConversationOwner(testSession), f.queued.ID, "recovery-queued")
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.SetManagedBudgetState(ctx, token, "uncertain"); err != nil {
		t.Fatal(err)
	}
	provider := &nativeHistoryConversation{fakeConversation: newFakeConversation()}
	service := f.service(t, provider)
	service.SetExecutionBudgetGuard(budget)
	if _, err := service.Start(ctx, f.startConfig()); err != nil {
		t.Fatal(err)
	}
	if err := service.DrainQueued(ctx, testSession); !errors.Is(err, instructorsvc.ErrDeliveryUncertain) {
		t.Fatalf("ambiguous claim was not fenced: %v", err)
	}
	if err := service.DrainQueued(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	calls := provider.sendCalls
	provider.mu.Unlock()
	if calls != 0 {
		t.Fatalf("uncertain pre-crash native delivery replayed %d times", calls)
	}
	usage, err := instructor.BudgetUsage(ctx, work.ID)
	if err != nil || usage.Turns != 1 {
		t.Fatalf("recovery reset or duplicated durable budget usage: %+v error=%v", usage, err)
	}
}

func TestGovernedProjectConversationRecoversDeadProviderWithQueuedInput(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	provider := &terminatingConversation{fakeConversation: newFakeConversation()}
	service := chatsvc.New(chatsvc.Options{Store: st, Reader: fullSnapshotReader(st), Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: provider}}, NewID: uuid.NewString, Log: slog.New(slog.DiscardHandler)})
	t.Cleanup(func() { service.StopAll(ctx) })
	cfg := chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex,
		ControllerReady: func(r chatsvc.StartResult) (chatsvc.ControllerCommit, error) {
			return chatsvc.ControllerCommit{}, lifecycle.New(st, nil).MarkSpawned(ctx, testSession, domain.SessionMetadata{ProviderConversationID: r.ProviderConversationID, ControllerGeneration: r.ControllerGeneration})
		}}
	controller, err := service.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	running, err := controller.Send(ctx, ports.ChatUserMessage{Text: "in flight before provider loss", ClientMessageID: "project-running"})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := controller.Send(ctx, ports.ChatUserMessage{Text: "retain the next orchestrator instruction", ClientMessageID: "project-queued"})
	if err != nil || queued.State != domain.TurnStateQueued {
		t.Fatalf("queue=%+v err=%v", queued, err)
	}
	service.StopAll(ctx)
	before, err := st.LoadConversationSnapshot(ctx, controller.ConversationID())
	if err != nil || before.Conversation.Scope != domain.ConversationScopeProject {
		t.Fatalf("expected project conversation: %+v, %v", before.Conversation, err)
	}

	replacement := &nativeHistoryConversation{fakeConversation: newFakeConversation()}
	f := recoveryQueueFixture{st: st}
	recoveredService := f.service(t, replacement)
	budget := &pausedRecoveryBudget{deferred: make(chan struct{}, 1)}
	budget.paused.Store(true)
	recoveredService.SetExecutionBudgetGuard(budget)
	cfg.ProviderConversationID = "thread-1"
	cfg.HistoryMode = ports.ChatHistoryContinuity
	recovered, err := recoveredService.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ConversationID() != controller.ConversationID() || replacement.historyReads() != 1 {
		t.Fatal("project recovery changed conversation or skipped native history")
	}
	after, err := st.LoadConversationSnapshot(ctx, recovered.ConversationID())
	if err != nil || !reflect.DeepEqual(after.Messages, before.Messages) {
		t.Fatalf("project recovery changed prior prompts: %+v, %v", after.Messages, err)
	}
	retired, err := st.TurnByID(ctx, running.ID)
	if err != nil || retired.State != domain.TurnStateFailed {
		t.Fatalf("old native turn not safely retired: %+v, %v", retired, err)
	}
	retained, err := st.TurnByID(ctx, queued.ID)
	if err != nil || retained.State != domain.TurnStateQueued || retained.ProviderTurnID != "" {
		t.Fatalf("unsent instruction changed: %+v, %v", retained, err)
	}
	if err := recoveredService.DrainQueued(ctx, testSession); !errors.Is(err, ports.ErrSemanticDeliveryDeferred) {
		t.Fatalf("paused project input was admitted: %v", err)
	}
	replacement.mu.Lock()
	calls := replacement.sendCalls
	replacement.mu.Unlock()
	if calls != 0 {
		t.Fatalf("recovery replayed %d prompts", calls)
	}
}

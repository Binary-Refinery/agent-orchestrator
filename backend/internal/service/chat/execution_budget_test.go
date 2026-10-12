package chat_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type persistedChatBudget struct {
	store    *sqlite.Store
	service  *instructorsvc.Service
	workID   string
	resource string
}

func (b persistedChatBudget) ReserveManagedSession(ctx context.Context, _ domain.ConversationOwner, _ domain.SessionID) (string, error) {
	entry, err := b.service.ReserveBudget(ctx, b.workID, "session", b.resource, testSession, "", false)
	return entry.ID, err
}
func (b persistedChatBudget) ReserveManagedTurn(ctx context.Context, _ domain.ConversationOwner, id, _ string) (string, error) {
	entry, err := b.service.ReserveBudget(ctx, b.workID, "turn", b.resource+"/turn:"+id, testSession, "", false)
	if err == nil && entry.State != "reserved" {
		return "", instructorsvc.ErrDeliveryUncertain
	}
	return entry.ID, err
}
func (b persistedChatBudget) SetManagedBudgetState(ctx context.Context, token, state string) error {
	return b.service.SetBudgetState(ctx, token, state)
}
func (b persistedChatBudget) FinishManagedSession(ctx context.Context, _ domain.ConversationOwner) error {
	return b.store.FinishInstructorSessionBudgets(ctx, b.resource, time.Now())
}
func (b persistedChatBudget) CancelManagedBudgetBeforeAdmission(ctx context.Context, token string) error {
	return b.store.CancelInstructorBudgetBeforeAdmission(ctx, token, time.Now())
}

func budgetChat(t *testing.T) (*chatsvc.Service, *fakeConversation, persistedChatBudget) {
	t.Helper()
	store := openStore(t)
	instructor := instructorsvc.New(instructorsvc.Deps{Store: store})
	policy := instructorsvc.DefaultPolicy()
	policy.Budgets = &domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 1, MaxTurns: 1}
	work, err := instructor.Register(context.Background(), instructorsvc.RegisterInput{ProjectID: testProject, Name: "Budgeted chat", Goal: "One actual turn", Source: "test", SourceRef: "budget-chat", Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if err := instructor.AttachSession(context.Background(), work.ID, testSession); err != nil {
		t.Fatal(err)
	}
	instance, found, err := store.InstructorSessionInstance(context.Background(), testSession)
	if err != nil || !found {
		t.Fatalf("instance=%s found=%v error=%v", instance, found, err)
	}
	budget := persistedChatBudget{store: store, service: instructor, workID: work.ID, resource: "session:" + instance}
	provider := newFakeConversation()
	service := chatsvc.New(chatsvc.Options{Store: store, Sessions: store, Reader: fullSnapshotReader(store), NewID: uuid.NewString, Drivers: fakeRegistry{driver: fakeDriver{conv: provider}}, Log: slog.New(slog.DiscardHandler)})
	service.SetExecutionBudgetGuard(budget)
	if _, err := service.Start(context.Background(), chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Stop(context.Background(), testSession) })
	return service, provider, budget
}

func TestInstructorBudgetChatQueuePromotionStopsBeforeNativeSecondTurn(t *testing.T) {
	service, provider, budget := budgetChat(t)
	ctx := context.Background()
	first, err := service.Send(ctx, testSession, ports.ChatUserMessage{Text: "first", ClientMessageID: "first", AuthoredByUser: true})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := service.Send(ctx, testSession, ports.ChatUserMessage{Text: "first", ClientMessageID: "first", AuthoredByUser: true}); err != nil || duplicate.ID != "" {
		t.Fatalf("duplicate=%+v error=%v", duplicate, err)
	}
	reserveBudgetSemanticChat(t, budget, "second")
	queued, err := service.Send(ctx, testSession, ports.ChatUserMessage{Text: "second", ClientMessageID: "second", AuthoredByUser: true})
	if err != nil || queued.State != domain.TurnStateQueued {
		t.Fatalf("queue=%+v error=%v", queued, err)
	}
	before, err := budget.service.BudgetUsage(ctx, budget.workID)
	if err != nil || before.Turns != 1 {
		t.Fatalf("queued message spent turn: %+v error=%v", before, err)
	}
	provider.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderEventID: "budget-first-completed", ProviderTurnID: first.ProviderTurnID, TurnState: domain.TurnStateCompleted}
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		turn, err := budget.store.TurnByID(ctx, queued.ID)
		if err != nil {
			t.Fatal(err)
		}
		if turn.State == domain.TurnStateCancelled {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("queued turn never reflected budget refusal: %+v", turn)
		case <-ticker.C:
		}
	}
	provider.mu.Lock()
	sent := provider.sendCalls
	provider.mu.Unlock()
	if sent != 1 {
		t.Fatalf("native turn budget bypassed: sendCalls=%d", sent)
	}
	if _, err := service.RelaySemanticChatTurn(ctx, testSession, "second", "second"); !errors.Is(err, ports.ErrSemanticDeliveryRejected) {
		t.Fatalf("queued budget refusal became an uncertain gate: %v", err)
	}
	if err := service.ReconcileSemanticChatTurn(ctx, testSession, "second", false); err != nil {
		t.Fatalf("queued budget refusal cannot reconcile: %v", err)
	}
	usage, err := budget.service.BudgetUsage(ctx, budget.workID)
	if err != nil || usage.Turns != 1 {
		t.Fatalf("usage=%+v error=%v", usage, err)
	}
}

func TestInstructorBudgetChatRetryConsumesFreshTurnAndKeepsOriginal(t *testing.T) {
	service, provider, budget := budgetChat(t)
	ctx := context.Background()
	first, err := service.Send(ctx, testSession, ports.ChatUserMessage{Text: "first", ClientMessageID: "first", Origin: domain.MessageOriginHuman, AuthoredByUser: true})
	if err != nil {
		t.Fatal(err)
	}
	provider.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderEventID: "budget-first-failed", ProviderTurnID: first.ProviderTurnID, TurnState: domain.TurnStateFailed}
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		turn, err := budget.store.TurnByID(ctx, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		if turn.State == domain.TurnStateFailed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("native failure was not projected")
		case <-ticker.C:
		}
	}
	if _, err := service.RetryTurn(ctx, testSession, first.ID); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("retry budget admission=%v", err)
	}
	provider.mu.Lock()
	sent := provider.sendCalls
	provider.mu.Unlock()
	if sent != 1 {
		t.Fatalf("retry bypassed native budget: sendCalls=%d", sent)
	}
	original, err := budget.store.TurnByID(ctx, first.ID)
	if err != nil || original.State != domain.TurnStateFailed || original.ProviderTurnID != first.ProviderTurnID {
		t.Fatalf("original=%+v error=%v", original, err)
	}
}

func reserveBudgetSemanticChat(t *testing.T, budget persistedChatBudget, key string) {
	t.Helper()
	rec, found, err := budget.store.GetSession(context.Background(), testSession)
	if err != nil || !found {
		t.Fatalf("session found=%t error=%v", found, err)
	}
	if _, fresh, err := budget.store.BeginSemanticDelivery(context.Background(), testSession, key, "chat:"+rec.Metadata.ControllerGeneration); err != nil || !fresh {
		t.Fatalf("reservation fresh=%t error=%v", fresh, err)
	}
}

func TestInstructorBudgetSemanticChatRefusalIsRejectedAndProvablyCancellable(t *testing.T) {
	service, provider, budget := budgetChat(t)
	ctx := context.Background()
	spent, err := budget.service.ReserveBudget(ctx, budget.workID, "turn", "already-spent", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.service.SetBudgetState(ctx, spent.ID, "started"); err != nil {
		t.Fatal(err)
	}
	reserveBudgetSemanticChat(t, budget, "quota-refusal")
	for attempt := range 2 {
		_, err := service.RelaySemanticChatTurn(ctx, testSession, "continue", "quota-refusal")
		if !errors.Is(err, ports.ErrSemanticDeliveryRejected) || errors.Is(err, ports.ErrSemanticDeliveryUncertain) {
			t.Fatalf("attempt %d refusal=%v", attempt, err)
		}
	}
	if err := service.ReconcileSemanticChatTurn(ctx, testSession, "quota-refusal", false); err != nil {
		t.Fatalf("known unsubmitted refusal cannot reconcile: %v", err)
	}
	conversation, err := budget.store.ConversationForSession(ctx, testSession)
	if err != nil {
		t.Fatal(err)
	}
	message, found, err := budget.store.ConversationMessageByClientID(ctx, conversation.ID, "quota-refusal")
	if err != nil || !found {
		t.Fatalf("message found=%t error=%v", found, err)
	}
	turn, err := budget.store.TurnByID(ctx, message.TurnID)
	if err != nil || turn.State != domain.TurnStateCancelled || turn.ProviderTurnID != "" {
		t.Fatalf("turn=%+v error=%v", turn, err)
	}
	provider.mu.Lock()
	calls := provider.sendCalls
	provider.mu.Unlock()
	if calls != 0 {
		t.Fatalf("native call after quota refusal: %d", calls)
	}
}

func TestInstructorBudgetSemanticChatNativeFailureRemainsUncertainAndCharged(t *testing.T) {
	service, provider, budget := budgetChat(t)
	ctx := context.Background()
	provider.mu.Lock()
	provider.sendErr = errors.New("native connection lost during submission")
	provider.mu.Unlock()
	reserveBudgetSemanticChat(t, budget, "native-unknown")
	for attempt := range 2 {
		if _, err := service.RelaySemanticChatTurn(ctx, testSession, "continue", "native-unknown"); !errors.Is(err, ports.ErrSemanticDeliveryUncertain) || errors.Is(err, ports.ErrSemanticDeliveryRejected) {
			t.Fatalf("attempt %d native error=%v", attempt, err)
		}
	}
	if err := service.ReconcileSemanticChatTurn(ctx, testSession, "native-unknown", false); !errors.Is(err, ports.ErrSemanticDeliveryUncertain) {
		t.Fatalf("unknown native effect was declared unsubmitted: %v", err)
	}
	usage, err := budget.service.BudgetUsage(ctx, budget.workID)
	if err != nil || usage.Turns != 1 {
		t.Fatalf("unknown native effect refunded budget: %+v error=%v", usage, err)
	}
	provider.mu.Lock()
	calls := provider.sendCalls
	provider.mu.Unlock()
	if calls != 1 {
		t.Fatalf("unknown native effect replayed: calls=%d", calls)
	}
}

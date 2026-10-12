package chat_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func reserveSemanticChat(t *testing.T, h *harness, key string) {
	t.Helper()
	rec, found, err := h.st.GetSession(context.Background(), testSession)
	if err != nil || !found {
		t.Fatalf("session found=%t err=%v", found, err)
	}
	if _, fresh, err := h.st.BeginSemanticDelivery(context.Background(), testSession, key, "chat:"+rec.Metadata.ControllerGeneration); err != nil || !fresh {
		t.Fatalf("reserve fresh=%t err=%v", fresh, err)
	}
}

func TestSemanticChatNativeReceiptDistinguishesAcceptedAndQueued(t *testing.T) {
	h := newHarness(t)
	reserveSemanticChat(t, h, "first")
	receipt, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "first", "first")
	if err != nil || receipt.State != "accepted" || receipt.ClientMessageID != "first" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	reserveSemanticChat(t, h, "queued")
	receipt, err = h.svc.RelaySemanticChatTurn(context.Background(), testSession, "queued", "queued")
	if !errors.Is(err, ports.ErrSemanticDeliveryQueued) || receipt.State != "queued" {
		t.Fatalf("queued receipt=%+v err=%v", receipt, err)
	}
	stored, _, err := h.st.GetSemanticDelivery(context.Background(), testSession, "queued")
	if err != nil || stored.State != "pending" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if err := h.svc.ReconcileSemanticChatTurn(context.Background(), testSession, "queued", false); err != nil {
		t.Fatal(err)
	}
	conversation, err := h.st.ConversationForSession(context.Background(), testSession)
	if err != nil {
		t.Fatal(err)
	}
	message, found, err := h.st.ConversationMessageByClientID(context.Background(), conversation.ID, "queued")
	if err != nil || !found {
		t.Fatalf("message found=%t err=%v", found, err)
	}
	turn, err := h.st.TurnByID(context.Background(), message.TurnID)
	if err != nil || turn.State != domain.TurnStateCancelled {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
}

func TestSemanticChatFailedDuplicateNeverBecomesAcceptance(t *testing.T) {
	h := newHarness(t)
	h.conv.mu.Lock()
	h.conv.sendErr = errors.New("native connection lost")
	h.conv.mu.Unlock()
	reserveSemanticChat(t, h, "failed")
	for range 2 {
		if _, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "failed", "failed"); !errors.Is(err, ports.ErrSemanticDeliveryUncertain) {
			t.Fatalf("failed error=%v", err)
		}
	}
	h.conv.mu.Lock()
	calls := h.conv.sendCalls
	h.conv.mu.Unlock()
	stored, _, err := h.st.GetSemanticDelivery(context.Background(), testSession, "failed")
	if err != nil || calls != 1 || stored.State != "pending" {
		t.Fatalf("calls=%d stored=%+v err=%v", calls, stored, err)
	}
}

type deferredSemanticConversation struct{ *fakeConversation }

func (c *deferredSemanticConversation) StartDeferredTurn(id string) error {
	c.events <- ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: id}
	return nil
}
func (*deferredSemanticConversation) DiscardDeferredTurn(string) {}

func TestSemanticChatSyntheticDeferredStartIsNotNativeAcceptance(t *testing.T) {
	conv := &deferredSemanticConversation{newFakeConversation()}
	h := newHarnessWithConversation(t, conv)
	reserveSemanticChat(t, h, "deferred")
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if _, err := h.svc.RelaySemanticChatTurn(ctx, testSession, "deferred", "deferred"); !errors.Is(err, ports.ErrSemanticDeliveryUncertain) {
		t.Fatalf("synthetic started error=%v", err)
	}
	stored, _, err := h.st.GetSemanticDelivery(context.Background(), testSession, "deferred")
	if err != nil || stored.State != "pending" {
		t.Fatalf("synthetic start stored=%+v err=%v", stored, err)
	}
	conv.events <- ports.ChatEvent{Kind: ports.ChatEventMessageDelta, ProviderTurnID: "provider-turn-1", ProviderItemID: "reply", Delta: "actual native response"}
	if receipt, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "deferred", "deferred"); err != nil || receipt.State != "accepted" {
		t.Fatalf("native response receipt=%+v err=%v", receipt, err)
	}
	conv.mu.Lock()
	calls := conv.sendCalls
	conv.mu.Unlock()
	if calls != 1 {
		t.Fatalf("native replay calls=%d", calls)
	}
}

func TestSemanticChatQueuedDirectiveRevalidatesBeforeNativeDispatch(t *testing.T) {
	h := newHarness(t)
	var stopped atomic.Bool
	h.svc.SetSemanticDispatchValidator(func(_ context.Context, _ domain.SessionID, key string) error {
		if key == "queued-stop" && stopped.Load() {
			return errors.New("work parked")
		}
		return nil
	})
	reserveSemanticChat(t, h, "running")
	if _, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "running", "running"); err != nil {
		t.Fatal(err)
	}
	reserveSemanticChat(t, h, "queued-stop")
	if _, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "queued-stop", "queued-stop"); !errors.Is(err, ports.ErrSemanticDeliveryQueued) {
		t.Fatalf("queue err=%v", err)
	}
	stopped.Store(true)
	h.conv.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: "provider-turn-1", TurnState: domain.TurnStateCompleted}
	deadline := time.Now().Add(time.Second)
	for {
		conversation, err := h.st.ConversationForSession(context.Background(), testSession)
		if err != nil {
			t.Fatal(err)
		}
		message, _, err := h.st.ConversationMessageByClientID(context.Background(), conversation.ID, "queued-stop")
		if err != nil {
			t.Fatal(err)
		}
		turn, err := h.st.TurnByID(context.Background(), message.TurnID)
		if err != nil {
			t.Fatal(err)
		}
		if turn.State == domain.TurnStateCancelled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not cancelled: %+v", turn)
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.conv.mu.Lock()
	calls := h.conv.sendCalls
	h.conv.mu.Unlock()
	if calls != 1 {
		t.Fatalf("parked native directive submitted: calls=%d", calls)
	}
	if _, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "queued-stop", "queued-stop"); !errors.Is(err, ports.ErrSemanticDeliveryRejected) {
		t.Fatalf("cancelled duplicate err=%v", err)
	}
}

func TestSemanticChatCancellationDoesNotWaitForBlockedNativeSubmission(t *testing.T) {
	h := newHarness(t)
	reserveSemanticChat(t, h, "running")
	if _, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "running", "running"); err != nil {
		t.Fatal(err)
	}
	reserveSemanticChat(t, h, "blocked")
	if _, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "blocked", "blocked"); !errors.Is(err, ports.ErrSemanticDeliveryQueued) {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	h.conv.mu.Lock()
	h.conv.onSend = func(id string) {
		if id == "provider-turn-2" {
			close(entered)
			<-release
		}
	}
	h.conv.mu.Unlock()
	defer close(release)
	h.conv.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: "provider-turn-1", TurnState: domain.TurnStateCompleted}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("native queued submission did not begin")
	}
	started := time.Now()
	if err := h.svc.ReconcileSemanticChatTurn(context.Background(), testSession, "blocked", false); !errors.Is(err, ports.ErrSemanticDeliveryUncertain) {
		t.Fatalf("blocked native request was not proved cancelled: %v", err)
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatal("cancellation waited for blocked provider I/O")
	}
}

func TestSemanticChatEvidenceReinspectionDefersExactQueueWithoutCancelling(t *testing.T) {
	h := newHarness(t)
	var pending atomic.Bool
	pending.Store(true)
	h.svc.SetSemanticDispatchValidator(func(_ context.Context, _ domain.SessionID, _ string) error {
		if pending.Load() {
			return ports.ErrSemanticDeliveryDeferred
		}
		return nil
	})
	reserveSemanticChat(t, h, "inspect")
	if _, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "inspect", "inspect"); !errors.Is(err, ports.ErrSemanticDeliveryQueued) {
		t.Fatalf("inspection should preserve its existing queue: %v", err)
	}
	h.conv.mu.Lock()
	calls := h.conv.sendCalls
	h.conv.mu.Unlock()
	if calls != 0 {
		t.Fatalf("native submission before fresh evidence: %d", calls)
	}
	pending.Store(false)
	if receipt, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "inspect", "inspect"); err != nil || receipt.State != "accepted" {
		t.Fatalf("fresh inspection should wake the exact queued directive: %+v, %v", receipt, err)
	}
	h.conv.mu.Lock()
	calls = h.conv.sendCalls
	h.conv.mu.Unlock()
	if calls != 1 {
		t.Fatalf("exact queue submitted %d times", calls)
	}
}

package chat_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Optional idempotency keys must not make the actual native admission check
// optional. The core separately verifies current managed-worker triage grants.
func TestManagedNativeAdmissionRunsWithoutClientMessageIdentity(t *testing.T) {
	for _, route := range []string{"ordinary-chat", "user-authored-relay"} {
		t.Run(route, func(t *testing.T) {
			h := newHarness(t)
			denied := &ports.ChatAdmissionError{Reason: "managed worker awaits the current human fix grant"}
			var validations atomic.Int32
			h.svc.SetSemanticDispatchValidator(func(_ context.Context, id domain.SessionID, key string) error {
				validations.Add(1)
				if id != testSession || key != "" {
					return errors.New("unexpected ordinary message identity")
				}
				return denied
			})
			var err error
			if route == "ordinary-chat" {
				_, err = h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{Text: "Start fixing now", Origin: domain.MessageOriginHuman})
			} else {
				_, err = h.svc.RelayUserAuthoredChatTurn(context.Background(), testSession, "Start fixing now")
			}
			h.conv.mu.Lock()
			nativeSends := h.conv.sendCalls
			h.conv.mu.Unlock()
			if !errors.Is(err, ports.ErrSemanticDeliveryRejected) || !strings.Contains(err.Error(), denied.Reason) || validations.Load() != 1 || nativeSends != 0 {
				t.Fatalf("empty message key bypassed installed policy: validations=%d nativeSends=%d err=%v", validations.Load(), nativeSends, err)
			}
		})
	}
}

func TestManagedQueuedNativeAdmissionRunsWithoutClientMessageIdentity(t *testing.T) {
	h := newHarness(t)
	var denied atomic.Bool
	var rejected atomic.Int32
	h.svc.SetSemanticDispatchValidator(func(_ context.Context, _ domain.SessionID, key string) error {
		if key == "" && denied.Load() {
			rejected.Add(1)
			return errors.New("managed worker awaits the current human fix grant")
		}
		return nil
	})
	if _, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{Text: "Existing admitted turn", ClientMessageID: "already-admitted"}); err != nil {
		t.Fatal(err)
	}
	queued, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{Text: "Fix after this", Origin: domain.MessageOriginHuman})
	if err != nil || queued.State != domain.TurnStateQueued {
		t.Fatalf("ordinary empty-key turn not queued: %+v err=%v", queued, err)
	}
	denied.Store(true)
	h.conv.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: "provider-turn-1", TurnState: domain.TurnStateCompleted}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for {
		turn, err := h.st.TurnByID(context.Background(), queued.ID)
		if err != nil {
			t.Fatal(err)
		}
		if turn.State == domain.TurnStateCancelled {
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("queued empty-key turn was not rejected before native dispatch: %+v", turn)
		case <-poll.C:
		}
	}
	h.conv.mu.Lock()
	defer h.conv.mu.Unlock()
	if rejected.Load() != 1 || h.conv.sendCalls != 1 {
		t.Fatalf("empty queued key bypassed changed authority: rejections=%d nativeSends=%d", rejected.Load(), h.conv.sendCalls)
	}
}

func TestUnmanagedNativeMessageWithoutClientIdentityKeepsExistingBehavior(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{Text: "Ordinary legacy message", Origin: domain.MessageOriginHuman}); err != nil {
		t.Fatalf("unmanaged legacy turn rejected: %v", err)
	}
	h.conv.mu.Lock()
	defer h.conv.mu.Unlock()
	if h.conv.sendCalls != 1 {
		t.Fatalf("legacy native submission count=%d", h.conv.sendCalls)
	}
}

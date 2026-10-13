package chat_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestGovernedAdmissionRejectionRetainsCauseAcrossDurableRetry(t *testing.T) {
	for _, queued := range []bool{false, true} {
		name := "direct"
		if queued {
			name = "queued"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			var reject atomic.Bool
			cause := "role requires harness opencode, got codex"
			h.svc.SetSemanticDispatchValidator(func(_ context.Context, _ domain.SessionID, key string) error {
				if key == "assignment" && reject.Load() {
					return &ports.ChatAdmissionError{Reason: cause}
				}
				return nil
			})
			if queued {
				reserveSemanticChat(t, h, "running")
				if _, err := h.svc.RelaySemanticChatTurn(ctx, testSession, "already admitted", "running"); err != nil {
					t.Fatal(err)
				}
			}
			reserveSemanticChat(t, h, "assignment")
			reject.Store(!queued)
			_, err := h.svc.RelaySemanticChatTurn(ctx, testSession, "private task body", "assignment")
			if queued {
				if !errors.Is(err, ports.ErrSemanticDeliveryQueued) {
					t.Fatalf("expected queued assignment: %v", err)
				}
				reject.Store(true)
				h.conv.emit(ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: "provider-turn-1", TurnState: domain.TurnStateCompleted})
			} else if !errors.Is(err, ports.ErrSemanticDeliveryRejected) || !strings.Contains(err.Error(), cause) {
				t.Fatalf("direct rejection lost cause: %v", err)
			}
			rows := h.awaitSnapshot(t, func(rows store.ConversationSnapshot) bool {
				for _, message := range rows.Messages {
					if message.ClientMessageID != "assignment" {
						continue
					}
					for _, turn := range rows.Turns {
						if turn.ID == message.TurnID {
							return turn.State == domain.TurnStateCancelled && turn.ProviderTurnID == "" && strings.Contains(turn.ErrorMessage, cause)
						}
					}
				}
				return false
			})
			if len(rows.Turns) == 0 {
				t.Fatal("missing durable admission result")
			}
			_, err = h.svc.RelaySemanticChatTurn(ctx, testSession, "private task body", "assignment")
			if !errors.Is(err, ports.ErrSemanticDeliveryRejected) || !strings.Contains(err.Error(), cause) || strings.Contains(err.Error(), "private task body") {
				t.Fatalf("durable rejection lost admission cause or leaked prompt: %v", err)
			}
			h.conv.mu.Lock()
			calls := h.conv.sendCalls
			h.conv.mu.Unlock()
			want := 0
			if queued {
				want = 1
			}
			if calls != want {
				t.Fatalf("rejected assignment reached native provider: calls=%d want=%d", calls, want)
			}
		})
	}
}

func TestGovernedAdmissionStoredReasonIsBounded(t *testing.T) {
	h := newHarness(t)
	h.svc.SetSemanticDispatchValidator(func(context.Context, domain.SessionID, string) error {
		return &ports.ChatAdmissionError{Reason: strings.Repeat("ü", 4096)}
	})
	if _, err := h.ctrl.Send(context.Background(), ports.ChatUserMessage{Text: "task", ClientMessageID: "bounded-rejection"}); !errors.Is(err, ports.ErrSemanticDeliveryRejected) {
		t.Fatal(err)
	}
	rows, err := h.st.LoadConversationSnapshot(context.Background(), h.ctrl.ConversationID())
	if err != nil || len(rows.Turns) != 1 {
		t.Fatalf("rows=%+v error=%v", rows, err)
	}
	const prefix = "governed directive no longer admitted: "
	if got := rows.Turns[0].ErrorMessage; !strings.HasPrefix(got, prefix) || utf8.RuneCountInString(got) != len(prefix)+2048 {
		t.Fatalf("unexpected stored reason length: %d", utf8.RuneCountInString(got))
	}
}

func TestGovernedAdmissionUnexpectedValidatorFailureIsPrivate(t *testing.T) {
	const secret = "private-provider-token-and-task-body"
	for name, failure := range map[string]error{
		"store":   errors.New("lookup storage: " + secret),
		"catalog": fmt.Errorf("%w: %s", ports.ErrModelCapabilitiesUnavailable, secret),
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			h.svc.SetSemanticDispatchValidator(func(context.Context, domain.SessionID, string) error {
				return failure
			})
			reserveSemanticChat(t, h, "private-admission")
			for attempt := range 2 {
				_, err := h.svc.RelaySemanticChatTurn(ctx, testSession, secret, "private-admission")
				if !errors.Is(err, ports.ErrSemanticDeliveryRejected) || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "check controller readiness") {
					t.Fatalf("attempt %d exposed an unclassified validator error or lost recovery guidance: %v", attempt, err)
				}
			}
			rows, err := h.st.LoadConversationSnapshot(ctx, h.ctrl.ConversationID())
			if err != nil || len(rows.Turns) != 1 {
				t.Fatalf("rows=%+v error=%v", rows, err)
			}
			turn := rows.Turns[0]
			if turn.State != domain.TurnStateCancelled || turn.ProviderTurnID != "" || strings.Contains(turn.ErrorMessage, secret) || !strings.Contains(turn.ErrorMessage, "check controller readiness") {
				t.Fatalf("durable rejection exposed an unclassified validator error: %+v", turn)
			}
			h.conv.mu.Lock()
			calls := h.conv.sendCalls
			h.conv.mu.Unlock()
			if calls != 0 {
				t.Fatalf("unverified assignment reached native provider: calls=%d", calls)
			}
		})
	}
}

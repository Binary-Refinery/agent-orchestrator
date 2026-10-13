package chat_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type receiptReadInterleavingStore struct {
	*sqlite.Store
	reads        atomic.Int32
	afterPending func(context.Context)
	readError    error
	hideReceipt  bool
}

func (s *receiptReadInterleavingStore) GetSemanticDelivery(ctx context.Context, id domain.SessionID, key string) (domain.SemanticDelivery, bool, error) {
	receipt, found, err := s.Store.GetSemanticDelivery(ctx, id, key)
	// First read admits the reserved operation; second read begins the receipt
	// wait. Complete the real native projection after its pending snapshot was
	// read, but before Relay reads the associated turn's terminal state.
	read := s.reads.Add(1)
	if read == 2 && found && err == nil && receipt.State == "pending" {
		s.afterPending(ctx)
	}
	if read >= 3 {
		if s.readError != nil {
			return domain.SemanticDelivery{}, false, s.readError
		}
		if s.hideReceipt {
			return domain.SemanticDelivery{}, false, nil
		}
	}
	return receipt, found, err
}

func TestSemanticChatReceiptCommittedBetweenReceiptAndTerminalReads(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		outcome       domain.TurnState
		cancelReceipt bool
		hideReceipt   bool
		readError     error
	}{
		{name: "accepted completion", outcome: domain.TurnStateCompleted},
		{name: "failed native turn remains pending", outcome: domain.TurnStateFailed},
		{name: "cancelled native turn remains pending", outcome: domain.TurnStateCancelled},
		{name: "cancelled receipt cannot prove acceptance", outcome: domain.TurnStateFailed, cancelReceipt: true},
		{name: "missing receipt cannot prove acceptance", outcome: domain.TurnStateCompleted, hideReceipt: true},
		{name: "unreadable receipt cannot prove acceptance", outcome: domain.TurnStateCompleted, readError: errors.New("receipt read unavailable")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			outcome := scenario.outcome
			provider := &deferredConversation{fakeConversation: newFakeConversation(), start: func(string) error { return nil }}
			var interleaved *receiptReadInterleavingStore
			h := newHarnessWithConversationAndStore(t, provider, func(st *sqlite.Store) chatsvc.Store {
				interleaved = &receiptReadInterleavingStore{Store: st, readError: scenario.readError, hideReceipt: scenario.hideReceipt}
				return interleaved
			})
			reserveSemanticChat(t, h, "fast-native-result")
			var projected bool
			interleaved.afterPending = func(ctx context.Context) {
				message, found, err := h.st.ConversationMessageByClientID(ctx, h.ctrl.ConversationID(), "fast-native-result")
				if err != nil || !found {
					t.Fatalf("durable exact-key message: found=%t error=%v", found, err)
				}
				turn, err := h.st.TurnByID(ctx, message.TurnID)
				if err != nil || turn.ProviderTurnID == "" {
					t.Fatalf("durable native binding: %+v error=%v", turn, err)
				}
				provider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: turn.ProviderTurnID, TurnState: outcome})
				deadline := time.Now().Add(2 * time.Second)
				for {
					current, err := h.st.TurnByID(ctx, turn.ID)
					if err != nil {
						t.Fatal(err)
					}
					if current.State == outcome {
						projected = true
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("native completion not projected: %+v", current)
					}
					time.Sleep(time.Millisecond)
				}
				stored, found, err := h.st.GetSemanticDelivery(ctx, testSession, "fast-native-result")
				if err != nil || !found || (stored.State == "accepted") != (outcome == domain.TurnStateCompleted) {
					t.Fatalf("native evidence before stale read returned: %+v error=%v", stored, err)
				}
				if scenario.cancelReceipt {
					if err := h.st.ResolveSemanticDelivery(ctx, testSession, "fast-native-result", false); err != nil {
						t.Fatal(err)
					}
				}
			}
			receipt, err := h.svc.RelaySemanticChatTurn(context.Background(), testSession, "exact native task", "fast-native-result")
			if !projected {
				t.Fatal("test did not cross the receipt/terminal observation boundary")
			}
			if outcome == domain.TurnStateCompleted && !scenario.hideReceipt && scenario.readError == nil {
				if err != nil || receipt.State != "accepted" {
					t.Fatalf("durable native acceptance was misclassified by stale receipt: %+v error=%v", receipt, err)
				}
			} else if !errors.Is(err, ports.ErrSemanticDeliveryUncertain) {
				t.Fatalf("terminal state without native acceptance must remain uncertain: %+v error=%v", receipt, err)
			}
			if scenario.readError != nil && !errors.Is(err, scenario.readError) {
				t.Fatalf("receipt read failure was hidden: %v", err)
			}
			if provider.sendCallCount() != 1 {
				t.Fatal("receipt observation resubmitted native work")
			}
		})
	}
}

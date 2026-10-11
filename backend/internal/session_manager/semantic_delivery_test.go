package sessionmanager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type memorySemanticLedger struct {
	mu      sync.Mutex
	entries map[string]domain.SemanticDelivery
}

func (l *memorySemanticLedger) GetSemanticDelivery(_ context.Context, id domain.SessionID, key string) (domain.SemanticDelivery, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, found := l.entries[string(id)+"\x00"+key]
	return rec, found, nil
}

func (l *memorySemanticLedger) BeginSemanticDelivery(_ context.Context, id domain.SessionID, key, generation string) (domain.SemanticDelivery, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]domain.SemanticDelivery{}
	}
	k := string(id) + "\x00" + key
	if rec, found := l.entries[k]; found {
		return rec, false, nil
	}
	rec := domain.SemanticDelivery{SessionID: id, ClientMessageID: key, RuntimeLaunchID: generation, State: "pending"}
	l.entries[k] = rec
	return rec, true, nil
}
func (l *memorySemanticLedger) AcceptSemanticDelivery(_ context.Context, id domain.SessionID, key, generation string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := string(id) + "\x00" + key
	rec, found := l.entries[k]
	if !found || rec.RuntimeLaunchID != generation || rec.State == "cancelled" {
		return errors.New("unbound acceptance")
	}
	rec.State = "accepted"
	l.entries[k] = rec
	return nil
}
func (l *memorySemanticLedger) ResolveSemanticDelivery(_ context.Context, id domain.SessionID, key string, accepted bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := string(id) + "\x00" + key
	rec, found := l.entries[k]
	if !found {
		return nil
	}
	if accepted {
		rec.State = "accepted"
	} else {
		rec.State = "cancelled"
	}
	l.entries[k] = rec
	return nil
}

func TestSemanticDeliveryAcceptedSurvivesCheckpointAndRuntimeReplacement(t *testing.T) {
	store := newFakeStore()
	store.sessions["s"] = pastStartupGate(domain.SessionRecord{ID: "s", Harness: domain.HarnessClaudeCode, Mode: domain.SessionModeTUI, Metadata: domain.SessionMetadata{RuntimeLaunchID: "launch-1"}})
	ledger := &memorySemanticLedger{}
	if _, _, err := ledger.BeginSemanticDelivery(context.Background(), "s", "key", "launch-1"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.AcceptSemanticDelivery(context.Background(), "s", "key", "launch-1"); err != nil {
		t.Fatal(err)
	}
	rec := store.sessions["s"]
	rec.Metadata.RuntimeLaunchID = "launch-2"
	rec.Metadata.ConversationCheckpointTurnID = "later-message"
	store.sessions["s"] = rec
	messenger := &fakeMessenger{}
	manager := newSendTestManager(t, semanticSignalingAgent{}, messenger, store)
	manager.SetSemanticDeliveryLedger(ledger)
	if err := manager.SendGovernedSemantic(context.Background(), "s", "do not duplicate", "key"); err != nil {
		t.Fatal(err)
	}
	if len(messenger.msgs) != 0 {
		t.Fatalf("accepted retry wrote %d native messages", len(messenger.msgs))
	}
}

func TestSemanticDeliveryPendingNeverResendsAfterRestart(t *testing.T) {
	store := newFakeStore()
	store.sessions["s"] = pastStartupGate(domain.SessionRecord{ID: "s", Harness: domain.HarnessClaudeCode, Mode: domain.SessionModeTUI, Activity: domain.Activity{State: domain.ActivityActive}, Metadata: domain.SessionMetadata{RuntimeLaunchID: "launch-1"}})
	ledger := &memorySemanticLedger{}
	firstMessenger := &fakeMessenger{}
	first := newSendTestManager(t, semanticSignalingAgent{}, firstMessenger, store)
	first.SetSemanticDeliveryLedger(ledger)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := first.SendGovernedSemantic(ctx, "s", "one directive", "key"); !errors.Is(err, ports.ErrSemanticDeliveryUncertain) {
		t.Fatalf("first error=%v", err)
	}
	if len(firstMessenger.msgs) != 1 {
		t.Fatalf("first writes=%d", len(firstMessenger.msgs))
	}
	rec := store.sessions["s"]
	rec.Metadata.RuntimeLaunchID = "launch-2"
	store.sessions["s"] = rec
	secondMessenger := &fakeMessenger{}
	second := newSendTestManager(t, semanticSignalingAgent{}, secondMessenger, store)
	second.SetSemanticDeliveryLedger(ledger)
	if err := second.SendGovernedSemantic(context.Background(), "s", "retry", "key"); !errors.Is(err, ports.ErrSemanticDeliveryUncertain) {
		t.Fatalf("retry error=%v", err)
	}
	if len(secondMessenger.msgs) != 0 {
		t.Fatalf("uncertain retry wrote %d native messages", len(secondMessenger.msgs))
	}
}

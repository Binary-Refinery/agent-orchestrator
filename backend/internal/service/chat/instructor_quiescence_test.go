package chat_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestInstructorQuiescenceRequiresNativeReadyAndSettledQueue(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	quiet, err := h.svc.InstructorQuiescent(ctx, testSession)
	if err != nil || !quiet {
		t.Fatalf("fresh native ready controller: %v %v", quiet, err)
	}
	if _, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "work", ClientMessageID: "cancel-proof"}); err != nil {
		t.Fatal(err)
	}
	h.conv.emit(ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: "provider-turn-1"})
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return len(s.Turns) == 1 && s.Turns[0].State == domain.TurnStateRunning
	})
	if quiet, err = h.svc.InstructorQuiescent(ctx, testSession); err != nil || quiet {
		t.Fatalf("native running turn was quiescent: %v %v", quiet, err)
	}
	if err := h.svc.Interrupt(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	if quiet, err = h.svc.InstructorQuiescent(ctx, testSession); err != nil || quiet {
		t.Fatalf("interrupt admission falsely proved provider completed: %v %v", quiet, err)
	}
	h.conv.emit(ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: "provider-turn-1", TurnState: domain.TurnStateInterrupted})
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return len(s.Turns) == 1 && s.Turns[0].State == domain.TurnStateInterrupted
	})
	// Persistence precedes the controller's ready transition and queue drain.
	// Poll the actual native quiet seam rather than racing that final callback.
	deadline := time.Now().Add(5 * time.Second)
	for {
		quiet, err = h.svc.InstructorQuiescent(ctx, testSession)
		if err != nil {
			t.Fatal(err)
		}
		if quiet {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native terminal receipt never became quiescent")
		}
		time.Sleep(time.Millisecond)
	}
	if err := h.svc.Stop(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	if quiet, err = h.svc.InstructorQuiescent(ctx, testSession); quiet || err == nil {
		t.Fatalf("missing controller falsely proved stopped: %v %v", quiet, err)
	}
}

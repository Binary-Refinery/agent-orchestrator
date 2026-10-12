package sessionmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type controlIdentityStore struct {
	*fakeStore
	instance string
}

func (s controlIdentityStore) InstructorSessionForMembership(ctx context.Context, id domain.SessionID) (domain.SessionRecord, string, bool, error) {
	rec, found, err := s.GetSession(ctx, id)
	return rec, s.instance, found, err
}

func TestInstructorNativeStopUsesControlAndRejectsReusedIdentity(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	st.sessions["worker"] = domain.SessionRecord{ID: "worker", Kind: domain.KindWorker, Mode: domain.SessionModeTUI, Activity: domain.Activity{State: domain.ActivityActive}, Metadata: domain.SessionMetadata{RuntimeHandleID: "owned-runtime"}}
	runtime := &fakeRuntime{}
	messenger := &fakeMessenger{}
	identity := controlIdentityStore{fakeStore: st, instance: "inserted-row-1"}
	m := New(Deps{Store: identity, Runtime: runtime, Messenger: messenger})
	if err := m.InterruptInstructorProducer(ctx, "worker", "inserted-row-1"); err != nil {
		t.Fatal(err)
	}
	if len(runtime.interrupts) != 1 || runtime.interrupts[0] != "owned-runtime" || len(messenger.msgs) != 0 {
		t.Fatalf("stop became a model prompt: runtime=%+v messages=%+v", runtime.interrupts, messenger.msgs)
	}
	if err := m.InterruptInstructorProducer(ctx, "worker", "historical-row"); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("reused identity didn't fail: %v", err)
	}
	if len(runtime.interrupts) != 1 {
		t.Fatal("historical identity interrupted replacement")
	}
	if err := m.beginAgentOperation(ctx, "worker", agentOperationSwitch); err != nil {
		t.Fatal(err)
	}
	if err := m.InterruptInstructorProducer(ctx, "worker", "inserted-row-1"); !errors.Is(err, errAgentOperationInProgress) {
		t.Fatalf("stop wrote during exclusive replacement: %v", err)
	}
	m.endAgentOperation("worker", agentOperationSwitch)
}

func TestInstructorCancellationQuiescenceDoesNotInferTUIDeathOrReplacement(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	st.sessions["worker"] = domain.SessionRecord{ID: "worker", Kind: domain.KindWorker, Mode: domain.SessionModeTUI, Activity: domain.Activity{State: domain.ActivityIdle}}
	m := New(Deps{Store: controlIdentityStore{fakeStore: st, instance: "original"}})
	if quiet, err := m.InstructorProducerQuiescent(ctx, "worker", "original"); err != nil || quiet {
		t.Fatalf("idle TUI was native termination: %v %v", quiet, err)
	}
	if quiet, err := m.InstructorProducerQuiescent(ctx, "worker", "replaced"); quiet || !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("replacement inherited native proof: %v %v", quiet, err)
	}
	rec := st.sessions["worker"]
	rec.Activity.State = domain.ActivityExited
	st.sessions["worker"] = rec
	if quiet, err := m.InstructorProducerQuiescent(ctx, "worker", "original"); err != nil || !quiet {
		t.Fatalf("positive native exit missing: %v %v", quiet, err)
	}
}

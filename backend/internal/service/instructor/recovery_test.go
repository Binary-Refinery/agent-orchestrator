package instructor_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type recoveryDelivery struct {
	*deliveryRecorder
	ready      bool
	recoveries int
	created    time.Time
}

func (d *recoveryDelivery) RecoveryStatus(_ context.Context, w domain.InstructorWork) (domain.InstructorRecoveryStatus, error) {
	action := "resume"
	if d.ready {
		action = "none"
	}
	return domain.InstructorRecoveryStatus{WorkID: w.ID, WorkRevision: w.Revision, PolicyVersion: w.PolicyVersion, SessionID: "orchestrator", SessionCreatedAt: d.created, ProviderConversationID: "same-native-thread", ControllerGeneration: "old-generation", Ready: d.ready, Action: action, ReasonCode: "CONTROLLER_UNAVAILABLE", Reason: "existing controller needs recovery"}, nil
}

func (d *recoveryDelivery) RecoverController(context.Context, domain.InstructorWork, domain.InstructorRecoveryInput) error {
	d.recoveries++
	d.ready = true
	return nil
}

func fixtureWithRecovery(t *testing.T) (*instructorFixture, *recoveryDelivery) {
	f := newInstructorFixture(t)
	d := &recoveryDelivery{deliveryRecorder: f.delivery, created: f.now}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: d, Clock: func() time.Time { return f.now }})
	return f, d
}

func capturedRecovery(s domain.InstructorRecoveryStatus) domain.InstructorRecoveryInput {
	return domain.InstructorRecoveryInput{WorkID: s.WorkID, WorkRevision: s.WorkRevision, PolicyVersion: s.PolicyVersion, SessionID: s.SessionID, SessionCreatedAt: s.SessionCreatedAt, ProviderConversationID: s.ProviderConversationID, ControllerGeneration: s.ControllerGeneration}
}

func TestInstructorRecoveryWithholdsApprovalWithoutChangingHumanGate(t *testing.T) {
	f, delivery := fixtureWithRecovery(t)
	w := f.register(t, "recovery-gate", instructorsvc.DefaultPolicy())
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State = "waiting_human"
		w.Gate = &domain.InstructorGate{ID: "business-gate", Kind: "manual", Title: "Confirm scope", ExactApproval: "SCOPE-GO", OpenedAt: f.now}
	})
	snapshot := f.snapshot(t)
	if snapshot.Recoveries[w.ID].Ready || slices.Contains(snapshot.Works[0].Actions, "go") {
		t.Fatalf("missing controller admitted: %+v", snapshot)
	}
	_, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: snapshot.ID, WorkIDs: []string{w.ID}, Approval: "SCOPE-GO", IdempotencyKey: "missing-controller"})
	if !errors.Is(err, instructorsvc.ErrRecoveryRequired) {
		t.Fatalf("got %v", err)
	}
	before, _, _ := f.store.GetInstructorWork(context.Background(), w.ID)
	if before.Revision != w.Revision || before.Gate == nil || before.Gate.ID != "business-gate" {
		t.Fatalf("readiness consumed approval: %+v", before)
	}
	input := capturedRecovery(snapshot.Recoveries[w.ID])
	for i := 0; i < 2; i++ {
		status, err := f.service.Recover(context.Background(), input)
		if err != nil || !status.Ready {
			t.Fatalf("recover %d: %+v %v", i, status, err)
		}
	}
	if delivery.recoveries != 1 || len(delivery.calls) != 0 {
		t.Fatalf("recovery reexecuted work: %+v", delivery)
	}
	after, _, _ := f.store.GetInstructorWork(context.Background(), w.ID)
	if after.Revision != before.Revision || after.Gate.ID != before.Gate.ID || after.State != "waiting_human" {
		t.Fatalf("recovery changed authority: %+v", after)
	}
}

func TestInstructorRecoveryRejectsChangedCapture(t *testing.T) {
	for _, field := range []string{"revision", "policy", "instance", "provider", "generation"} {
		t.Run(field, func(t *testing.T) {
			f, d := fixtureWithRecovery(t)
			w := f.register(t, "stale-recovery", instructorsvc.DefaultPolicy())
			status, err := f.service.Recovery(context.Background(), w.ID)
			if err != nil {
				t.Fatal(err)
			}
			in := capturedRecovery(status)
			switch field {
			case "revision":
				in.WorkRevision++
			case "policy":
				in.PolicyVersion = "changed"
			case "instance":
				in.SessionCreatedAt = in.SessionCreatedAt.Add(time.Second)
			case "provider":
				in.ProviderConversationID = "replacement"
			case "generation":
				in.ControllerGeneration = "replacement"
			}
			if _, err = f.service.Recover(context.Background(), in); !errors.Is(err, instructorsvc.ErrConflict) || d.recoveries != 0 {
				t.Fatalf("stale capture admitted: %v calls=%d", err, d.recoveries)
			}
		})
	}
}

func TestInstructorControllerLossAfterApprovalDoesNotStopCohort(t *testing.T) {
	f, d := fixtureWithRecovery(t)
	d.ready = true
	w := f.register(t, "controller-lost-after-approval", instructorsvc.DefaultPolicy())
	batch, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Weiter", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{w.ID}, IdempotencyKey: "same-directive"})
	if err != nil {
		t.Fatal(err)
	}
	d.ready = false
	d.err = instructorsvc.ErrRecoveryRequired
	for i := 0; i < 5; i++ {
		f.now = f.now.Add(time.Minute)
		if err := f.service.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := f.store.ListInstructorDispatches(context.Background(), batch.BatchID, w.ID)
	if err != nil || len(rows) != 1 || rows[0].State != "pending" {
		t.Fatalf("recovery stopped batch: %+v %v", rows, err)
	}
	events, _ := f.store.ListInstructorEvents(context.Background(), w.ID)
	for _, event := range events {
		if event.Kind == "delivery_failed" || event.Kind == "approval_cohort_stopped" {
			t.Fatalf("repeated approval loop: %+v", events)
		}
	}
}

func TestInstructorExactPolicyReboundApprovalCannotBeBypassed(t *testing.T) {
	f := newInstructorFixture(t)
	w := f.register(t, "rebound-approval", instructorsvc.DefaultPolicy())
	f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State = "waiting_human"
		w.Gate = &domain.InstructorGate{ID: "rebound", Kind: "policy_rebound", ExactApproval: "RESUME-GO exact-policy", OpenedAt: f.now}
	})
	_, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{w.ID}, IdempotencyKey: "unsigned-rebind"})
	if !errors.Is(err, instructorsvc.ErrValidation) {
		t.Fatalf("unsigned policy-rebound gate accepted: %v", err)
	}
}

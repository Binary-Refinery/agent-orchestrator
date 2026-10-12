package instructor_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorPendingApprovalSuffixCannotStartPhaseAction(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, sha := committedWorkspace(t)
	first := f.register(t, "prefix-gate", instructorsvc.DefaultPolicy())
	last := f.register(t, "suffix-review-gate", instructorsvc.DefaultPolicy())
	f.attachWorker(t, last.ID)
	for _, w := range []domain.InstructorWork{first, last} {
		f.trustedFacts(t, w.ID, func(item *domain.InstructorWork) {
			item.State = "waiting_human"
			item.Gate = &domain.InstructorGate{ID: "gate-" + item.ID, Kind: "phase_action_failed", Title: "Retry review"}
			if item.ID == last.ID {
				item.Phase = "review"
				item.PhaseSequence = 2
				item.SHA = sha
			}
		})
	}
	f.delivery.err = instructorsvc.ErrDeliveryQueued
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reviews := mechanicalReview{started: make(chan domain.InstructorWork, 1), release: make(chan struct{})}
	f.service.SetPhaseActions(reviews)
	defer func() { close(reviews.release); _ = f.service.Shutdown(ctx) }()
	snap := f.snapshot(t)
	result, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, IdempotencyKey: "pending-suffix-phase", WorkIDs: []string{first.ID, last.ID}, Approvals: []domain.InstructorWorkApproval{{WorkID: first.ID}, {WorkID: last.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	w, _, err := f.store.GetInstructorWork(ctx, last.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.PhaseActions) != 0 {
		t.Fatalf("pending approval suffix started phase action and staled native directive: actions=%d workRevision=%d dispatchRevision=%d", len(w.PhaseActions), w.Revision, result.Dispatches[1].WorkRevision)
	}
}

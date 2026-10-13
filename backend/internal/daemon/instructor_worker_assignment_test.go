package daemon

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestOrdinaryManagedWorkerAssignmentDoesNotRequireDispatchRecord(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	const assignmentID = "ordinary-worker-task-without-dispatch"
	if err := f.service.ValidateDispatchAdmission(ctx, f.worker.ID, assignmentID); err != nil {
		t.Fatalf("ordinary admitted worker assignment requires a control dispatch: %v", err)
	}
	if _, err := f.guard.ReserveManagedTurn(ctx, domain.SessionConversationOwner(f.worker.ID), "ordinary-worker-turn", assignmentID); err != nil {
		t.Fatalf("ordinary worker assignment failed native budget admission: %v", err)
	}
	dispatches, err := f.store.ListInstructorDispatches(ctx, "", f.work.ID)
	if err != nil || len(dispatches) != 0 {
		t.Fatalf("ordinary worker admission manufactured a dispatch: %+v %v", dispatches, err)
	}
}

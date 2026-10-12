package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorBudgetDispatchChargesItsWorkInsteadOfCoordinatorBinding(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	orch, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: f.worker.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Metadata: domain.SessionMetadata{Model: "provider/coordinator", Effort: "high"}, Activity: domain.Activity{State: domain.ActivityIdle}, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.AttachSession(ctx, f.work.ID, orch.ID); err != nil {
		t.Fatal(err)
	}
	bound, found, err := f.store.GetInstructorWork(ctx, f.work.ID)
	if err != nil || !found {
		t.Fatal(err)
	}
	expected := bound.Revision
	bound.Revision++
	bound.State = "parked"
	if saved, err := f.store.SaveInstructorWork(ctx, bound, expected, domain.InstructorEvent{ID: "other-work-parked", WorkID: bound.ID}); err != nil || !saved {
		t.Fatalf("save=%v error=%v", saved, err)
	}
	policy := bound.Policy
	policy.Budgets = &domain.InstructorBudgets{MaxSessions: 2, MaxTurns: 1}
	requesting, err := f.service.Register(ctx, instructorsvc.RegisterInput{ProjectID: f.worker.ProjectID, Name: "Requesting work", Goal: "Charge this active work", Source: "test", SourceRef: "requesting-work", Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := domain.InstructorDispatch{ID: "exact-dispatch", BatchID: "budget-dispatch", WorkID: requesting.ID, ProjectID: requesting.ProjectID, WorkRevision: requesting.Revision, State: "pending"}
	if err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{dispatch}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.BindDispatchTarget(ctx, dispatch.ID, orch.ID); err != nil {
		t.Fatal(err)
	}
	first, err := f.guard.ReserveManagedTurn(ctx, domain.SessionConversationOwner(orch.ID), "actual-first-turn", dispatch.ID)
	if err != nil {
		t.Fatalf("another work's park blocked this work: %v", err)
	}
	if err := f.guard.SetManagedBudgetState(ctx, first, "uncertain"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.guard.ReserveManagedTurn(ctx, domain.SessionConversationOwner(orch.ID), "actual-first-turn", dispatch.ID); !errors.Is(err, instructorsvc.ErrDeliveryUncertain) {
		t.Fatalf("replay admission=%v", err)
	}
	if _, err := f.guard.ReserveManagedTurn(ctx, domain.SessionConversationOwner(orch.ID), "actual-second-turn", dispatch.ID); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("dispatch turn quota=%v", err)
	}
	actual, err := f.service.BudgetUsage(ctx, requesting.ID)
	if err != nil || actual.Turns != 1 || actual.Sessions != 1 {
		t.Fatalf("requesting usage=%+v error=%v", actual, err)
	}
	other, err := f.service.BudgetUsage(ctx, bound.ID)
	if err != nil || other.Turns != 0 {
		t.Fatalf("wrong work charged: usage=%+v error=%v", other, err)
	}
	if _, err := f.guard.ReserveManagedTurn(ctx, domain.SessionConversationOwner(f.worker.ID), "wrong-target", dispatch.ID); err == nil {
		t.Fatal("dispatch budget admitted a different target")
	}
}

func TestInstructorBudgetRejectsReusedDispatchTargetIdentity(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	seed, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: f.worker.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	d := domain.InstructorDispatch{ID: "old-target-budget", BatchID: "old-target-batch", WorkID: f.work.ID, ProjectID: f.work.ProjectID, WorkRevision: f.work.Revision, State: "pending"}
	if err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{d}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.BindDispatchTarget(ctx, d.ID, seed.ID); err != nil {
		t.Fatal(err)
	}
	// The reference cannot move to a new inserted row with the same display ID.
	if removed, err := f.store.DeleteSession(ctx, seed.ID); err != nil || !removed {
		t.Fatalf("delete seed=%v error=%v", removed, err)
	}
	replacement, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: f.worker.ProjectID, Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID != seed.ID {
		t.Fatalf("fixture did not reuse display ID: old=%s replacement=%s", seed.ID, replacement.ID)
	}
	if _, err := f.guard.ReserveManagedTurn(ctx, domain.SessionConversationOwner(replacement.ID), "wrong-incarnation", d.ID); err == nil {
		t.Fatal("reused target consumed a budget reservation")
	}
	usage, err := f.service.BudgetUsage(ctx, f.work.ID)
	if err != nil || usage.Turns != 0 {
		t.Fatalf("usage=%+v error=%v", usage, err)
	}
}

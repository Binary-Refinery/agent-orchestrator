package instructor_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

func TestInstructorCancelledWorkerResultDoesNotBlockResume(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, _ := committedWorkspace(t)
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "HEAD"}, TimeoutSeconds: 10}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "diff", "--exit-code", "HEAD"}, TimeoutSeconds: 10}
	w := f.register(t, "cancelled-result", policy)
	worker := f.attachWorker(t, w.ID)
	rec, _, _ := f.store.GetSession(ctx, worker)
	rec.Metadata.RuntimeLaunchID = "worker-runtime"
	if err := f.store.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) { w.Phase = "implement"; w.PhaseSequence = 1; w.State = "running" })
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reports := reportsvc.New(reportsvc.Deps{Store: f.store, ManagedDone: f.service, Now: func() time.Time { return f.now }})
	if _, err := reports.Create(ctx, reportsvc.CreateInput{SessionID: worker, State: domain.ReportDone, Note: "done"}); err != nil {
		t.Fatal(err)
	}
	stop := f.snapshot(t)
	if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Stopp", SnapshotID: stop.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "stop-result"}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	resume := f.snapshot(t)
	if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Weiter", SnapshotID: resume.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "resume-result"}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// The same durable cancellation remains inert after rebuilding the service.
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	if _, err := f.service.RunChecks(ctx, w.ID, worker, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := f.service.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	ds, _ := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if current.Phase != "review" {
		t.Fatalf("resumed phase stuck=%s state=%s evidence=%d dispatches=%+v", current.Phase, current.State, len(current.Evidence), ds)
	}
}

func TestInstructorCancelledStartedResultCannotDriveChecksAfterRestart(t *testing.T) {
	ctx := context.Background()
	f, w, worker, orch := managedReportFixture(t)
	path, _ := committedWorkspace(t)
	policy := w.Policy
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "HEAD"}, TimeoutSeconds: 10}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "diff", "--exit-code", "HEAD"}, TimeoutSeconds: 10}
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.Policy, w.PolicyVersion, w.State = policy, instructorsvc.PolicyVersion(policy), "ready"
	})
	instance, _, err := f.store.InstructorSessionInstance(ctx, orch.ID)
	if err != nil {
		t.Fatal(err)
	}
	d := domain.InstructorDispatch{ID: "cancelled-started-result", BatchID: "cancelled-started-result", WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: w.Revision, Action: "worker_result", PolicyVersion: w.PolicyVersion, ReportID: "cancelled-started-report", ProducerSessionID: worker.ID, Phase: w.Phase, PhaseSequence: w.PhaseSequence, State: "cancelled", NativeReceipt: &domain.InstructorNativeReceipt{RuntimeID: "chat:orchestrator-runtime", ProviderTurnID: "cancelled-provider-turn", StartedAt: &f.now}, CreatedAt: f.now, UpdatedAt: f.now}
	d.TargetSessionID, d.TargetSessionInstanceID, d.TargetRuntimeID, d.TargetCreatedAt = orch.ID, instance, "chat:orchestrator-runtime", &orch.CreatedAt
	if err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{d}); err != nil {
		t.Fatal(err)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reviews := mechanicalReview{started: make(chan domain.InstructorWork, 1), release: make(chan struct{})}
	f.service.SetPhaseActions(reviews)
	defer func() { close(reviews.release); _ = f.service.Shutdown(ctx) }()
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.store.GetInstructorWork(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.PhaseActions) != 0 || len(current.Evidence) != 0 {
		t.Fatalf("cancelled receipt still scheduled native checks: %+v", current)
	}
	d.ID, d.BatchID, d.ReportID, d.State = "new-started-result", "new-started-result", "new-started-report", "delivered"
	d.NativeReceipt = &domain.InstructorNativeReceipt{RuntimeID: d.TargetRuntimeID, ProviderTurnID: "new-provider-turn", StartedAt: &f.now}
	if err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{d}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, _, err = f.store.GetInstructorWork(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(current.Evidence) == 2 && len(current.PhaseActions) == 1 && current.PhaseActions[0].State == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new result did not drive exact checks: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, _ = f.store.GetInstructorWork(ctx, w.ID)
	if current.Phase != "review" {
		t.Fatalf("old cancellation blocked new result transition: %+v", current)
	}
}

package instructor_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type mechanicalReview struct {
	started chan domain.InstructorWork
	release chan struct{}
}

func (r mechanicalReview) ReviewLocal(ctx context.Context, w domain.InstructorWork, _ domain.SessionID) error {
	r.started <- w
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestInstructorMechanicalDriverRunsRealProfilesThenOneIndependentReview(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, sha := committedWorkspace(t)
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "--verify", "HEAD"}, TimeoutSeconds: 10}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "diff", "--exit-code", "HEAD"}, TimeoutSeconds: 10}
	w := f.register(t, "mechanical", policy)
	worker := f.attachWorker(t, w.ID)
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) { w.Phase = "implement"; w.PhaseSequence = 1; w.State = "ready" })
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reviews := mechanicalReview{started: make(chan domain.InstructorWork, 2), release: make(chan struct{})}
	f.service.SetPhaseActions(reviews)
	defer func() { close(reviews.release); _ = f.service.Shutdown(ctx) }()
	// A trusted native receipt, not an agent green claim, enables the result action.
	d := domain.InstructorDispatch{ID: "mechanical-result", BatchID: "mechanical-result", WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: w.Revision, Action: "worker_result", PolicyVersion: w.PolicyVersion, ReportID: "native-done", ProducerSessionID: worker, Phase: w.Phase, PhaseSequence: w.PhaseSequence, State: "delivered", NativeReceipt: &domain.InstructorNativeReceipt{RuntimeID: "chat:orch", ProviderTurnID: "provider-turn", StartedAt: &f.now}, CreatedAt: f.now, UpdatedAt: f.now}
	orch, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: w.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, Metadata: domain.SessionMetadata{ControllerGeneration: "orch"}, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	instance, _, err := f.store.InstructorSessionInstance(ctx, orch.ID)
	if err != nil {
		t.Fatal(err)
	}
	d.TargetSessionID, d.TargetSessionInstanceID, d.TargetRuntimeID, d.TargetCreatedAt = orch.ID, instance, "chat:orch", &orch.CreatedAt
	if err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{d}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, _, err := f.store.GetInstructorWork(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(current.Evidence) == 2 && len(current.PhaseActions) == 1 && current.PhaseActions[0].State == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("real approved profiles not driven: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// The new phase directive must be delivered before a new action changes its revision.
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case reviewed := <-reviews.started:
		if reviewed.Phase != "review" || reviewed.SHA != sha {
			t.Fatalf("review didn't follow exact checks: %+v", reviewed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("phase engine didn't start independent local review")
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reviews.started:
		t.Fatal("another tick duplicated running native review")
	default:
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	if current.State != "ready" && current.State != "running" {
		t.Fatalf("mechanical review invalidated phase directive: state=%s gate=%+v wait=%s", current.State, current.Gate, current.WaitCondition)
	}
	if len(current.PhaseActions) != 2 {
		t.Fatalf("missing durable mechanical action audit: %+v", current.PhaseActions)
	}
}

func TestInstructorMechanicalRestartNeverRepeatsUnknownNativeEffect(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, sha := committedWorkspace(t)
	w := f.register(t, "restart-action", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, w.ID)
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State = "ready"
		w.Phase = "review"
		w.PhaseSequence = 2
		w.SHA = sha
		w.PhaseActions = []domain.InstructorPhaseAction{{ID: "mechanical-" + w.ID + "-2-review", Kind: "review", PhaseSequence: 2, Producer: worker, State: "running", StartedAt: f.now}}
	})
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reviews := mechanicalReview{started: make(chan domain.InstructorWork, 1), release: make(chan struct{})}
	f.service.SetPhaseActions(reviews)
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	if current.State != "blocked" || current.WaitCondition == "" {
		t.Fatalf("unknown native effect not escalated: %+v", current)
	}
	select {
	case <-reviews.started:
		t.Fatal("restart repeated unknown review launch")
	default:
	}
}

func TestInstructorMechanicalRestartRecoversCompletedChecksBeforePhaseDirective(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, _ := committedWorkspace(t)
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "HEAD"}, TimeoutSeconds: 10}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "diff", "--exit-code", "HEAD"}, TimeoutSeconds: 10}
	w := f.register(t, "completed-check-action", policy)
	worker := f.attachWorker(t, w.ID)
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State, w.Phase, w.PhaseSequence = "ready", "implement", 1
	})
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	if _, err := f.service.RunChecks(ctx, w.ID, worker, nil); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash before the native action completion audit, retaining
	// real profile evidence. A second live check must not supersede a running action.
	f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.PhaseActions = []domain.InstructorPhaseAction{{ID: "checks-before-restart", Kind: "checks", PhaseSequence: 1, Producer: worker, State: "running", StartedAt: f.now}}
	})
	// The native profiles committed successfully, but their completion audit was interrupted.
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reviews := mechanicalReview{started: make(chan domain.InstructorWork, 1), release: make(chan struct{})}
	f.service.SetPhaseActions(reviews)
	defer func() { close(reviews.release); _ = f.service.Shutdown(ctx) }()
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	dispatches, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Phase != "implement" || current.State != "ready" || current.PhaseActions[0].State != "completed" || len(current.Evidence) != 2 || len(dispatches) != 0 {
		t.Fatalf("running check audit advanced too early or repeated profiles: work=%+v dispatches=%+v", current, dispatches)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, _ = f.store.GetInstructorWork(ctx, w.ID)
	dispatches, _ = f.store.ListInstructorDispatches(ctx, "", w.ID)
	if current.Phase != "review" || len(current.PhaseActions) != 1 || len(dispatches) != 1 || dispatches[0].State != "pending" || dispatches[0].WorkRevision != current.Revision {
		t.Fatalf("new phase action invalidated undelivered directive: work=%+v dispatches=%+v", current, dispatches)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reviews.started:
	case <-time.After(5 * time.Second):
		t.Fatal("recovered action did not permit the next native review")
	}
	dispatches, _ = f.store.ListInstructorDispatches(ctx, "", w.ID)
	if dispatches[0].State != "delivered" {
		t.Fatalf("fresh phase directive was rejected: %+v", dispatches)
	}
}

func TestInstructorMechanicalRestartKeepsPartiallyCompletedChecksBlocked(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, _ := committedWorkspace(t)
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "HEAD"}, TimeoutSeconds: 10}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "diff", "--exit-code", "HEAD"}, TimeoutSeconds: 10}
	w := f.register(t, "partial-check-action", policy)
	worker := f.attachWorker(t, w.ID)
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State, w.Phase, w.PhaseSequence = "ready", "implement", 1
	})
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	if _, err := f.service.RunChecks(ctx, w.ID, worker, []string{"push"}); err != nil {
		t.Fatal(err)
	}
	// Native push completed before a crash; full has no positive proof.
	f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.PhaseActions = []domain.InstructorPhaseAction{{ID: "partial-checks-before-restart", Kind: "checks", PhaseSequence: 1, Producer: worker, State: "running", StartedAt: f.now}}
	})
	if _, err := f.service.RunChecks(ctx, w.ID, worker, []string{"full"}); err == nil {
		t.Fatal("direct check superseded unknown mechanical effect")
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reviews := mechanicalReview{started: make(chan domain.InstructorWork, 1), release: make(chan struct{})}
	f.service.SetPhaseActions(reviews)
	defer func() { close(reviews.release); _ = f.service.Shutdown(ctx) }()
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	dispatches, _ := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if current.State != "blocked" || current.Phase != "implement" || current.PhaseActions[0].State != "running" || len(current.Evidence) != 1 || len(dispatches) != 0 {
		t.Fatalf("partial native proof repeated a profile or advanced its phase: work=%+v dispatches=%+v", current, dispatches)
	}
}

package instructor_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

func TestReviewDeltaStopDirectiveSurvivesPhaseActionCompletion(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, sha := committedWorkspace(t)
	w := f.register(t, "stop-audit-race", instructorsvc.DefaultPolicy())
	f.attachWorker(t, w.ID)
	f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State = "ready"
		w.Phase = "review"
		w.PhaseSequence = 2
		w.SHA = sha
	})
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reviews := mechanicalReview{started: make(chan domain.InstructorWork, 1), release: make(chan struct{})}
	f.service.SetPhaseActions(reviews)
	defer func() { _ = f.service.Shutdown(ctx) }()
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reviews.started:
	case <-time.After(time.Second):
		t.Fatal("review not started")
	}
	snap := f.snapshot(t)
	if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Stopp", SnapshotID: snap.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "stop-audit-race"}); err != nil {
		t.Fatal(err)
	}
	close(reviews.release)
	deadline := time.Now().Add(time.Second)
	for {
		cur, _, _ := f.store.GetInstructorWork(ctx, w.ID)
		if cur.PhaseActions[0].State == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("audit unfinished")
		}
		time.Sleep(time.Millisecond)
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	pending, _ := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if len(pending) != 1 || pending[0].State != "pending" || pending[0].WorkRevision != current.Revision {
		t.Fatalf("benign completion audit lost frozen stop authority: work=%+v dispatches=%+v", current, pending)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	ds, _ := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if len(f.delivery.calls) != 1 || f.delivery.calls[0].Action != "stopp" {
		t.Fatalf("native stop never attempted after action completion: calls=%v dispatches=%+v", f.delivery.calls, ds)
	}
}

func TestReviewDeltaRejectedWorkerResultCanResumeAfterFreshApproval(t *testing.T) {
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
	f.delivery.err = instructorsvc.ErrDeliveryRejected
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	rejected, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil || len(rejected) != 1 || rejected[0].State != "cancelled" {
		t.Fatalf("certified no-send retained a native wake-up obligation: rows=%+v err=%v", rejected, err)
	}
	storedReport, _, err := f.store.GetReport(ctx, rejected[0].ReportID)
	if err != nil || storedReport.DeliveryState != domain.ReportClaimed {
		t.Fatalf("cancellation fabricated a native report acknowledgment: report=%+v err=%v", storedReport, err)
	}
	f.delivery.err = nil
	stop := f.snapshot(t)
	if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Go", SnapshotID: stop.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "retry-rejected"}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}

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

func TestInstructorNewResumeSupersedesStopWithoutLosingItsOwnDirective(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, sha := committedWorkspace(t)
	w := f.register(t, "superseded-stop", instructorsvc.DefaultPolicy())
	f.attachWorker(t, w.ID)
	f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State, w.Phase, w.PhaseSequence, w.SHA = "ready", "review", 2, sha
	})
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reviews := mechanicalReview{started: make(chan domain.InstructorWork, 1), release: make(chan struct{})}
	f.service.SetPhaseActions(reviews)
	defer func() { _ = f.service.Shutdown(ctx) }()
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reviews.started:
	case <-time.After(time.Second):
		t.Fatal("review not started")
	}
	stop, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Stopp", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{w.ID}, IdempotencyKey: "overridden-stop"})
	if err != nil {
		t.Fatal(err)
	}
	resume, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Weiter", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{w.ID}, IdempotencyKey: "new-resume"})
	if err != nil {
		t.Fatal(err)
	}
	close(reviews.release)
	deadline := time.Now().Add(time.Second)
	for {
		current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
		if current.PhaseActions[0].State == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("action audit unfinished")
		}
		time.Sleep(time.Millisecond)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if len(f.delivery.calls) != 1 || f.delivery.calls[0].ID != resume.Dispatches[0].ID || f.delivery.calls[0].Action != "weiter" {
		t.Fatalf("audit revived old stop or staled the new resume: calls=%+v rows=%+v", f.delivery.calls, rows)
	}
	if len(rows) != 2 || rows[0].ID != stop.Dispatches[0].ID || rows[0].State != "cancelled" || rows[1].State != "delivered" {
		t.Fatalf("superseded stop lost durable fencing: %+v", rows)
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	if current.State != "ready" || current.Gate != nil {
		t.Fatalf("obsolete stop interfered with fresh operator intent: %+v", current)
	}
}

func TestInstructorOnlyTypedNoSendCancelsWorkerWakeup(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cause     error
		uncertain bool
	}{
		{name: "lookalike-text", cause: errors.New(instructorsvc.ErrDeliveryRejected.Error())},
		{name: "actual-uncertainty", cause: fmt.Errorf("%w: %s", instructorsvc.ErrDeliveryUncertain, instructorsvc.ErrDeliveryRejected.Error()), uncertain: true},
		{name: "uncertainty-dominates-conflicting-tags", cause: errors.Join(instructorsvc.ErrDeliveryUncertain, instructorsvc.ErrDeliveryRejected), uncertain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f, w, worker, _ := managedReportFixture(t)
			createManagedDone(t, f, worker)
			f.delivery.err = tc.cause
			if err := f.service.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			rows, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
			if err != nil || len(rows) != 1 || rows[0].State != "failed" || rows[0].NativeReceipt != nil && rows[0].NativeReceipt.StartedAt != nil {
				t.Fatalf("unproved/uncertain send was discarded: rows=%+v err=%v", rows, err)
			}
			current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
			if current.State != "waiting_human" || current.Phase != "implement" || current.Gate == nil {
				t.Fatalf("uncertified send lost its visible blocker: %+v", current)
			}
			if tc.uncertain {
				if current.Gate.Kind != "delivery_uncertain" {
					t.Fatalf("unknown effect lost reconciliation requirement: %+v", current)
				}
				if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Go", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{w.ID}, IdempotencyKey: "go-with-unknown-effect"}); err == nil {
					t.Fatal("fresh generic Go bypassed unresolved native effect")
				}
			}
		})
	}
}

// The child test process is an actual approved profile, held until the operator
// command has committed. No socket, provider or network timing is involved.
func TestInstructorHeldCheckHelper(t *testing.T) {
	if len(os.Args) < 5 || os.Args[len(os.Args)-4] != "--" {
		return
	}
	ready, release, outcome := os.Args[len(os.Args)-3], os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if err := os.WriteFile(ready, []byte("started"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(release); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("profile release missing")
		}
		time.Sleep(time.Millisecond)
	}
	if outcome == "failed" {
		t.Fatal("approved profile failure fixture")
	}
}

func TestInstructorStopSurvivesRunningProfileReceipts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		resume    bool
		tickEarly bool
	}{
		{name: "executed"},
		{name: "failed"},
		{name: "executed", tickEarly: true},
		{name: "executed", resume: true},
	} {
		name := tc.name
		if tc.resume {
			name += "-superseded-stop"
		}
		if tc.tickEarly {
			name += "-native-waits-for-profile"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f, w, worker, orch := managedReportFixture(t)
			path, sha := committedWorkspace(t)
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			markers := t.TempDir()
			ready, release := filepath.Join(markers, "ready"), filepath.Join(markers, "release")
			policy := w.Policy
			role := policy.Roles["worker"]
			role.EffortByModel = map[string]string{"provider/worker": "high"}
			policy.Roles["worker"] = role
			policy.Profiles["push"] = domain.InstructorProfile{Command: []string{binary, "-test.run=^TestInstructorHeldCheckHelper$", "--", ready, release, tc.name}, TimeoutSeconds: 15}
			policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "HEAD"}, TimeoutSeconds: 10}
			w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
				w.Policy, w.PolicyVersion, w.State, w.SHA = policy, instructorsvc.PolicyVersion(policy), "ready", sha
			})
			instance, _, err := f.store.InstructorSessionInstance(ctx, orch.ID)
			if err != nil {
				t.Fatal(err)
			}
			d := domain.InstructorDispatch{ID: "held-result", BatchID: "held-result", WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: w.Revision, Action: "worker_result", PolicyVersion: w.PolicyVersion, ReportID: "held-report", ProducerSessionID: worker.ID, Phase: w.Phase, PhaseSequence: w.PhaseSequence, State: "delivered", NativeReceipt: &domain.InstructorNativeReceipt{RuntimeID: "chat:orchestrator-runtime", ProviderTurnID: "held-native-turn", StartedAt: &f.now}, CreatedAt: f.now, UpdatedAt: f.now}
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
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("approved native profile did not start")
				}
				time.Sleep(time.Millisecond)
			}
			stop, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Stopp", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{w.ID}, IdempotencyKey: "stop-held-profile"})
			if err != nil {
				t.Fatal(err)
			}
			if tc.resume {
				if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Weiter", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{w.ID}, IdempotencyKey: "resume-held-profile"}); err != nil {
					t.Fatal(err)
				}
			}
			var tickDone chan error
			var releaseNative chan struct{}
			if tc.tickEarly {
				entered := make(chan struct{})
				releaseNative = make(chan struct{})
				f.delivery.bind = func(ctx context.Context, d domain.InstructorDispatch) error {
					if d.Action != "stopp" {
						return nil
					}
					close(entered)
					select {
					case <-releaseNative:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				tickDone = make(chan error, 1)
				go func() { tickDone <- f.service.Tick(stopCtx) }()
				select {
				case <-entered:
				case <-stopCtx.Done():
					t.Fatal("native pause never attempted while profile was running")
				}
			}
			if err := os.WriteFile(release, []byte("finish"), 0600); err != nil {
				t.Fatal(err)
			}
			deadline = time.Now().Add(5 * time.Second)
			for {
				current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
				if len(current.PhaseActions) == 1 && current.PhaseActions[0].State != "running" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("profile could not commit while stopped: %+v", current)
				}
				time.Sleep(time.Millisecond)
			}
			if tc.tickEarly {
				// Native control can wait for runtime acknowledgment while profile
				// saves acquire the work lock; neither side may deadlock.
				close(releaseNative)
				if err := <-tickDone; err != nil {
					t.Fatal(err)
				}
			} else if err := f.service.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			stopAttempted := false
			for _, call := range f.delivery.calls {
				stopAttempted = stopAttempted || call.ID == stop.Dispatches[0].ID
			}
			if stopAttempted == tc.resume {
				t.Fatalf("running profile receipt changed stop authority: resumed=%v calls=%+v", tc.resume, f.delivery.calls)
			}
			current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
			if !tc.resume && (current.State != "parked" || len(current.Evidence) != 0) {
				t.Fatalf("stopped native profile committed green proof or resumed work: %+v", current)
			}
			if tc.resume && (len(f.delivery.calls) != 0 || current.State != "waiting_human" || len(current.Evidence) != 2) {
				t.Fatalf("new native evidence was treated as harmless audit for Go/Weiter: work=%+v calls=%+v", current, f.delivery.calls)
			}
		})
	}
}

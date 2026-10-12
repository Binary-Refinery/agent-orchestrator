package daemon

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type cancellationActualControl struct {
	stopBoundaryActualControl
	quiet bool
}

func (c *cancellationActualControl) InstructorProducerQuiescent(context.Context, domain.SessionID, string) (bool, error) {
	return c.quiet, nil
}

func TestInstructorActualCancellationControlsOnlyOwnedCurrentWorkers(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitetest.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 10, 12, 22, 59, 0, 0, time.UTC)
	if err := st.UpsertProject(ctx, domain.ProjectRecord{ID: "p", Path: t.TempDir(), RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	control := &cancellationActualControl{}
	delivery := &instructorDelivery{store: st, sessions: control}
	svc := instructorsvc.New(instructorsvc.Deps{Store: st, Delivery: delivery, Clock: func() time.Time { return now }})
	delivery.binder = svc
	w, err := svc.Register(ctx, instructorsvc.RegisterInput{ProjectID: "p", Name: "cancel current", Goal: "stop owned work", Source: "test", SourceRef: "native cancellation", Policy: instructorsvc.DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	var workers []domain.SessionID
	for _, kind := range []domain.SessionKind{domain.KindWorker, domain.KindWorker, domain.KindOrchestrator, domain.KindWorker} {
		r, err := st.CreateSession(ctx, domain.SessionRecord{ProjectID: "p", Kind: kind, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, Metadata: domain.SessionMetadata{ControllerGeneration: "native-controller"}, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		if len(workers) < 2 || kind == domain.KindOrchestrator {
			if err := svc.AttachSession(ctx, w.ID, r.ID); err != nil {
				t.Fatal(err)
			}
		}
		if kind == domain.KindWorker && len(workers) < 2 {
			workers = append(workers, r.ID)
		}
	}
	snap, err := svc.Snapshot(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: snap.ID, IdempotencyKey: "native cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err := st.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State != "parked" || !slices.Equal(control.interrupts, workers) {
		t.Fatalf("cancel crossed membership/control authority: work=%+v attempted=%v %v", current, control.interrupts, err)
	}
	control.quiet = true
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err = st.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State != "cancelled" || current.Cancellation.CompletedAt == nil {
		t.Fatalf("native proof did not complete cancellation: %+v %v", current, err)
	}
	if err := svc.Tick(ctx); err != nil || len(control.interrupts) != 2 {
		t.Fatalf("settled cancellation duplicated native interrupt: %v %v", control.interrupts, err)
	}
}

func TestInstructorCancellationObserverRejectsReplacedMissingAndIdleTUI(t *testing.T) {
	for _, tc := range []struct {
		name      string
		state     domain.ActivityState
		found     bool
		instance  string
		wantQuiet bool
	}{
		{"replaced", domain.ActivityIdle, true, "replacement", false},
		{"missing", domain.ActivityExited, false, "", false},
		{"idle TUI", domain.ActivityIdle, true, "original", false},
		{"positive exit", domain.ActivityExited, true, "original", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := nativePauseStore{instructorTestStore: instructorTestStore{}, members: []domain.InstructorSessionMembership{{SessionID: "worker", InstanceID: "original", ProjectID: "p", Role: "worker", Provenance: "admitted"}}, instances: map[domain.SessionID]string{"worker": tc.instance}}
			if tc.found {
				st.sessions = []domain.SessionRecord{{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, Mode: domain.SessionModeTUI, Activity: domain.Activity{State: tc.state}}}
			}
			control := &cancellationActualControl{}
			proof, quiet, err := (instructorDelivery{store: st, sessions: control}).ObserveCancellation(context.Background(), domain.InstructorWork{ID: "w", ProjectID: "p", SessionIDs: []domain.SessionID{"worker"}})
			if quiet != tc.wantQuiet || quiet && len(proof.Producers) != 1 {
				t.Fatalf("native quiet incorrectly inferred: %+v quiet=%v %v", proof, quiet, err)
			}
			if (!tc.found || tc.instance != "original") && !errors.Is(err, instructorsvc.ErrDeliveryUncertain) {
				t.Fatalf("unknown producer hidden: %v", err)
			}
			if len(control.interrupts) != 0 {
				t.Fatal("observation interrupted replacement")
			}
		})
	}
}

type cancellationActualReviews struct {
	quiet       bool
	interrupted []string
}

func (r *cancellationActualReviews) InterruptInstructorReviewer(_ context.Context, review domain.Review) error {
	r.interrupted = append(r.interrupted, review.ID)
	return nil
}
func (r *cancellationActualReviews) InstructorReviewerQuiescent(context.Context, domain.Review) (bool, error) {
	return r.quiet, nil
}

func TestInstructorActualCancellationWaitsForNativeReviewerAndCancelsOnlyCapturedRun(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitetest.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	if err := st.UpsertProject(ctx, domain.ProjectRecord{ID: "p", Path: t.TempDir(), RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	control := &cancellationActualControl{quiet: true}
	reviews := &cancellationActualReviews{}
	delivery := &instructorDelivery{store: st, sessions: control, reviews: reviews}
	svc := instructorsvc.New(instructorsvc.Deps{Store: st, Delivery: delivery, Clock: func() time.Time { return now }})
	delivery.binder = svc
	w, err := svc.Register(ctx, instructorsvc.RegisterInput{ProjectID: "p", Name: "review cancel", Goal: "cancel current reviewer", Source: "test", SourceRef: "review cancellation", Policy: instructorsvc.DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := st.CreateSession(ctx, domain.SessionRecord{ProjectID: "p", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, Metadata: domain.SessionMetadata{ControllerGeneration: "worker-controller"}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AttachSession(ctx, w.ID, worker.ID); err != nil {
		t.Fatal(err)
	}
	review := domain.Review{ID: "review-current", SessionID: worker.ID, ProjectID: "p", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-chat:review-current", ReviewerLaunchID: "native-review-launch", ControllerGeneration: "review-controller", InterfaceMode: domain.ReviewerInterfaceChat, ReviewerActivityState: domain.ActivityActive, CreatedAt: now, UpdatedAt: now}
	if err := st.UpsertReview(ctx, review); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{ID: "captured-run", ReviewID: review.ID, SessionID: worker.ID, Harness: review.Harness, TargetSHA: "1111111111111111111111111111111111111111", Status: domain.ReviewRunRunning, CreatedAt: now, ScopeKey: w.ID + "/review/1"}
	if err := st.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordLaunch(ctx, w.ID, domain.InstructorLaunch{ReviewID: review.ID, ReviewRunID: run.ID, Role: "reviewer", Status: "launched", SHA: run.TargetSHA, RuntimeGeneration: review.ReviewerLaunchID}); err != nil {
		t.Fatal(err)
	}
	snap, err := svc.Snapshot(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: snap.ID, IdempotencyKey: "cancel reviewer"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err := st.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State != "parked" || !slices.Equal(reviews.interrupted, []string{review.ID}) {
		t.Fatalf("reviewer cancel admission: %+v %v %v", current, reviews.interrupted, err)
	}
	retained, _, err := st.GetReviewRun(ctx, run.ID)
	if err != nil || retained.Status != domain.ReviewRunRunning {
		t.Fatalf("interrupt admission erased native uncertainty: %+v %v", retained, err)
	}
	reviews.quiet = true
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err = st.GetInstructorWork(ctx, w.ID)
	retained, _, runErr := st.GetReviewRun(ctx, run.ID)
	if err != nil || runErr != nil || current.State != "cancelled" || retained.Status != domain.ReviewRunCancelled || len(current.Launches) != 1 {
		t.Fatalf("native reviewer completion/history: %+v %+v %v %v", current, retained, err, runErr)
	}
}

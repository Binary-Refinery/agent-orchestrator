package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type stopBoundaryActualStore struct {
	*sqlite.Store
	beforeRead func(context.Context)
}

func (s *stopBoundaryActualStore) GetInstructorWork(ctx context.Context, id string) (domain.InstructorWork, bool, error) {
	if s.beforeRead != nil {
		fn := s.beforeRead
		s.beforeRead = nil
		fn(ctx)
	}
	return s.Store.GetInstructorWork(ctx, id)
}

type stopBoundaryActualControl struct {
	reportDeliverySession
	interrupts     []domain.SessionID
	interruptError error
}

func (*stopBoundaryActualControl) InstructorControllerReady(domain.SessionRecord) bool { return true }

func (c *stopBoundaryActualControl) InterruptInstructorProducer(_ context.Context, id domain.SessionID, _ string) error {
	c.interrupts = append(c.interrupts, id)
	if len(c.interrupts) == 1 {
		return c.interruptError
	}
	return nil
}

func TestIndependentDeltaActualDaemonResumeBeforeInterruptDoesNotLoseIntent(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitetest.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	if err := st.UpsertProject(ctx, domain.ProjectRecord{ID: "p", Path: t.TempDir(), RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	ctl := &stopBoundaryActualControl{}
	wrapped := &stopBoundaryActualStore{Store: st}
	delivery := &instructorDelivery{store: wrapped, sessions: ctl}
	svc := instructorsvc.New(instructorsvc.Deps{Store: st, Delivery: delivery, Clock: func() time.Time { return now }})
	delivery.binder = svc
	w, err := svc.Register(ctx, instructorsvc.RegisterInput{ProjectID: "p", Name: "actual-stop-boundary", Goal: "pause owned worker", Source: "test", SourceRef: "boundary", Policy: instructorsvc.DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := st.CreateSession(ctx, domain.SessionRecord{ProjectID: "p", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, Mode: domain.SessionModeTUI, Activity: domain.Activity{State: domain.ActivityActive}, Metadata: domain.SessionMetadata{RuntimeLaunchID: "worker-runtime"}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AttachSession(ctx, w.ID, worker.ID); err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateSession(ctx, domain.SessionRecord{ProjectID: "p", Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, Activity: domain.Activity{State: domain.ActivityIdle}, Metadata: domain.SessionMetadata{ControllerGeneration: "orch-runtime"}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := svc.Snapshot(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Command(ctx, instructorsvc.CommandInput{Command: "Stopp", SnapshotID: snap.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "old-stop"})
	if err != nil {
		t.Fatal(err)
	}
	var resume domain.InstructorCommandResult
	wrapped.beforeRead = func(ctx context.Context) {
		snap, err := svc.Snapshot(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		resume, err = svc.Command(ctx, instructorsvc.CommandInput{Command: "Weiter", SnapshotID: snap.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "new-resume"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, _ := st.GetInstructorWork(ctx, w.ID)
	rows, _ := st.ListInstructorDispatches(ctx, "", w.ID)
	if len(ctl.interrupts) != 0 {
		t.Fatalf("obsolete stop still interrupted: %v", ctl.interrupts)
	}
	if current.Gate != nil || len(rows) < 2 || rows[0].State != "cancelled" || rows[1].ID != resume.Dispatches[0].ID || rows[1].State != "delivered" {
		t.Fatalf("pre-native obsolete Stop defeated actual fresh Weiter: state=%s gate=%+v rows=%+v", current.State, current.Gate, rows)
	}
}

func TestInstructorActualNativeStopFailureAfterFirstAttemptRemainsUncertain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
	}{
		{name: "native-conflict", cause: instructorsvc.ErrConflict},
		{name: "native-superseded-tag", cause: instructorsvc.ErrStopSuperseded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := sqlitetest.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
			if err := st.UpsertProject(ctx, domain.ProjectRecord{ID: "p", Path: t.TempDir(), RegisteredAt: now}); err != nil {
				t.Fatal(err)
			}
			ctl := &stopBoundaryActualControl{interruptError: tc.cause}
			delivery := &instructorDelivery{store: st, sessions: ctl}
			svc := instructorsvc.New(instructorsvc.Deps{Store: st, Delivery: delivery, Clock: func() time.Time { return now }})
			delivery.binder = svc
			w, err := svc.Register(ctx, instructorsvc.RegisterInput{ProjectID: "p", Name: "partial-native-stop", Goal: "pause owned workers", Source: "test", SourceRef: "partial", Policy: instructorsvc.DefaultPolicy()})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				worker, err := st.CreateSession(ctx, domain.SessionRecord{ProjectID: "p", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, Mode: domain.SessionModeTUI, Activity: domain.Activity{State: domain.ActivityActive}, Metadata: domain.SessionMetadata{RuntimeLaunchID: "worker-runtime"}, CreatedAt: now, UpdatedAt: now})
				if err != nil {
					t.Fatal(err)
				}
				if err := svc.AttachSession(ctx, w.ID, worker.ID); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := svc.Snapshot(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			stop, err := svc.Command(ctx, instructorsvc.CommandInput{Command: "Stopp", SnapshotID: snapshot.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "partial-native-pause"})
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			current, _, err := st.GetInstructorWork(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := st.ListInstructorDispatches(ctx, "", w.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(ctl.interrupts) != 2 || len(rows) != 1 || rows[0].State != "failed" || current.State != "parked" || current.Gate == nil || current.Gate.Kind != "delivery_uncertain" || current.Gate.DispatchID != stop.Dispatches[0].ID {
				t.Fatalf("partial native effect was discarded as no-send: attempts=%v rows=%+v work=%+v", ctl.interrupts, rows, current)
			}
			if !errors.Is(tc.cause, instructorsvc.ErrConflict) {
				t.Fatal("negative fixture must carry the same conflict/superseded error class")
			}
			if err := svc.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			if len(ctl.interrupts) != 2 {
				t.Fatal("uncertain native pause was automatically repeated")
			}
		})
	}
}

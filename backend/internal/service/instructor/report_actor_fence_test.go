package instructor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type reportActorRaceStore struct {
	*sqlite.Store
	beforeWrite func()
}

func (s *reportActorRaceStore) race() {
	if s.beforeWrite != nil {
		f := s.beforeWrite
		s.beforeWrite = nil
		f()
	}
}
func (s *reportActorRaceStore) SaveInstructorReport(ctx context.Context, w domain.InstructorWork, expected int64, e domain.InstructorEvent, a domain.InstructorReportAdmission, state string) (bool, error) {
	s.race()
	return s.Store.SaveInstructorReport(ctx, w, expected, e, a, state)
}
func (s *reportActorRaceStore) PutInstructorHeartbeatForReport(ctx context.Context, h domain.InstructorHeartbeat, a domain.InstructorReportAdmission) error {
	s.race()
	return s.Store.PutInstructorHeartbeatForReport(ctx, h, a)
}

func TestInstructorReportFinalWriterRejectsReusedProducerWithoutWorkRevisionChange(t *testing.T) {
	for _, kind := range []string{"progress", "heartbeat", "accepted"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := newInstructorFixture(t)
			w := f.register(t, "actor-fence", instructorsvc.DefaultPolicy())
			sid := f.attachWorker(t, w.ID)
			actor, _, err := f.store.GetSession(ctx, sid)
			if err != nil {
				t.Fatal(err)
			}
			dispatchID := ""
			if kind == "accepted" {
				actor, err = f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: w.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, CreatedAt: f.now, UpdatedAt: f.now})
				if err != nil {
					t.Fatal(err)
				}
				w, _, _ = f.store.GetInstructorWork(ctx, w.ID)
				d := domain.InstructorDispatch{ID: "actor-dispatch", BatchID: "actor-dispatch", WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: w.Revision, State: "delivered", CreatedAt: f.now, UpdatedAt: f.now}
				if err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{d}); err != nil {
					t.Fatal(err)
				}
				if err := f.service.BindDispatchTarget(ctx, d.ID, actor.ID); err != nil {
					t.Fatal(err)
				}
				dispatchID = d.ID
			}
			before, _, _ := f.store.GetInstructorWork(ctx, w.ID)
			oldInstance, _, _ := f.store.InstructorSessionInstance(ctx, actor.ID)
			racing := &reportActorRaceStore{Store: f.store, beforeWrite: func() {
				if deleted, err := f.store.DeleteSession(ctx, actor.ID); err != nil || !deleted {
					t.Fatalf("delete=%v %v", deleted, err)
				}
				replacement, err := f.store.CreateSession(ctx, actor)
				if err != nil || replacement.ID != actor.ID {
					t.Fatalf("replacement=%+v %v", replacement, err)
				}
				newInstance, _, _ := f.store.InstructorSessionInstance(ctx, replacement.ID)
				if newInstance == oldInstance {
					t.Fatal("fixture didn't replace incarnation")
				}
			}}
			f.now = f.now.Add(time.Second)
			svc := instructorsvc.New(instructorsvc.Deps{Store: racing, Delivery: f.delivery, Clock: func() time.Time { return f.now }})
			_, err = svc.Report(ctx, instructorsvc.ReportInput{WorkID: w.ID, SessionID: actor.ID, Kind: kind, Summary: "replacement must not mutate original work", DispatchID: dispatchID})
			if !errors.Is(err, instructorsvc.ErrConflict) {
				t.Fatalf("ABA didn't fail with typed conflict: %v", err)
			}
			after, _, _ := f.store.GetInstructorWork(ctx, w.ID)
			if after.Revision != before.Revision || after.Summary != before.Summary || !after.ProgressAt.Equal(before.ProgressAt) {
				t.Fatalf("replacement mutated work: before=%+v after=%+v", before, after)
			}
			if kind == "heartbeat" {
				if _, found, _ := f.store.GetInstructorHeartbeat(ctx, w.ID); found {
					t.Fatal("replacement heartbeat was admitted")
				}
			}
			if kind == "accepted" {
				ds, _ := f.store.ListInstructorDispatches(ctx, "", w.ID)
				if ds[0].State != "delivered" {
					t.Fatalf("replacement acknowledged old target: %+v", ds[0])
				}
			}
		})
	}
}

func TestInstructorCheckProducerEpochChangeCannotCommitGreenEvidence(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, sha := committedWorkspace(t)
	w := f.register(t, "check-fence", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, w.ID)
	producer, admission, found, err := f.store.CaptureInstructorCheckProducer(ctx, w.ID, sid)
	if err != nil || !found {
		t.Fatalf("capture %v %v", found, err)
	}
	// Same inserted session, changed execution resource: instance-only fences are insufficient.
	producer.Metadata.WorkspacePath = path
	producer.Metadata.RuntimeLaunchID = "replacement-runtime"
	if err := f.store.UpdateSession(ctx, producer); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ValidateInstructorCheckProducer(ctx, admission, producer); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("epoch change not rejected: %v", err)
	}
	w, _, _ = f.store.GetInstructorWork(ctx, w.ID)
	expected := w.Revision
	w.SHA = sha
	w.Revision++
	w.Evidence = []domain.InstructorEvidence{{ID: "forged-after-epoch", Kind: "check", Profile: "full", SHA: sha, Verified: true}}
	updated, err := f.store.SaveInstructorReport(ctx, w, expected, domain.InstructorEvent{ID: "epoch-event", WorkID: w.ID, At: f.now}, admission, "")
	if updated || !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("epoch proof committed: %v %v", updated, err)
	}
	stored, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	if stored.Revision != expected || len(stored.Evidence) != 0 {
		t.Fatalf("partial green evidence=%+v", stored)
	}
}

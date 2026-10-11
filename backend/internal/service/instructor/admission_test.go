package instructor_test

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorNativeQueueRejectsDirectiveAfterWorkRevisionChanges(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "queued-revision", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	target, err := f.store.CreateSession(context.Background(), domain.SessionRecord{ProjectID: "governed", Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Weiter", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{work.ID}, IdempotencyKey: "queued-before-progress"})
	if err != nil || len(batch.Dispatches) != 1 {
		t.Fatalf("admit queued directive: %+v err=%v", batch, err)
	}
	if err = f.service.BindDispatchTarget(context.Background(), batch.Dispatches[0].ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.ValidateDispatchAdmission(context.Background(), target.ID, batch.Dispatches[0].ID); err != nil {
		t.Fatalf("current directive unexpectedly rejected: %v", err)
	}
	if _, err = f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "progress", SHA: immutableSHA, Branch: "feature/new-observed-state", Summary: "New commit arrived while the orchestrator was busy"}); err != nil {
		t.Fatal(err)
	}
	if err = f.service.ValidateDispatchAdmission(context.Background(), target.ID, batch.Dispatches[0].ID); err == nil {
		t.Fatal("native queue sent an old frozen directive after its work revision changed")
	}
	if err = f.service.ValidateDispatchAdmission(context.Background(), target.ID, "ordinary-unmanaged-client-key"); err != nil {
		t.Fatalf("managed admission changed ordinary messages: %v", err)
	}
}

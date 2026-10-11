package store_test

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestInstructorOriginLinkSurvivesRestartWithoutSession(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seedProject(t, store, "ao")
	work := domain.InstructorWork{ID: "work", ProjectID: "ao", Source: "automation", SourceRef: "run", Revision: 1}
	if _, err := store.CreateInstructorWork(ctx, work, domain.InstructorEvent{ID: "event", WorkID: "work"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	linked, found, err := store.InstructorWorkByOrigin(ctx, "ao", "automation", "run")
	if err != nil || !found || linked.ID != "work" || len(linked.SessionIDs) != 0 {
		t.Fatalf("work=%+v found=%t err=%v", linked, found, err)
	}
	if _, found, err := store.InstructorWorkByOrigin(ctx, "ao", "manual", "run"); err != nil || found {
		t.Fatalf("wrong origin found=%t err=%v", found, err)
	}
}

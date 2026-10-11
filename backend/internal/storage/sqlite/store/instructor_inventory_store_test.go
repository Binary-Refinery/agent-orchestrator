package store_test

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestInstructorInventoryCachePersistsCapturedFactsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"capturedAt":"2026-10-11T00:00:00Z","resources":[{"id":"branch","lastSeenAt":"2026-10-11T00:00:00Z","status":"not-observed"}]}`)
	if err := store.PutInstructorInventoryCache(context.Background(), body); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restored, found, err := store.GetInstructorInventoryCache(context.Background())
	if err != nil || !found || string(restored) != string(body) {
		t.Fatalf("found=%v error=%v body=%s", found, err, restored)
	}
	if err := store.PutInstructorInventoryCache(context.Background(), []byte("not json")); err == nil {
		t.Fatal("invalid snapshot was stored")
	}
}

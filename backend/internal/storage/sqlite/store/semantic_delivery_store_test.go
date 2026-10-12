package store_test

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestSemanticDeliveryLedgerSurvivesRestartAndNewRuntime(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seedProject(t, store, "ao")
	session, err := store.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := store.BeginSemanticDelivery(ctx, session.ID, "accepted-key", "launch-1"); err != nil || !fresh {
		t.Fatalf("begin fresh=%v err=%v", fresh, err)
	}
	if err := store.AcceptSemanticDelivery(ctx, session.ID, "accepted-key", "launch-1"); err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := store.BeginSemanticDelivery(ctx, session.ID, "uncertain-key", "launch-1"); err != nil || !fresh {
		t.Fatalf("begin pending fresh=%v err=%v", fresh, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	accepted, fresh, err := store.BeginSemanticDelivery(ctx, session.ID, "accepted-key", "launch-2")
	if err != nil || fresh || accepted.State != "accepted" || accepted.RuntimeLaunchID != "launch-1" {
		t.Fatalf("accepted=%+v fresh=%v err=%v", accepted, fresh, err)
	}
	pending, fresh, err := store.BeginSemanticDelivery(ctx, session.ID, "uncertain-key", "launch-2")
	if err != nil || fresh || pending.State != "pending" || pending.RuntimeLaunchID != "launch-1" {
		t.Fatalf("pending=%+v fresh=%v err=%v", pending, fresh, err)
	}
	if err := store.AcceptSemanticDelivery(ctx, session.ID, "uncertain-key", "launch-2"); err == nil {
		t.Fatal("accepted a different runtime's checkpoint")
	}
	if err := store.ResolveSemanticDelivery(ctx, session.ID, "accepted-key", false); err == nil {
		t.Fatal("downgraded accepted native delivery")
	}
	if err := store.ResolveSemanticDelivery(ctx, session.ID, "uncertain-key", false); err != nil {
		t.Fatal(err)
	}
	cancelled, fresh, err := store.BeginSemanticDelivery(ctx, session.ID, "uncertain-key", "launch-3")
	if err != nil || fresh || cancelled.State != "cancelled" {
		t.Fatalf("cancelled=%+v fresh=%v err=%v", cancelled, fresh, err)
	}
}

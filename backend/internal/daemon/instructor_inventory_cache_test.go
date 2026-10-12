package daemon

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func waitInstructorInventory(t *testing.T, cache *instructorInventoryCache, match func(controllers.InstructorInventoryResponse) bool) controllers.InstructorInventoryResponse {
	t.Helper()
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		snapshot := cache.read(context.Background())
		if match(snapshot) {
			return snapshot
		}
		select {
		case <-deadline:
			t.Fatal("background inventory did not produce expected facts")
		case <-ticker.C:
		}
	}
}

func TestInstructorInventoryBackgroundTracksGitWithoutGUIAndSurvivesRestart(t *testing.T) {
	repo := instructorTestRepository(t)
	instructorTestGit(t, repo, "branch", "forgotten")
	store := sqlitetest.MustOpen(t)
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{ID: "project", Path: repo, RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	collector := instructorInventory{store: store}
	var scans atomic.Int64
	cache := newInstructorInventoryCache(func(ctx context.Context) (controllers.InstructorInventoryResponse, error) {
		scans.Add(1)
		return collector.collectInventory(ctx)
	}, store)
	cache.interval = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := cache.start(ctx, true, nil)
	initial := waitInstructorInventory(t, cache, func(snapshot controllers.InstructorInventoryResponse) bool {
		for _, resource := range snapshot.Resources {
			if resource.Kind == "branch" && resource.Branch == "forgotten" && resource.Status == "observed" {
				return true
			}
		}
		return false
	})
	// No GUI/HTTP request has been used to drive collection. A later external
	// branch removal is observed by the daemon's independent timer.
	instructorTestGit(t, repo, "branch", "-D", "forgotten")
	later := waitInstructorInventory(t, cache, func(snapshot controllers.InstructorInventoryResponse) bool {
		for _, resource := range snapshot.Resources {
			if resource.Kind == "branch" && resource.Branch == "forgotten" && resource.Status == "not-observed" {
				return true
			}
		}
		return false
	})
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("inventory loop did not stop")
	}
	if scans.Load() < 2 || !later.CapturedAt.After(initial.CapturedAt) {
		t.Fatalf("scans=%d initial=%s later=%s", scans.Load(), initial.CapturedAt, later.CapturedAt)
	}
	restarted := newInstructorInventoryCache(func(context.Context) (controllers.InstructorInventoryResponse, error) {
		t.Fatal("cache read started Git scan")
		return controllers.InstructorInventoryResponse{}, nil
	}, store)
	restored := restarted.read(context.Background())
	if restored.Enabled || !restored.Cached {
		t.Fatalf("restored=%+v", restored)
	}
	for _, resource := range restored.Resources {
		if resource.Kind == "branch" && resource.Branch == "forgotten" {
			if resource.Status != "not-observed" || resource.NotObservedSince == nil || resource.LastSeenAt.IsZero() {
				t.Fatalf("forgotten resource was lost: %+v", resource)
			}
			return
		}
	}
	t.Fatal("forgotten branch disappeared after restart")
}

func TestInstructorInventoryFeatureOffAndReadsNeverScanGit(t *testing.T) {
	var scans atomic.Int64
	store := sqlitetest.MustOpen(t)
	cache := newInstructorInventoryCache(func(context.Context) (controllers.InstructorInventoryResponse, error) {
		scans.Add(1)
		return controllers.InstructorInventoryResponse{}, nil
	}, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	<-cache.start(ctx, false, nil)
	inventory := instructorInventory{store: store, cache: cache}
	for range 3 {
		snapshot, err := inventory.Inventory(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Enabled || snapshot.Cached || snapshot.NextScanAt != nil {
			t.Fatalf("disabled=%+v", snapshot)
		}
	}
	if scans.Load() != 0 {
		t.Fatalf("disabled feature performed %d scans", scans.Load())
	}
}

func TestInstructorInventoryReadRemainsAvailableDuringBackgroundScan(t *testing.T) {
	started := make(chan struct{})
	cache := newInstructorInventoryCache(func(ctx context.Context) (controllers.InstructorInventoryResponse, error) {
		close(started)
		<-ctx.Done()
		return controllers.InstructorInventoryResponse{}, ctx.Err()
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := cache.start(ctx, true, nil)
	<-started
	readDone := make(chan struct{})
	go func() { _ = cache.read(context.Background()); close(readDone) }()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("read blocked behind Git scan")
	}
	cancel()
	<-done
}

func TestInstructorInventoryReconciliationIsFencedDurableAndReopensOnDiscovery(t *testing.T) {
	store := sqlitetest.MustOpen(t)
	observed := true
	clock := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	cache := newInstructorInventoryCache(func(context.Context) (controllers.InstructorInventoryResponse, error) {
		resources := []controllers.InstructorGitResourceInventory{}
		if observed {
			resources = append(resources, controllers.InstructorGitResourceInventory{ID: "branch", Kind: "branch", Branch: "forgotten", Status: "observed", Assignment: "unassigned"})
		}
		return controllers.InstructorInventoryResponse{CapturedAt: clock, Resources: resources}, nil
	}, store)
	cache.now = func() time.Time { return clock }
	if err := cache.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	lastSeen := cache.read(context.Background()).Resources[0].LastSeenAt
	if err := cache.reconcile(context.Background(), "branch", lastSeen); err == nil {
		t.Fatal("still-observed branch was reconciled")
	}
	observed = false
	clock = clock.Add(time.Minute)
	if err := cache.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := cache.reconcile(context.Background(), "branch", lastSeen.Add(time.Second)); err == nil {
		t.Fatal("stale reconciliation was accepted")
	}
	if err := cache.reconcile(context.Background(), "branch", lastSeen); err != nil {
		t.Fatal(err)
	}
	restored := newInstructorInventoryCache(nil, store).read(context.Background())
	if len(restored.Resources) != 1 || restored.Resources[0].ReconciledAt == nil || restored.Resources[0].Status != "reconciled" || restored.UnassignedResourceCount != 0 {
		t.Fatalf("restored=%+v", restored)
	}
	observed = true
	clock = clock.Add(time.Minute)
	if err := cache.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened := cache.read(context.Background()).Resources[0]
	if reopened.ReconciledAt != nil || reopened.NotObservedSince != nil || reopened.Status != "observed" || !reopened.FirstSeenAt.Equal(lastSeen) {
		t.Fatalf("reopened=%+v", reopened)
	}
}

func TestInstructorInventoryFailedScanPreservesFactsAndReportsError(t *testing.T) {
	store := sqlitetest.MustOpen(t)
	fail := false
	cache := newInstructorInventoryCache(func(context.Context) (controllers.InstructorInventoryResponse, error) {
		if fail {
			return controllers.InstructorInventoryResponse{}, errors.New("registry unavailable")
		}
		return controllers.InstructorInventoryResponse{CapturedAt: time.Now().UTC(), Resources: []controllers.InstructorGitResourceInventory{{ID: "branch", Status: "observed", Assignment: "unassigned"}}}, nil
	}, store)
	if err := cache.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := cache.read(context.Background())
	fail = true
	if err := cache.scan(context.Background()); err == nil {
		t.Fatal("failed scan returned success")
	}
	after := newInstructorInventoryCache(nil, store).read(context.Background())
	if !after.CapturedAt.Equal(before.CapturedAt) || len(after.Resources) != 1 || after.Resources[0].Status != "observed" || !strings.Contains(after.ScanError, "registry unavailable") {
		t.Fatalf("after=%+v", after)
	}
}

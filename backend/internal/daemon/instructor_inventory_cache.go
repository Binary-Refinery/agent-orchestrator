package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

type instructorInventoryPersistence interface {
	GetInstructorInventoryCache(context.Context) ([]byte, bool, error)
	PutInstructorInventoryCache(context.Context, []byte) error
}

// The cache owns captured Git observations, including discoveries no longer
// observed. Status/GUI reads never trigger an external scan or a worker turn.
type instructorInventoryCache struct {
	mu       sync.RWMutex
	loadMu   sync.Mutex
	writeMu  sync.Mutex
	collect  func(context.Context) (controllers.InstructorInventoryResponse, error)
	persist  instructorInventoryPersistence
	snapshot controllers.InstructorInventoryResponse
	loaded   bool
	enabled  bool
	interval time.Duration
	now      func() time.Time
}

func newInstructorInventoryCache(collect func(context.Context) (controllers.InstructorInventoryResponse, error), store any) *instructorInventoryCache {
	persist, _ := store.(instructorInventoryPersistence)
	return &instructorInventoryCache{collect: collect, persist: persist, interval: 60 * time.Second, now: func() time.Time { return time.Now().UTC() }}
}

func (i instructorInventory) collectInventory(ctx context.Context) (controllers.InstructorInventoryResponse, error) {
	result, err := i.collectInventoryMetadata(ctx)
	if err != nil {
		return result, err
	}
	projects, err := i.store.ListProjects(ctx)
	if err != nil {
		return result, err
	}
	sessions, err := i.store.ListAllSessions(ctx)
	if err != nil {
		return result, err
	}
	works, err := i.store.ListInstructorWorks(ctx)
	if err != nil {
		return result, err
	}
	result.Resources, result.UnassignedResourceCount = i.gitInventory(ctx, projects, sessions, works)
	return result, nil
}

func (i instructorInventory) Inventory(ctx context.Context) (controllers.InstructorInventoryResponse, error) {
	// Project/session rows remain a cheap read of durable facts even when the
	// new control plane is disabled. External observations keep their own age.
	metadata, err := i.collectInventoryMetadata(ctx)
	if err != nil {
		return metadata, err
	}
	if i.cache == nil {
		metadata.Resources = []controllers.InstructorGitResourceInventory{}
		metadata.ScanError = "background inventory cache is unavailable"
		metadata.CapturedAt = time.Time{}
		return metadata, nil
	}
	captured := i.cache.read(ctx)
	metadata.CapturedAt = captured.CapturedAt
	metadata.Resources = captured.Resources
	metadata.UnassignedResourceCount = captured.UnassignedResourceCount
	metadata.Enabled, metadata.Cached, metadata.ScanError, metadata.NextScanAt = captured.Enabled, captured.Cached, captured.ScanError, captured.NextScanAt
	return metadata, nil
}

func (i instructorInventory) ReconcileInventory(ctx context.Context, id string, lastSeen time.Time) error {
	if i.cache == nil {
		return evidenceInvalid("inventory cache is unavailable")
	}
	return i.cache.reconcile(ctx, id, lastSeen)
}

func (c *instructorInventoryCache) ensureLoaded(ctx context.Context) error {
	c.loadMu.Lock()
	defer c.loadMu.Unlock()
	c.mu.RLock()
	loaded := c.loaded
	c.mu.RUnlock()
	if loaded {
		return nil
	}
	var captured controllers.InstructorInventoryResponse
	if c.persist != nil {
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		body, found, err := c.persist.GetInstructorInventoryCache(readCtx)
		cancel()
		if err != nil {
			return err
		}
		if found {
			if len(body) > 8<<20 || json.Unmarshal(body, &captured) != nil {
				return errors.New("persisted inventory cache is invalid")
			}
		}
	}
	c.mu.Lock()
	c.snapshot = captured
	c.loaded = true
	c.mu.Unlock()
	return nil
}

func (c *instructorInventoryCache) read(ctx context.Context) controllers.InstructorInventoryResponse {
	loadErr := c.ensureLoaded(ctx)
	c.mu.RLock()
	result := cloneInstructorInventory(c.snapshot)
	result.Enabled = c.enabled
	c.mu.RUnlock()
	result.Cached = !result.CapturedAt.IsZero()
	if !result.Enabled {
		result.NextScanAt = nil
	}
	if loadErr != nil {
		result.ScanError = "persisted inventory read failed: " + loadErr.Error()
	}
	if result.Resources == nil {
		result.Resources = []controllers.InstructorGitResourceInventory{}
	}
	return result
}

func cloneInstructorInventory(snapshot controllers.InstructorInventoryResponse) controllers.InstructorInventoryResponse {
	data, _ := json.Marshal(snapshot)
	var result controllers.InstructorInventoryResponse
	_ = json.Unmarshal(data, &result)
	return result
}

func (c *instructorInventoryCache) scan(ctx context.Context) error {
	if err := c.ensureLoaded(ctx); err != nil {
		return err
	}
	if c.collect == nil {
		return errors.New("inventory collector is unavailable")
	}
	nextAt := c.now().Add(c.interval)
	// Collection is deliberately outside cache locks: concurrent status and
	// reconciliation requests continue reading the previous captured facts.
	scanCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	next, collectErr := c.collect(scanCtx)
	cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.RLock()
	previous := cloneInstructorInventory(c.snapshot)
	c.mu.RUnlock()
	if collectErr != nil {
		next = previous
		next.ScanError = "inventory scan failed: " + collectErr.Error()
	} else {
		if next.CapturedAt.IsZero() {
			next.CapturedAt = c.now()
		}
		next.Resources = mergeInstructorInventoryResources(previous.Resources, next.Resources, next.CapturedAt)
		next.UnassignedResourceCount = countInstructorUnassignedResources(next.Resources)
		next.Cached = true
	}
	next.NextScanAt = &nextAt
	if c.persist == nil {
		next.ScanError = "persistent inventory storage is unavailable; observations are ephemeral"
	} else if err := c.save(ctx, next); err != nil {
		next.ScanError = "inventory persistence failed: " + err.Error()
		c.mu.Lock()
		c.snapshot = next
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.snapshot = next
	c.mu.Unlock()
	return collectErr
}

func (c *instructorInventoryCache) save(ctx context.Context, snapshot controllers.InstructorInventoryResponse) error {
	body, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return c.persist.PutInstructorInventoryCache(writeCtx, body)
}

func mergeInstructorInventoryResources(previous, current []controllers.InstructorGitResourceInventory, at time.Time) []controllers.InstructorGitResourceInventory {
	old := map[string]controllers.InstructorGitResourceInventory{}
	for _, resource := range previous {
		old[resource.ID] = resource
	}
	result := make([]controllers.InstructorGitResourceInventory, 0, len(current)+len(previous))
	for _, resource := range current {
		prior, found := old[resource.ID]
		resource.FirstSeenAt = at
		if found && !prior.FirstSeenAt.IsZero() {
			resource.FirstSeenAt = prior.FirstSeenAt
		}
		resource.LastSeenAt = at
		resource.NotObservedSince = nil
		resource.ReconciledAt = nil
		result = append(result, resource)
		delete(old, resource.ID)
	}
	for _, resource := range old {
		if resource.ReconciledAt != nil {
			resource.Status = "reconciled"
		} else {
			resource.Status = "not-observed"
			if resource.NotObservedSince == nil {
				resource.NotObservedSince = &at
			}
			resource.Error = "not observed by the latest bounded scan; absence is not proof of deletion"
		}
		result = append(result, resource)
	}
	sort.Slice(result, func(a, b int) bool { return result[a].ID < result[b].ID })
	return result
}

func countInstructorUnassignedResources(resources []controllers.InstructorGitResourceInventory) int {
	count := 0
	for _, resource := range resources {
		if resource.Assignment == "unassigned" && resource.ReconciledAt == nil {
			count++
		}
	}
	return count
}

func (c *instructorInventoryCache) reconcile(ctx context.Context, id string, lastSeen time.Time) error {
	if err := c.ensureLoaded(ctx); err != nil {
		return err
	}
	if c.persist == nil {
		return evidenceInvalid("resource reconciliation requires durable inventory storage")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.RLock()
	next := cloneInstructorInventory(c.snapshot)
	c.mu.RUnlock()
	found := false
	for n := range next.Resources {
		resource := &next.Resources[n]
		if resource.ID != id {
			continue
		}
		found = true
		if resource.Status != "not-observed" || !resource.LastSeenAt.Equal(lastSeen) || lastSeen.IsZero() {
			return evidenceInvalid("resource changed or is still observed; refresh inventory before reconciliation")
		}
		at := c.now()
		resource.ReconciledAt = &at
		resource.Status = "reconciled"
	}
	if !found {
		return evidenceInvalid("inventory resource does not exist")
	}
	next.UnassignedResourceCount = countInstructorUnassignedResources(next.Resources)
	if err := c.save(ctx, next); err != nil {
		return err
	}
	c.mu.Lock()
	c.snapshot = next
	c.mu.Unlock()
	return nil
}

func startInstructorInventory(ctx context.Context, i instructorInventory, log *slog.Logger) <-chan struct{} {
	if i.cache == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return i.cache.start(ctx, true, log)
}

func (c *instructorInventoryCache) start(ctx context.Context, enabled bool, log *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	c.mu.Lock()
	c.enabled = enabled
	c.mu.Unlock()
	if !enabled {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		defer func() { c.mu.Lock(); c.enabled = false; c.mu.Unlock() }()
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			if err := c.scan(ctx); err != nil && ctx.Err() == nil && log != nil {
				log.Warn("Instructor inventory scan failed", "error", fmt.Sprint(err))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

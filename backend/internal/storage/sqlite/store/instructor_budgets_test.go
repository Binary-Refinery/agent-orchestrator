package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func budgetWork(t *testing.T, store *sqlite.Store, caps domain.InstructorBudgets) domain.InstructorWork {
	t.Helper()
	seedProject(t, store, "budget-project")
	policy := instructorsvc.DefaultPolicy()
	policy.Budgets = &caps
	policy.Version = instructorsvc.PolicyVersion(policy)
	work := domain.InstructorWork{ID: "budget-work", ProjectID: "budget-project", Source: "test", SourceRef: "one", State: "ready", Revision: 1, Policy: policy, PolicyVersion: policy.Version}
	created, err := store.CreateInstructorWork(context.Background(), work, domain.InstructorEvent{ID: "budget-intake", WorkID: work.ID, Kind: "registered"})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func budgetEntry(work domain.InstructorWork, kind, resource string, delegated bool) domain.InstructorBudgetEntry {
	return domain.InstructorBudgetEntry{ID: domain.InstructorBudgetReservationID(work.ID, kind, resource), WorkID: work.ID, PolicyVersion: work.PolicyVersion, Kind: kind, ResourceKey: resource, Delegated: delegated, State: "reserved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
}

func TestInstructorBudgetConcurrentAdmissionIsDurableAndAtomic(t *testing.T) {
	store := newTestStore(t)
	work := budgetWork(t, store, domain.InstructorBudgets{MaxConcurrentSessions: 2, MaxSessions: 32})
	var admitted atomic.Int32
	var refused atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 32; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			_, err := store.ReserveInstructorBudget(context.Background(), work, budgetEntry(work, "session", fmt.Sprintf("instance-%d", index), false))
			if err == nil {
				admitted.Add(1)
			} else if errors.Is(err, domain.ErrInstructorBudgetExceeded) {
				refused.Add(1)
			} else {
				t.Errorf("admission error: %v", err)
			}
		}(index)
	}
	group.Wait()
	if admitted.Load() != 2 || refused.Load() != 30 {
		t.Fatalf("admitted=%d refused=%d", admitted.Load(), refused.Load())
	}
	usage, err := store.InstructorBudgetUsage(context.Background(), work.ID)
	if err != nil || usage.ConcurrentSessions != 2 || usage.Sessions != 2 {
		t.Fatalf("usage=%+v error=%v", usage, err)
	}
}

func TestInstructorBudgetLifetimeAndUncertainTurnsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	work := budgetWork(t, store, domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 2, MaxTurns: 1})
	ctx := context.Background()
	first, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "immutable-first", false))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, first.ID, "started", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishInstructorSessionBudgets(ctx, first.ResourceKey, time.Now()); err != nil {
		t.Fatal(err)
	}
	second, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "immutable-replacement", false))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, second.ID, "started", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishInstructorSessionBudgets(ctx, second.ResourceKey, time.Now()); err != nil {
		t.Fatal(err)
	}
	turn, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "turn", "actual-turn-id", false))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, turn.ID, "uncertain", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, turn.ID, "cancelled", time.Now()); err == nil {
		t.Fatal("unknown native attempt replenished the turn budget")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "third-replacement", false)); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("lifetime admission=%v", err)
	}
	if _, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "turn", "different-turn", false)); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("turn admission=%v", err)
	}
	replayed, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "turn", "actual-turn-id", false))
	if err != nil || replayed.ID != turn.ID || replayed.State != "uncertain" {
		t.Fatalf("replayed=%+v error=%v", replayed, err)
	}
	// Restoring the same inserted session is capacity, not a third lifetime session.
	restored, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", first.ResourceKey, false))
	if err != nil || restored.ID != first.ID {
		t.Fatalf("restore=%+v error=%v", restored, err)
	}
	usage, err := store.InstructorBudgetUsage(ctx, work.ID)
	if err != nil || usage.ConcurrentSessions != 1 || usage.Sessions != 2 || usage.Turns != 1 {
		t.Fatalf("usage=%+v error=%v", usage, err)
	}
}

func TestInstructorBudgetDefinitePrelaunchCancellationReleasesReservation(t *testing.T) {
	store := newTestStore(t)
	work := budgetWork(t, store, domain.InstructorBudgets{MaxSessions: 1, MaxDelegations: 1})
	ctx := context.Background()
	first, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "not-launched", true))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, first.ID, "cancelled", time.Now()); err != nil {
		t.Fatal(err)
	}
	second, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "launched", true))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, second.ID, "started", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, second.ID, "cancelled", time.Now()); err == nil {
		t.Fatal("started session refunded lifetime budget")
	}
	usage, err := store.InstructorBudgetUsage(ctx, work.ID)
	if err != nil || usage.Sessions != 1 || usage.Delegations != 1 {
		t.Fatalf("usage=%+v error=%v", usage, err)
	}
}

func TestInstructorBudgetDelegationLimitIsSeparateFromDirectSessions(t *testing.T) {
	store := newTestStore(t)
	work := budgetWork(t, store, domain.InstructorBudgets{MaxSessions: 4, MaxDelegations: 1})
	ctx := context.Background()
	if _, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "manual", false)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "review-child", true)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "second-child", true)); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("delegation admission=%v", err)
	}
	if _, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "second-manual", false)); err != nil {
		t.Fatal(err)
	}
}

func TestInstructorBudgetRejectsStaleWorkBeforeReservation(t *testing.T) {
	store := newTestStore(t)
	work := budgetWork(t, store, domain.InstructorBudgets{MaxSessions: 1})
	stale := work
	stale.Revision++
	if _, err := store.ReserveInstructorBudget(context.Background(), stale, budgetEntry(stale, "session", "never-admitted", false)); err == nil {
		t.Fatal("stale work admitted native resource")
	}
	usage, err := store.InstructorBudgetUsage(context.Background(), work.ID)
	if err != nil || usage.Sessions != 0 {
		t.Fatalf("usage=%+v error=%v", usage, err)
	}
}

func TestInstructorBudgetTurnEffectFenceHasOneOwner(t *testing.T) {
	store := newTestStore(t)
	work := budgetWork(t, store, domain.InstructorBudgets{MaxTurns: 1})
	entry, err := store.ReserveInstructorBudget(context.Background(), work, budgetEntry(work, "turn", "one-native-turn", false))
	if err != nil {
		t.Fatal(err)
	}
	var claimed atomic.Int32
	var refused atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			err := store.SetInstructorBudgetState(context.Background(), entry.ID, "uncertain", time.Now())
			if err == nil {
				claimed.Add(1)
			} else if errors.Is(err, domain.ErrInstructorBudgetClaimed) {
				refused.Add(1)
			} else {
				t.Errorf("fence=%v", err)
			}
		}()
	}
	group.Wait()
	if claimed.Load() != 1 || refused.Load() != 15 {
		t.Fatalf("native fence claimed=%d refused=%d", claimed.Load(), refused.Load())
	}
	if err := store.CancelInstructorBudgetBeforeAdmission(context.Background(), entry.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveInstructorBudget(context.Background(), work, budgetEntry(work, "turn", "replacement-after-proven-suppression", false)); err != nil {
		t.Fatalf("definite non-admission consumed budget: %v", err)
	}
}

func TestInstructorBudgetIndependentStoresCannotOversubscribeLastSlot(t *testing.T) {
	dir := t.TempDir()
	first, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	work := budgetWork(t, first, domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 1})
	second, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	start := make(chan struct{})
	results := make(chan error, 2)
	for index, store := range []*sqlite.Store{first, second} {
		go func(index int, store *sqlite.Store) {
			<-start
			_, err := store.ReserveInstructorBudget(context.Background(), work, budgetEntry(work, "session", fmt.Sprintf("independent-%d", index), false))
			results <- err
		}(index, store)
	}
	close(start)
	admitted := 0
	for index := 0; index < 2; index++ {
		if <-results == nil {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("independent writers admitted=%d", admitted)
	}
	usage, err := first.InstructorBudgetUsage(context.Background(), work.ID)
	if err != nil || usage.Sessions != 1 || usage.ConcurrentSessions != 1 {
		t.Fatalf("usage=%+v error=%v", usage, err)
	}
}

func TestInstructorBudgetReplacementFenceHoldsCapacityAcrossSourceExit(t *testing.T) {
	store := newTestStore(t)
	work := budgetWork(t, store, domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 2})
	ctx := context.Background()
	first, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: work.ProjectID, Kind: domain.KindWorker, Harness: domain.HarnessCodex, Activity: domain.Activity{State: domain.ActivityActive}})
	if err != nil {
		t.Fatal(err)
	}
	instance, _, err := store.InstructorSessionInstance(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := budgetEntry(work, "session", "session:"+instance, false)
	request.SessionID = first.ID
	entry, err := store.ReserveInstructorBudget(ctx, work, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, entry.ID, "started", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, entry.ID, "uncertain", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstructorBudgetState(ctx, entry.ID, "uncertain", time.Now()); !errors.Is(err, domain.ErrInstructorBudgetClaimed) {
		t.Fatalf("replacement fence has multiple owners: %v", err)
	}
	first.Activity.State = domain.ActivityExited
	if err := store.UpdateSession(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: work.ProjectID, Kind: domain.KindWorker, Harness: domain.HarnessCodex})
	if err != nil {
		t.Fatal(err)
	}
	secondInstance, _, err := store.InstructorSessionInstance(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	other := budgetEntry(work, "session", "session:"+secondInstance, false)
	other.SessionID = second.ID
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := store.ReserveInstructorBudget(ctx, work, other); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
				t.Errorf("source exit released fenced replacement capacity: %v", err)
			}
		}()
	}
	group.Wait()
	if err := store.CancelInstructorBudgetBeforeAdmission(ctx, entry.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveInstructorBudget(ctx, work, other); err != nil {
		t.Fatalf("definitely unstarted replacement did not restore prior exit state: %v", err)
	}
	usage, err := store.InstructorBudgetUsage(ctx, work.ID)
	if err != nil || usage.ConcurrentSessions != 1 || usage.Sessions != 2 {
		t.Fatalf("replacement refund erased lifetime: %+v error=%v", usage, err)
	}
}

func TestInstructorBudgetUnusedRestoreKeepsPreviouslyConsumedLifetime(t *testing.T) {
	for _, fenced := range []bool{false, true} {
		t.Run(fmt.Sprintf("fenced_%t", fenced), func(t *testing.T) {
			store := newTestStore(t)
			work := budgetWork(t, store, domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 1})
			ctx := context.Background()
			request := budgetEntry(work, "session", "immutable-existing-session", false)
			entry, err := store.ReserveInstructorBudget(ctx, work, request)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SetInstructorBudgetState(ctx, entry.ID, "started", time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := store.FinishInstructorSessionBudgets(ctx, entry.ResourceKey, time.Now()); err != nil {
				t.Fatal(err)
			}
			restored, err := store.ReserveInstructorBudget(ctx, work, request)
			if err != nil {
				t.Fatal(err)
			}
			if fenced {
				if err := store.SetInstructorBudgetState(ctx, restored.ID, "uncertain", time.Now()); err != nil {
					t.Fatal(err)
				}
				err = store.CancelInstructorBudgetBeforeAdmission(ctx, restored.ID, time.Now())
			} else {
				err = store.SetInstructorBudgetState(ctx, restored.ID, "cancelled", time.Now())
			}
			if err != nil {
				t.Fatal(err)
			}
			usage, err := store.InstructorBudgetUsage(ctx, work.ID)
			if err != nil || usage.Sessions != 1 || usage.ConcurrentSessions != 0 {
				t.Fatalf("unused restore refunded original session: %+v error=%v", usage, err)
			}
			if _, err := store.ReserveInstructorBudget(ctx, work, budgetEntry(work, "session", "new-lifetime-session", false)); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
				t.Fatalf("unused restore bypassed lifetime cap: %v", err)
			}
		})
	}
}

package instructor_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorBudgetManualAdoptionIsAtomicWithWorkBinding(t *testing.T) {
	f := newInstructorFixture(t)
	policy := instructorsvc.DefaultPolicy()
	policy.Budgets = &domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 1}
	work := f.register(t, "atomic-budget-binding", policy)
	first := f.attachWorker(t, work.ID)
	second, err := f.store.CreateSession(context.Background(), domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.service.Details(context.Background(), work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.AttachSession(context.Background(), work.ID, second.ID); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("adoption quota=%v", err)
	}
	if _, bound, err := f.service.WorkForSession(context.Background(), second.ID); err != nil || bound {
		t.Fatalf("refused session was bound: %v error=%v", bound, err)
	}
	after, err := f.service.Details(context.Background(), work.ID)
	if err != nil || after.Work.Revision != before.Work.Revision || len(after.Work.SessionIDs) != 1 || after.Work.SessionIDs[0] != first {
		t.Fatalf("failed admission changed work=%+v error=%v", after.Work, err)
	}
	if after.BudgetUsage == nil || after.BudgetUsage.Sessions != 1 || after.BudgetUsage.ConcurrentSessions != 1 {
		t.Fatalf("usage=%+v", after.BudgetUsage)
	}
}

func TestInstructorBudgetExceptionExtendsPinnedLimitsWithoutResettingHistory(t *testing.T) {
	f := newInstructorFixture(t)
	policy := instructorsvc.DefaultPolicy()
	policy.Budgets = &domain.InstructorBudgets{MaxTurns: 1}
	work := f.register(t, "execution-budget-exception", policy)
	entry, err := f.service.ReserveBudget(context.Background(), work.ID, "turn", "first-native-turn", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.SetBudgetState(context.Background(), entry.ID, "started"); err != nil {
		t.Fatal(err)
	}
	caps := domain.InstructorBudgets{MaxTurns: 2}
	input := instructorsvc.ExceptionInput{SnapshotID: f.snapshot(t).ID, WorkID: work.ID, ExecutionBudgets: &caps, Reason: "One further operator-approved turn", ExpiresAt: f.now.Add(time.Hour)}
	preview, err := f.service.PreviewException(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ApplyException(context.Background(), input); err == nil {
		t.Fatal("unsigned budget exception accepted")
	}
	input.Approval = preview.ExactApproval
	granted, err := f.service.ApplyException(context.Background(), input)
	if err != nil || granted.Policy.Budgets.MaxTurns != 2 || len(granted.Exceptions) != 1 || granted.Exceptions[0].Kind != "execution-budget" {
		t.Fatalf("grant=%+v error=%v", granted, err)
	}
	if _, err := f.service.ReserveBudget(context.Background(), work.ID, "turn", "second-native-turn", "", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ReserveBudget(context.Background(), work.ID, "turn", "third-native-turn", "", "", false); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("exception reset durable consumption: %v", err)
	}
	global, err := f.service.GetPolicy(context.Background())
	if err != nil || global.Budgets.MaxTurns != 256 {
		t.Fatalf("exception modified global limits: %+v error=%v", global.Budgets, err)
	}
	f.now = f.now.Add(2 * time.Hour)
	if _, err := f.service.ReserveBudget(context.Background(), work.ID, "turn", "expired-exception-turn", "", "", false); err == nil {
		t.Fatal("expired exception admitted native turn")
	}
}

func TestInstructorBudgetsValidateNativeLimitsAndPreserveLegacyPolicyEncoding(t *testing.T) {
	policy := instructorsvc.DefaultPolicy()
	policy.Budgets = nil
	policy.Version = instructorsvc.PolicyVersion(policy)
	body, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"budgets"`) {
		t.Fatal("new optional budget field rewrote old pinned policy encoding")
	}
	parsed, err := instructorsvc.ParsePolicy(body)
	if err != nil || parsed.Budgets != nil || parsed.Version != policy.Version {
		t.Fatalf("legacy policy=%+v error=%v", parsed, err)
	}
	for _, caps := range []domain.InstructorBudgets{{MaxTurns: -1}, {MaxSessions: 1, MaxConcurrentSessions: 2}, {MaxDelegations: 1000001}} {
		policy.Budgets = &caps
		if err := instructorsvc.ValidatePolicy(policy); err == nil {
			t.Fatalf("invalid caps accepted: %+v", caps)
		}
	}
	policy.Budgets = &domain.InstructorBudgets{}
	if err := instructorsvc.ValidatePolicy(policy); err != nil {
		t.Fatalf("explicit unlimited caps refused: %v", err)
	}
}

func TestInstructorBudgetAdoptedSessionExitReleasesCapacityWithoutErasingLifetime(t *testing.T) {
	f := newInstructorFixture(t)
	policy := instructorsvc.DefaultPolicy()
	policy.Budgets = &domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 3}
	work := f.register(t, "adoption-exit", policy)
	ctx := context.Background()
	first := f.attachWorker(t, work.ID)
	rec, _, err := f.store.GetSession(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	rec.Activity.State = domain.ActivityExited
	if err := f.store.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	second, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.AttachSession(ctx, work.ID, second.ID); err != nil {
		t.Fatalf("capacity stayed reserved after adopted session exited: %v", err)
	}
	usage, err := f.service.BudgetUsage(ctx, work.ID)
	if err != nil || usage.ConcurrentSessions != 1 || usage.Sessions != 2 {
		t.Fatalf("usage=%+v error=%v", usage, err)
	}
	instance, _, err := f.store.InstructorSessionInstance(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	retired := domain.InstructorBudgetReservationID(work.ID, "session", "session:"+instance)
	if err := f.service.SetBudgetState(ctx, retired, "uncertain"); !errors.Is(err, domain.ErrInstructorBudgetClaimed) {
		t.Fatalf("retired slot allowed native effect after replacement: %v", err)
	}
}

func TestInstructorBudgetOldExitCannotReleasePendingRestoreOrUncertainAttempt(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprintf("uncertain_%t", uncertain), func(t *testing.T) {
			f := newInstructorFixture(t)
			policy := instructorsvc.DefaultPolicy()
			policy.Budgets = &domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 3}
			work := f.register(t, "pending-restore", policy)
			ctx := context.Background()
			first := f.attachWorker(t, work.ID)
			instance, _, err := f.store.InstructorSessionInstance(ctx, first)
			if err != nil {
				t.Fatal(err)
			}
			token := domain.InstructorBudgetReservationID(work.ID, "session", "session:"+instance)
			if !uncertain {
				if err := f.store.FinishInstructorSessionBudgets(ctx, "session:"+instance, f.now); err != nil {
					t.Fatal(err)
				}
			} else if err := f.service.SetBudgetState(ctx, token, "uncertain"); err != nil {
				t.Fatal(err)
			}
			rec, _, err := f.store.GetSession(ctx, first)
			if err != nil {
				t.Fatal(err)
			}
			rec.Activity.State = domain.ActivityExited
			if err := f.store.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			if !uncertain {
				if _, err := f.service.ReserveBudget(ctx, work.ID, "session", "session:"+instance, first, "", false); err != nil {
					t.Fatal(err)
				}
			}
			second, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, CreatedAt: f.now, UpdatedAt: f.now})
			if err != nil {
				t.Fatal(err)
			}
			if err := f.service.AttachSession(ctx, work.ID, second.ID); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
				t.Fatalf("old exit/uncertainty released pending capacity: %v", err)
			}
		})
	}
}

func TestInstructorBudgetQuotaRefusalPreservesAdoptedSourceButReleasesUnusedSeed(t *testing.T) {
	for _, prelaunch := range []bool{false, true} {
		t.Run(fmt.Sprintf("prelaunch_%t", prelaunch), func(t *testing.T) {
			f := newInstructorFixture(t)
			ctx := context.Background()
			policy := instructorsvc.DefaultPolicy()
			policy.Budgets = &domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 1, MaxTurns: 1}
			work := f.register(t, "quota-refused-replacement", policy)
			first, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, Activity: domain.Activity{State: domain.ActivityIdle}, CreatedAt: f.now, UpdatedAt: f.now})
			if err != nil {
				t.Fatal(err)
			}
			if prelaunch {
				err = f.service.AttachLaunchingSession(ctx, work.ID, first.ID, "")
			} else {
				err = f.service.AttachSession(ctx, work.ID, first.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			instance, _, err := f.store.InstructorSessionInstance(ctx, first.ID)
			if err != nil {
				t.Fatal(err)
			}
			source, err := f.service.ReserveBudget(ctx, work.ID, "session", "session:"+instance, first.ID, "", false)
			if err != nil {
				t.Fatal(err)
			}
			spent, err := f.service.ReserveBudget(ctx, work.ID, "turn", "spent", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.service.SetBudgetState(ctx, spent.ID, "started"); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.ReserveBudget(ctx, work.ID, "turn", "session:"+instance+"/turn:switch:new", first.ID, "", false); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
				t.Fatalf("turn admission=%v", err)
			}
			if err := f.service.SetBudgetState(ctx, source.ID, "cancelled"); err != nil {
				t.Fatal(err)
			}
			if !prelaunch {
				// Repeated refusals and a proven-unstarted replacement must all preserve
				// the original adopted slot, including a source currently idle.
				if err := f.service.SetBudgetState(ctx, source.ID, "cancelled"); err != nil {
					t.Fatal(err)
				}
				if err := f.service.SetBudgetState(ctx, source.ID, "uncertain"); err != nil {
					t.Fatal(err)
				}
				if err := f.store.CancelInstructorBudgetBeforeAdmission(ctx, source.ID, f.now); err != nil {
					t.Fatal(err)
				}
			}
			usage, err := f.service.BudgetUsage(ctx, work.ID)
			expected := 1
			if prelaunch {
				expected = 0
			}
			if err != nil || usage.Sessions != expected || usage.ConcurrentSessions != expected || usage.Turns != 1 {
				t.Fatalf("usage=%+v error=%v expected sessions=%d", usage, err, expected)
			}
			second, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, CreatedAt: f.now, UpdatedAt: f.now})
			if err != nil {
				t.Fatal(err)
			}
			err = f.service.AttachSession(ctx, work.ID, second.ID)
			if prelaunch && err != nil || !prelaunch && !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
				t.Fatalf("source lifetime/capacity admission=%v prelaunch=%t", err, prelaunch)
			}
		})
	}
}

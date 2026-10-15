package instructor_test

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

const checkDeadlineMarker = "refs/ao-test/check-deadline-executed"

func deadlineCheckFixture(t *testing.T, seconds int) (*instructorFixture, domain.InstructorWork, domain.SessionID, string) {
	t.Helper()
	f := newInstructorFixture(t)
	path, _ := committedWorkspace(t)
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "update-ref", checkDeadlineMarker, "HEAD"}, TimeoutSeconds: seconds}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "--verify", "HEAD"}, TimeoutSeconds: seconds}
	work := f.register(t, "deadline-checks", policy)
	worker := f.attachWorker(t, work.ID)
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	return f, work, worker, path
}

func assertDeadlineMarker(t *testing.T, path string, wantExecuted bool) {
	t.Helper()
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", checkDeadlineMarker)
	cmd.Dir = path
	err := cmd.Run()
	if wantExecuted && err != nil || !wantExecuted && err == nil {
		t.Fatalf("native marker execution = %v, want %v (err=%v)", err == nil, wantExecuted, err)
	}
}

func TestInstructorCheckDeadlineRejectsBeforeExecutionOrDurableMutation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		deadline time.Duration
		profiles []string
	}{
		{name: "actual request below standard REST timeout", deadline: 2 * time.Second, profiles: []string{"push"}},
		{name: "cleanup margin remains reserved", deadline: 12 * time.Second, profiles: []string{"push"}},
		{name: "combined profile budget", deadline: 20 * time.Second, profiles: []string{"push", "full"}},
		{name: "default profile budget", deadline: 20 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, work, worker, path := deadlineCheckFixture(t, 10)
			before, found, err := f.store.GetInstructorWork(context.Background(), work.ID)
			if err != nil || !found {
				t.Fatalf("read before: found=%v err=%v", found, err)
			}
			eventsBefore, err := f.store.ListInstructorEvents(context.Background(), work.ID)
			if err != nil {
				t.Fatalf("read events before: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), tc.deadline)
			defer cancel()
			if _, err := f.service.RunChecks(ctx, work.ID, worker, tc.profiles); !errors.Is(err, instructorsvc.ErrChecksDeadline) {
				t.Fatalf("deadline rejection = %v, want preexecution deadline classification", err)
			}
			assertDeadlineMarker(t, path, false)
			after, _, err := f.store.GetInstructorWork(context.Background(), work.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("preexecution refusal changed work/action/evidence: before=%+v after=%+v err=%v", before, after, err)
			}
			eventsAfter, err := f.store.ListInstructorEvents(context.Background(), work.ID)
			if err != nil || !reflect.DeepEqual(eventsBefore, eventsAfter) {
				t.Fatalf("preexecution refusal added audit events: before=%+v after=%+v err=%v", eventsBefore, eventsAfter, err)
			}
		})
	}
}

type budgetConsumingWorkspace struct {
	path  string
	calls int
}

func (w *budgetConsumingWorkspace) WorkspaceForSession(ctx context.Context, _ domain.SessionID) (string, error) {
	w.calls++
	// Spend enough of a real request deadline that an initial admission check
	// succeeds, but a second check must refuse before any durable mutation.
	timer := time.NewTimer(2250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return w.path, nil
	}
}

func TestInstructorCheckDeadlineRechecksAfterReadOnlyPreflight(t *testing.T) {
	f, work, worker, path := deadlineCheckFixture(t, 1)
	// A different captured SHA would normally write worker_commit_observed.
	work = f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.SHA = immutableSHA
		w.Phase = "implement"
	})
	workspace := &budgetConsumingWorkspace{path: path}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: workspace, Clock: func() time.Time { return f.now }})
	eventsBefore, err := f.store.ListInstructorEvents(context.Background(), work.ID)
	if err != nil {
		t.Fatalf("events before: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := f.service.RunChecks(ctx, work.ID, worker, []string{"push"}); !errors.Is(err, instructorsvc.ErrChecksDeadline) {
		t.Fatalf("consumed preflight budget = %v, want preexecution deadline classification", err)
	}
	if workspace.calls != 1 {
		t.Fatalf("workspace preflight calls=%d, want one before final budget check", workspace.calls)
	}
	assertDeadlineMarker(t, path, false)
	after, _, err := f.store.GetInstructorWork(context.Background(), work.ID)
	if err != nil || !reflect.DeepEqual(work, after) {
		t.Fatalf("late preexecution refusal changed work: before=%+v after=%+v err=%v", work, after, err)
	}
	eventsAfter, err := f.store.ListInstructorEvents(context.Background(), work.ID)
	if err != nil || !reflect.DeepEqual(eventsBefore, eventsAfter) {
		t.Fatalf("late preexecution refusal added events: before=%+v after=%+v err=%v", eventsBefore, eventsAfter, err)
	}
}

func TestInstructorCheckDeadlinePreservesBackgroundAndAdequateRequests(t *testing.T) {
	for _, deadline := range []time.Duration{0, 30 * time.Second} {
		t.Run(deadline.String(), func(t *testing.T) {
			f, work, worker, path := deadlineCheckFixture(t, 10)
			ctx := context.Background()
			if deadline > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, deadline)
				defer cancel()
			}
			checked, err := f.service.RunChecks(ctx, work.ID, worker, []string{"push"})
			if err != nil || len(checked.Evidence) != 1 || len(checked.PhaseActions) != 1 || checked.PhaseActions[0].State != "completed" {
				t.Fatalf("adequate/no deadline rejected actual approved checks: work=%+v err=%v", checked, err)
			}
			assertDeadlineMarker(t, path, true)
		})
	}
}

func TestInstructorCheckPreflightValidatesEntireBatchBeforeExecution(t *testing.T) {
	f, work, worker, path := deadlineCheckFixture(t, 10)
	before, _, err := f.store.GetInstructorWork(context.Background(), work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RunChecks(context.Background(), work.ID, worker, []string{"push", "not-approved"}); !errors.Is(err, instructorsvc.ErrValidation) {
		t.Fatalf("unapproved later profile returned %v", err)
	}
	assertDeadlineMarker(t, path, false)
	after, _, err := f.store.GetInstructorWork(context.Background(), work.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("invalid batch executed or changed durable state: before=%+v after=%+v err=%v", before, after, err)
	}
}

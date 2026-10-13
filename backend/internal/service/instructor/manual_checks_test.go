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

type failedManualAuditStore struct {
	*sqlite.Store
	failure error
}

func (s failedManualAuditStore) SaveInstructorPhaseActionAudit(context.Context, domain.InstructorWork, int64, domain.InstructorEvent) (bool, error) {
	return false, s.failure
}

func TestInstructorManualCheckCleanupFailureIsNotSuccessfulResponse(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, _ := committedWorkspace(t)
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "--verify", "HEAD"}, TimeoutSeconds: 10}
	work := f.register(t, "cleanup-failure", policy)
	worker := f.attachWorker(t, work.ID)
	failure := errors.New("audit storage unavailable")
	f.service = instructorsvc.New(instructorsvc.Deps{Store: failedManualAuditStore{Store: f.store, failure: failure}, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	_, err := f.service.RunChecks(ctx, work.ID, worker, []string{"push"})
	if !errors.Is(err, failure) {
		t.Fatalf("cleanup failure was swallowed: %v", err)
	}
	stored, _, err := f.store.GetInstructorWork(ctx, work.ID)
	if err != nil || len(stored.PhaseActions) != 1 || stored.PhaseActions[0].State != "running" {
		t.Fatalf("failed cleanup lost durable recovery intent: %+v err=%v", stored, err)
	}
}

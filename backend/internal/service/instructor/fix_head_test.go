package instructor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorAuthorizedFixChecksObserveNewCleanHeadWithoutAgentSHAClaim(t *testing.T) {
	ctx := context.Background()
	f := newInstructorFixture(t)
	path, oldSHA := committedWorkspace(t)
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "--verify", "HEAD"}, TimeoutSeconds: 10}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "diff", "--exit-code", "HEAD"}, TimeoutSeconds: 10}
	w := f.register(t, "fix-head", policy)
	worker := f.attachWorker(t, w.ID)
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State = "running"
		w.Phase = "fix"
		w.PhaseSequence = 4
		w.FixPhaseSequence = 4
		w.FixRounds = 1
		w.SHA = oldSHA
	})
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("Fixed implementation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = path
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, b, err)
		}
		return strings.TrimSpace(string(b))
	}
	run("add", "README.md")
	run("-c", "user.name=Worker", "-c", "user.email=worker@example.invalid", "commit", "--quiet", "-m", "Fix")
	newSHA := run("rev-parse", "HEAD")
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	checked, err := f.service.RunChecks(ctx, w.ID, worker, nil)
	if err != nil || checked.SHA != newSHA || checked.FixRounds != 1 || len(checked.Evidence) != 2 {
		t.Fatalf("new authorized fix HEAD not adopted safely: %+v %v", checked, err)
	}
	for _, e := range checked.Evidence {
		if e.SHA != newSHA || e.PhaseSequence != 4 || !e.Verified {
			t.Fatalf("foreign/stale proof=%+v", e)
		}
	}
}

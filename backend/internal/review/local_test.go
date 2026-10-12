package review

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestManagedLocalReviewUsesCanonicalEngineBeforePRAndScopesDelta(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteReviewStore(t)
	worker := liveWorker()
	seedReviewWorker(t, store, worker)
	launcher := &fakeLauncher{handle: "local-review", agentSessionID: "native-local-review"}
	engine := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, fakePRs{}, fakeProjects{}, launcher)
	governance := &scopedReviewGovernance{scope: "work-1/review/2"}
	engine.SetInstructorGovernance(governance)
	sha := strings.Repeat("a", 40)
	first, err := engine.TriggerManagedLocal(ctx, worker.ID, domain.ReviewerCodex, domain.AgentConfig{}, sha)
	if err != nil || !first.Created || first.Run.PRURL != "" || first.Run.TargetSHA != sha || first.Run.ScopeKey != governance.scope {
		t.Fatalf("local run=%+v err=%v", first, err)
	}
	if _, err := store.UpdateReviewRunResult(ctx, first.Run.ID, domain.ReviewRunComplete, domain.VerdictApproved, "initial report", "", false); err != nil {
		t.Fatal(err)
	}
	repeated, err := engine.TriggerManagedLocal(ctx, worker.ID, domain.ReviewerCodex, domain.AgentConfig{}, sha)
	if err != nil || repeated.Created || repeated.Run.ID != first.Run.ID {
		t.Fatalf("duplicate local pass=%+v err=%v", repeated, err)
	}
	governance.scope = "work-1/delta/5"
	delta, err := engine.TriggerManagedLocal(ctx, worker.ID, domain.ReviewerCodex, domain.AgentConfig{}, sha)
	if err != nil || !delta.Created || delta.Run.ID == first.Run.ID || delta.Run.ScopeKey != governance.scope || launcher.spawnCount != 2 {
		t.Fatalf("distinct independent delta=%+v launcher=%+v err=%v", delta, launcher, err)
	}
	prompt, system := reviewTexts(launcher.gotSpec)
	if !strings.Contains(prompt, "Review only the fixes") || !strings.Contains(prompt, "--body -") || strings.Contains(prompt, "gh api") || !strings.Contains(system, "Do not execute") {
		t.Fatalf("unsafe/wrong local delta prompt: %s\n%s", prompt, system)
	}
	governance.scope = ""
	if _, err := engine.TriggerManagedLocal(ctx, worker.ID, domain.ReviewerCodex, domain.AgentConfig{}, sha); err == nil {
		t.Fatal("unmanaged local start bypassed work authority")
	}
}

package review

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type scopedReviewGovernance struct{ scope string }

func (*scopedReviewGovernance) ValidateReviewerLaunch(context.Context, LaunchSpec) error { return nil }
func (*scopedReviewGovernance) RecordReviewerLaunch(context.Context, LaunchSpec) error   { return nil }
func (g *scopedReviewGovernance) ReviewerRunScope(context.Context, domain.SessionID) (string, error) {
	return g.scope, nil
}

func TestGovernedDeltaCreatesFreshSameSHAReviewWithoutChangingApprovedHistory(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteReviewStore(t)
	worker := liveWorker()
	seedReviewWorker(t, store, worker)
	launcher := &fakeLauncher{handle: "review-mer-1", agentSessionID: "native-review"}
	engine := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)
	governance := &scopedReviewGovernance{}
	engine.SetInstructorGovernance(governance)

	original, err := engine.Trigger(ctx, worker.ID, "", domain.AgentConfig{})
	if err != nil || !original.Created {
		t.Fatalf("original review: %+v, %v", original, err)
	}
	if _, err := store.UpdateReviewRunResult(ctx, original.Run.ID, domain.ReviewRunComplete, domain.VerdictApproved, "original approval", "github-original", true); err != nil {
		t.Fatal(err)
	}

	governance.scope = "work-1/delta/1"
	delta, err := engine.Trigger(ctx, worker.ID, "", domain.AgentConfig{})
	if err != nil || !delta.Created {
		t.Fatalf("delta must create a separate same-SHA pass: %+v, %v", delta, err)
	}
	if delta.Run.ID == original.Run.ID || delta.Run.ScopeKey != governance.scope || delta.Run.TargetSHA != original.Run.TargetSHA {
		t.Fatalf("delta reused original approval: original=%+v delta=%+v", original.Run, delta.Run)
	}
	if launcher.spawnCount != 2 || launcher.notified || launcher.gotSpec.AgentSessionID != "" || launcher.gotSpec.ScopeKey != governance.scope {
		t.Fatalf("phase change must start a fresh native conversation: %+v", launcher)
	}
	row, found, err := store.GetReviewBySessionAndHarness(ctx, worker.ID, delta.Run.Harness)
	if err != nil || !found || row.LaunchScopeKey != governance.scope {
		t.Fatalf("durable actual launch scope: %+v, found=%t, %v", row, found, err)
	}

	if _, err := store.UpdateReviewRunResult(ctx, delta.Run.ID, domain.ReviewRunComplete, domain.VerdictApproved, "delta approval", "github-delta", true); err != nil {
		t.Fatal(err)
	}
	repeated, err := engine.Trigger(ctx, worker.ID, "", domain.AgentConfig{})
	if err != nil || repeated.Created || repeated.Run.ID != delta.Run.ID || launcher.spawnCount != 2 {
		t.Fatalf("same-phase approval must remain idempotent: %+v, %v", repeated, err)
	}
	governance.scope = "work-1/delta/2"
	nextDelta, err := engine.Trigger(ctx, worker.ID, "", domain.AgentConfig{})
	if err != nil || !nextDelta.Created || nextDelta.Run.ID == delta.Run.ID || nextDelta.Run.ScopeKey != governance.scope {
		t.Fatalf("new delta phase must get a new pass: %+v, %v", nextDelta, err)
	}
	runs, err := store.ListReviewRunsBySession(ctx, worker.ID)
	if err != nil || len(runs) != 3 {
		t.Fatalf("expected all three durable passes: %+v, %v", runs, err)
	}
	old, found, err := store.GetReviewRunByScope(ctx, worker.ID, original.Run.PRURL, "sha1", original.Run.Harness, "")
	if err != nil || !found || old.ID != original.Run.ID || old.Body != "original approval" || old.Verdict != domain.VerdictApproved || old.GithubReviewID != "github-original" {
		t.Fatalf("original history was changed: %+v, found=%t, %v", old, found, err)
	}
}

func TestRestoreCannotRelabelEarlierPhaseReviewerAsCurrentDelta(t *testing.T) {
	for _, mode := range []domain.ReviewerInterfaceMode{domain.ReviewerInterfaceTUI, domain.ReviewerInterfaceChat} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			worker := liveWorker()
			worker.ReviewerHarness = domain.ReviewerCodex
			review := domain.Review{ID: "review-1", SessionID: worker.ID, ProjectID: worker.ProjectID, Harness: domain.ReviewerCodex, InterfaceMode: mode, ReviewerHandleID: "old-live-reviewer", AgentSessionID: "old-native", LaunchScopeKey: ""}
			store := &fakeStore{review: &review}
			launcher := &fakeLauncher{alive: true, handle: "old-live-reviewer"}
			engine := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)
			engine.SetInstructorGovernance(&scopedReviewGovernance{scope: "work-1/delta/1"})
			var err error
			if mode == domain.ReviewerInterfaceChat {
				_, err = engine.restorePersistedChatReviewerLocked(ctx, worker, review, nil)
			} else {
				_, err = engine.RestoreReviewer(ctx, worker.ID)
			}
			if err == nil || !strings.Contains(err.Error(), "earlier governance phase") || launcher.restored || launcher.aliveChecked || store.review.LaunchScopeKey != "" {
				t.Fatalf("stale restore must fail before reuse or launch: err=%v launcher=%+v review=%+v", err, launcher, store.review)
			}
		})
	}
}

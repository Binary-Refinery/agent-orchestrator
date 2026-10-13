package daemon

import (
	"context"
	"fmt"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorReboundSelectionCanChangeAfterHumanGateWithoutResumingOldChoice(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	old := domain.InstructorAgentSelection{Harness: string(f.worker.Harness), Model: f.worker.Metadata.Model, Effort: f.worker.Metadata.Effort}
	if err := f.service.RecordLaunch(ctx, f.work.ID, domain.InstructorLaunch{SessionID: f.worker.ID, Role: "worker", Requested: old, Sent: &old, Status: "launched"}); err != nil {
		t.Fatal(err)
	}
	w, _, err := f.store.GetInstructorWork(ctx, f.work.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the already-approved replacement; the rebind service tests exercise
	// its exact preview/approval and native-quiescence transaction separately.
	expected := w.Revision
	role := w.Policy.Roles["worker"]
	role.Model, role.EffortByModel = "provider/approved-next", map[string]string{"provider/approved-next": "high"}
	w.Policy.Roles["worker"] = role
	w.Policy.Version = instructorsvc.PolicyVersion(w.Policy)
	w.PolicyVersion = w.Policy.Version
	w.State = "waiting_human"
	w.Gate = &domain.InstructorGate{ID: "new-policy-decision", Kind: "policy_rebound", ExactApproval: "RESUME-GO " + w.ID + " " + w.PolicyVersion}
	w.Revision++
	if saved, err := f.store.SaveInstructorWork(ctx, w, expected, domain.InstructorEvent{ID: "rebound-policy", WorkID: w.ID, Kind: "policy_rebound"}); err != nil || !saved {
		t.Fatalf("seed replacement: %v %v", saved, err)
	}
	choice := ports.AgentConfig{Model: role.Model, Effort: "high"}
	if err := f.guard.ValidateAgentChange(ctx, f.worker.ID, domain.HarnessOpenCode, choice); err == nil {
		t.Fatal("candidate selection bypassed human execution gate")
	}
	snapshot, err := f.service.Snapshot(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Go", SnapshotID: snapshot.ID, WorkIDs: []string{w.ID}, Approval: w.Gate.ExactApproval, IdempotencyKey: "replacement-execution-approved"}); err != nil {
		t.Fatal(err)
	}
	if err := f.guard.ValidateResume(ctx, f.worker.ID); err == nil {
		t.Fatal("historical launch resumed obsolete native choice")
	}
	if err := f.guard.ValidateControllerRecovery(ctx, f.worker.ID); err == nil {
		t.Fatal("controller recovery reused obsolete native choice")
	}
	if err := f.guard.ValidateAgentChange(ctx, f.worker.ID, domain.HarnessOpenCode, choice); err != nil {
		t.Fatalf("old native choice blocked explicit approved replacement: %v", err)
	}
	if err := f.guard.ValidateChatAgentConfig(ctx, f.worker.ID, choice.Model, choice.Effort); err != nil {
		t.Fatalf("chat settings could not select approved target: %v", err)
	}
	current, _, _ := f.store.GetSession(ctx, f.worker.ID)
	if current.Harness != f.worker.Harness || current.Metadata.Model != old.Model {
		t.Fatal("validation silently changed existing provider conversation")
	}
}

func TestInstructorReboundWorkerChoiceDoesNotBlockIndependentReviewOfRetainedSHA(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	w, _, err := f.store.GetInstructorWork(ctx, f.work.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := w.Revision
	role := w.Policy.Roles["worker"]
	role.Model, role.EffortByModel = "provider/next-worker", map[string]string{"provider/next-worker": "high"}
	w.Policy.Roles["worker"] = role
	w.Policy.Version = instructorsvc.PolicyVersion(w.Policy)
	w.PolicyVersion = w.Policy.Version
	w.Revision++
	if saved, err := f.store.SaveInstructorWork(ctx, w, expected, domain.InstructorEvent{ID: "new-worker-choice", WorkID: w.ID}); err != nil || !saved {
		t.Fatalf("persist current policy: %v %v", saved, err)
	}
	if err := f.guard.ValidateResume(ctx, f.worker.ID); err == nil {
		t.Fatal("obsolete worker choice resumed")
	}
	if err := f.service.ValidateDispatchAdmission(ctx, f.worker.ID, "ordinary-worker-turn"); err == nil {
		t.Fatal("obsolete worker choice submitted ordinary turn")
	}
	started, err := f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
	if err != nil || !started.Created || started.Run.TargetSHA != f.legacy.TargetSHA || started.Run.ScopeKey != instructorReviewScope(w) {
		t.Fatalf("independent reviewer could not inspect retained commit: %+v %v", started, err)
	}
	if err := f.guard.ValidateChatAgentConfig(ctx, domain.SessionID(started.Run.ReviewID), "provider/reviewer", "high"); err != nil {
		t.Fatalf("reviewer's actual configuration was blocked by worker's old model: %v", err)
	}
	if err := f.guard.ValidateChatAgentConfig(ctx, domain.SessionID(started.Run.ReviewID), "unapproved-reviewer", "high"); err == nil {
		t.Fatal("review authority separation bypassed reviewer selection")
	}
	if _, err := f.guard.ReserveManagedTurn(ctx, domain.ReviewConversationOwner(started.Run.ReviewID), "independent-review-turn", ""); err != nil {
		t.Fatalf("review budget incorrectly validated worker native choice: %v", err)
	}
	// Reviewer-only authority must still honor every stop/terminal gate.
	for _, state := range []string{"parked", "waiting_human", "cancelled"} {
		current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
		expected := current.Revision
		current.State = state
		current.Revision++
		if saved, err := f.store.SaveInstructorWork(ctx, current, expected, domain.InstructorEvent{ID: "review-state-" + state, WorkID: w.ID}); err != nil || !saved {
			t.Fatalf("persist state: %v %v", saved, err)
		}
		if err := f.guard.ValidateChatAgentConfig(ctx, domain.SessionID(started.Run.ReviewID), "provider/reviewer", "high"); err == nil {
			t.Fatalf("review chat bypassed %s", state)
		}
		if _, err := f.guard.ReserveManagedTurn(ctx, domain.ReviewConversationOwner(started.Run.ReviewID), fmt.Sprintf("paused-review-%s", state), ""); err == nil {
			t.Fatalf("review budget bypassed %s", state)
		}
		if err := f.guard.ValidateReviewerLaunch(ctx, f.launcher.spawns[len(f.launcher.spawns)-1]); err == nil {
			t.Fatalf("review launch bypassed %s", state)
		}
	}
}

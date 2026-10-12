package review

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type refusedReviewerBudget struct{ sessionError, turnError error }

func (*refusedReviewerBudget) ValidateReviewerLaunch(context.Context, LaunchSpec) error { return nil }
func (*refusedReviewerBudget) RecordReviewerLaunch(context.Context, LaunchSpec) error   { return nil }
func (g *refusedReviewerBudget) ReserveManagedSession(context.Context, domain.ConversationOwner, domain.SessionID) (string, error) {
	return "session", g.sessionError
}
func (g *refusedReviewerBudget) ReserveManagedTurn(context.Context, domain.ConversationOwner, string, string) (string, error) {
	return "turn", g.turnError
}
func (*refusedReviewerBudget) SetManagedBudgetState(context.Context, string, string) error {
	return nil
}
func (*refusedReviewerBudget) FinishManagedSession(context.Context, domain.ConversationOwner) error {
	return nil
}
func (*refusedReviewerBudget) CancelManagedBudgetBeforeAdmission(context.Context, string) error {
	return nil
}

func TestInstructorBudgetReviewerLaunchRejectsBeforeReplacingLivePane(t *testing.T) {
	for _, limit := range []string{"sessions", "turns"} {
		t.Run(limit, func(t *testing.T) {
			runtime := &fakeRuntime{alive: true}
			launcher := newTestLauncher(t, &fakeReviewer{}, runtime)
			guard := &refusedReviewerBudget{}
			if limit == "sessions" {
				guard.sessionError = domain.ErrInstructorBudgetExceeded
			} else {
				guard.turnError = domain.ErrInstructorBudgetExceeded
			}
			launcher.(interface{ SetInstructorGovernance(ReviewerGovernance) }).SetInstructorGovernance(guard)
			if _, err := launcher.Spawn(context.Background(), launchSpec()); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
				t.Fatalf("review budget admission=%v", err)
			}
			if runtime.created || runtime.destroyed != "" {
				t.Fatalf("budget refusal touched native pane: created=%v destroyed=%s", runtime.created, runtime.destroyed)
			}
		})
	}
}

func TestInstructorBudgetReviewerNotifyRejectsBeforeNativeWrite(t *testing.T) {
	runtime := &fakeRuntime{alive: true}
	launcher := newTestLauncher(t, &fakeReviewer{}, runtime)
	launcher.(interface{ SetInstructorGovernance(ReviewerGovernance) }).SetInstructorGovernance(&refusedReviewerBudget{turnError: domain.ErrInstructorBudgetExceeded})
	if err := launcher.Notify(context.Background(), "review-mer-1", launchSpec()); !errors.Is(err, domain.ErrInstructorBudgetExceeded) {
		t.Fatalf("review notify admission=%v", err)
	}
	if runtime.sentMsg != "" || len(runtime.sentMsgs) != 0 {
		t.Fatal("review notify bypassed turn admission")
	}
}

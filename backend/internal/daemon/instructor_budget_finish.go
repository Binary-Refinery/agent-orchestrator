package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func (g instructorGovernance) FinishManagedSession(ctx context.Context, owner domain.ConversationOwner) error {
	budget, err := g.budgetOwner(ctx, owner, "")
	if err != nil {
		return err
	}
	if budget.resource == "" && owner.Kind == domain.ConversationOwnerSession {
		instances, ok := g.store.(interface {
			InstructorSessionInstance(context.Context, domain.SessionID) (string, bool, error)
		})
		if !ok {
			return nil
		}
		instance, found, err := instances.InstructorSessionInstance(ctx, budget.sessionID)
		if err != nil || !found {
			return err
		}
		budget.resource = "session:" + instance
	}
	if budget.resource == "" {
		return nil
	}
	store, ok := g.store.(interface {
		FinishInstructorSessionBudgets(context.Context, string, time.Time) error
	})
	if !ok {
		return nil
	}
	return store.FinishInstructorSessionBudgets(ctx, budget.resource, time.Now().UTC())
}

func (g instructorGovernance) FinishManagedReviewerHandle(ctx context.Context, handle string) error {
	worker, ok := strings.CutPrefix(handle, "review-")
	if !ok || worker == "" {
		return nil
	}
	reader, ok := g.store.(interface {
		ListReviewsBySession(context.Context, domain.SessionID) ([]domain.Review, error)
	})
	if !ok {
		return nil
	}
	reviews, err := reader.ListReviewsBySession(ctx, domain.SessionID(worker))
	if err != nil {
		return err
	}
	for _, review := range reviews {
		if review.InterfaceMode != domain.ReviewerInterfaceChat && review.ReviewerHandleID == handle {
			if err := g.FinishManagedSession(ctx, domain.ReviewConversationOwner(review.ID)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g instructorGovernance) CancelManagedBudgetBeforeAdmission(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	store, ok := g.store.(interface {
		CancelInstructorBudgetBeforeAdmission(context.Context, string, time.Time) error
	})
	if !ok {
		return nil
	}
	return store.CancelInstructorBudgetBeforeAdmission(ctx, token, time.Now().UTC())
}

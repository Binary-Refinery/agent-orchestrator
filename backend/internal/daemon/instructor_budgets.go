package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type instructorBudgetOwner struct {
	work      domain.InstructorWork
	resource  string
	sessionID domain.SessionID
	reviewID  string
	managed   bool
}

func (g instructorGovernance) budgetOwner(ctx context.Context, owner domain.ConversationOwner, clientMessageID string) (instructorBudgetOwner, error) {
	id := domain.SessionID(owner.ID)
	reviewID, suffix := "", ""
	if owner.Kind == domain.ConversationOwnerReview {
		reader, ok := g.store.(interface {
			GetReviewByID(context.Context, string) (domain.Review, bool, error)
		})
		if !ok {
			return instructorBudgetOwner{}, fmt.Errorf("review budget owner lookup unavailable")
		}
		review, found, err := reader.GetReviewByID(ctx, owner.ID)
		if err != nil {
			return instructorBudgetOwner{}, err
		}
		if !found {
			return instructorBudgetOwner{}, fmt.Errorf("review budget owner no longer exists")
		}
		id, reviewID = review.SessionID, review.ID
		suffix = "/review:" + review.ID + ":" + review.CreatedAt.UTC().Format("20060102T150405.999999999Z")
	}
	work, managed, err := g.svc.WorkForSession(ctx, id)
	dispatchInstance := ""
	if err == nil && clientMessageID != "" && owner.Kind != domain.ConversationOwnerReview {
		if dispatchWork, instance, matched, lookupErr := g.svc.WorkForDispatchTarget(ctx, clientMessageID, id); lookupErr != nil {
			return instructorBudgetOwner{work: work, sessionID: id, reviewID: reviewID, managed: managed}, lookupErr
		} else if matched {
			work, managed = dispatchWork, true
			dispatchInstance = instance
		}
	}
	if err != nil || !managed {
		return instructorBudgetOwner{work: work, sessionID: id, reviewID: reviewID, managed: managed}, err
	}
	instances, ok := g.store.(interface {
		InstructorSessionInstance(context.Context, domain.SessionID) (string, bool, error)
	})
	if !ok {
		return instructorBudgetOwner{work: work, sessionID: id, reviewID: reviewID, managed: true}, fmt.Errorf("immutable session instance reader unavailable")
	}
	instance, found, err := instances.InstructorSessionInstance(ctx, id)
	if err != nil {
		return instructorBudgetOwner{work: work, sessionID: id, reviewID: reviewID, managed: true}, err
	}
	if !found || instance == "" {
		return instructorBudgetOwner{work: work, sessionID: id, reviewID: reviewID, managed: true}, fmt.Errorf("managed budget owner no longer exists")
	}
	if dispatchInstance != "" && instance != dispatchInstance {
		return instructorBudgetOwner{work: work, sessionID: id, reviewID: reviewID, managed: true}, fmt.Errorf("dispatch target instance changed during budget admission")
	}
	return instructorBudgetOwner{work: work, resource: "session:" + instance + suffix, sessionID: id, reviewID: reviewID, managed: true}, nil
}

func (g instructorGovernance) ReserveManagedSession(ctx context.Context, owner domain.ConversationOwner, parent domain.SessionID) (string, error) {
	budget, err := g.budgetOwner(ctx, owner, "")
	if err != nil || !budget.managed {
		return "", err
	}
	entry, err := g.svc.ReserveBudget(ctx, budget.work.ID, "session", budget.resource, budget.sessionID, budget.reviewID, parent != "")
	return entry.ID, err
}

func (g instructorGovernance) ReserveManagedTurn(ctx context.Context, owner domain.ConversationOwner, turnID, clientMessageID string) (string, error) {
	budget, err := g.budgetOwner(ctx, owner, clientMessageID)
	if err != nil || !budget.managed {
		return "", err
	}
	bound, boundToWork, err := g.svc.WorkForSession(ctx, budget.sessionID)
	if err != nil {
		return "", err
	}
	controlDispatch := false
	if clientMessageID != "" && owner.Kind != domain.ConversationOwnerReview {
		_, _, controlDispatch, err = g.svc.WorkForDispatchTarget(ctx, clientMessageID, budget.sessionID)
		if err != nil {
			return "", err
		}
	}
	// Dispatch admission is action-aware (including stop while parked) and is
	// independently rechecked by the native semantic guard before this call.
	if !controlDispatch && (!boundToWork || bound.ID == budget.work.ID) {
		var authorityErr error
		if owner.Kind == domain.ConversationOwnerReview {
			// The retained worker owns the review's code and budget. Only the
			// reviewer executes, with its own launch/chat configuration guard.
			authorityErr = g.svc.ValidateExecutionAuthority(ctx, budget.sessionID)
		} else {
			authorityErr = g.svc.ValidateResume(ctx, budget.sessionID)
		}
		if authorityErr != nil {
			return "", fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryDeferred, authorityErr)
		}
	}
	// Adoption and project-wide orchestrators also consume one lifetime slot
	// for this work. Recording a turn must not bypass session admission merely
	// because its already-running controller was started before association.
	_, err = g.svc.ReserveBudget(ctx, budget.work.ID, "session", budget.resource, budget.sessionID, budget.reviewID, false)
	if err != nil {
		return "", err
	}
	if turnID == "" {
		return "", fmt.Errorf("%w: managed native turn needs a durable operation identity", instructorsvc.ErrValidation)
	}
	entry, err := g.svc.ReserveBudget(ctx, budget.work.ID, "turn", budget.resource+"/turn:"+turnID, budget.sessionID, budget.reviewID, false)
	if err != nil {
		return "", err
	}
	if entry.State == "uncertain" || entry.State == "started" || entry.State == "finished" {
		return "", instructorsvc.ErrDeliveryUncertain
	}
	return entry.ID, nil
}

func (g instructorGovernance) SetManagedBudgetState(ctx context.Context, token, state string) error {
	err := g.svc.SetBudgetState(ctx, token, state)
	if errors.Is(err, domain.ErrInstructorBudgetClaimed) {
		return fmt.Errorf("%w: %w", instructorsvc.ErrDeliveryUncertain, err)
	}
	return err
}

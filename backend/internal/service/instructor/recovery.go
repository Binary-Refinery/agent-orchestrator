package instructor

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrRecoveryRequired is conclusive pre-submission unavailability, not an
// uncertain native effect and not a new request for business approval.
var ErrRecoveryRequired = errors.New("project controller recovery required")

// ErrRecoveryInProgress reports an existing recovery operation without starting another.
var ErrRecoveryInProgress = errors.New("project controller recovery already in progress")

type controllerRecovery interface {
	RecoveryStatus(context.Context, domain.InstructorWork) (domain.InstructorRecoveryStatus, error)
	RecoverController(context.Context, domain.InstructorWork, domain.InstructorRecoveryInput) error
}

// Recovery observes the controller needed by this Work without changing it.
func (s *Service) Recovery(ctx context.Context, id string) (domain.InstructorRecoveryStatus, error) {
	w, err := s.get(ctx, id)
	if err != nil {
		return domain.InstructorRecoveryStatus{}, err
	}
	r, ok := s.deps.Delivery.(controllerRecovery)
	if !ok {
		return domain.InstructorRecoveryStatus{}, invalid("controller recovery unavailable")
	}
	return r.RecoveryStatus(ctx, w)
}

// Recover reconnects one existing native conversation. No work state, gate,
// approval, policy or dispatch is rewritten by this operation.
func (s *Service) Recover(ctx context.Context, in domain.InstructorRecoveryInput) (domain.InstructorRecoveryStatus, error) {
	w, err := s.get(ctx, in.WorkID)
	if err != nil {
		return domain.InstructorRecoveryStatus{}, err
	}
	r, ok := s.deps.Delivery.(controllerRecovery)
	if !ok {
		return domain.InstructorRecoveryStatus{}, invalid("controller recovery unavailable")
	}
	current, err := r.RecoveryStatus(ctx, w)
	if err != nil {
		return current, err
	}
	if in.SessionID == "" || in.SessionID != current.SessionID || !in.SessionCreatedAt.Equal(current.SessionCreatedAt) || in.ProviderConversationID != current.ProviderConversationID {
		return current, ErrConflict
	}
	// A retry after successful adoption observes the same live conversation;
	// it must not stop/restart it or replay the original work directive.
	if current.Ready {
		return current, nil
	}
	if w.Revision != in.WorkRevision || w.PolicyVersion != in.PolicyVersion || current.ControllerGeneration != in.ControllerGeneration {
		return current, ErrConflict
	}
	if current.Action == "none" {
		if current.ReasonCode == "SESSION_RECOVERY_IN_PROGRESS" {
			return current, ErrRecoveryInProgress
		}
		return current, fmt.Errorf("%w: %s", ErrRecoveryRequired, current.Reason)
	}
	if err := r.RecoverController(ctx, w, in); err != nil {
		return current, err
	}
	return s.Recovery(ctx, w.ID)
}

func (s *Service) recoveryReadiness(ctx context.Context, w domain.InstructorWork) (*domain.InstructorRecoveryStatus, error) {
	r, ok := s.deps.Delivery.(controllerRecovery)
	if !ok || w.State == "done" || w.State == "cancelled" || w.Cancellation != nil {
		return nil, nil
	}
	status, err := r.RecoveryStatus(ctx, w)
	return &status, err
}

func (s *Service) requireControllerReady(ctx context.Context, w domain.InstructorWork) error {
	status, err := s.recoveryReadiness(ctx, w)
	if err != nil {
		return err
	}
	if status != nil && !status.Ready {
		return fmt.Errorf("%w: %s", ErrRecoveryRequired, status.Reason)
	}
	return nil
}

func withholdDeliveryActions(w *domain.InstructorWork) {
	w.Actions = slices.DeleteFunc(w.Actions, func(action string) bool {
		return action == "go" || action == "weiter" || action == "pruefen"
	})
}

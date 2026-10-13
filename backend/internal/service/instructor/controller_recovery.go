package instructor

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ValidateControllerRecovery admits observation of an existing conversation.
// It grants no execution authority: native turns still pass ValidateResume and
// the action-aware semantic/budget admission boundaries before submission.
func (s *Service) ValidateControllerRecovery(ctx context.Context, id domain.SessionID) error {
	w, managed, err := s.WorkForSession(ctx, id)
	if err != nil || !managed {
		return err
	}
	if w.Cancellation != nil || w.State == "done" || w.State == "cancelled" {
		return invalid("terminal or cancelled work cannot recover a controller")
	}
	if err := ValidatePolicy(w.Policy); err != nil {
		return err
	}
	if w.PolicyVersion != PolicyVersion(w.Policy) {
		return invalid("pinned work policy hash mismatch")
	}
	// Expired exceptions cannot authorize another native execution. Reconnecting
	// for observation leaves those grants unchanged and the turn gate closed.
	role, adopted, err := s.sessionRole(ctx, w, id)
	if err != nil {
		return err
	}
	return s.validateManagedSessionSelection(ctx, w, id, role, adopted)
}

package instructor

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ValidateDispatchAdmission is called by the native chat queue immediately
// before submission. Unmanaged messages retain their existing behavior.
func (s *Service) ValidateDispatchAdmission(ctx context.Context, target domain.SessionID, id string) error {
	dispatches, err := s.deps.Store.ListInstructorDispatches(ctx, "", "")
	if err != nil {
		return err
	}
	for _, d := range dispatches {
		if d.ID != id {
			continue
		}
		if d.TargetSessionID == "" || d.TargetSessionID != target {
			return invalid("native submission target does not match the immutable dispatch target")
		}
		if d.State == "cancelled" || d.State == "failed" {
			return invalid("native submission was cancelled or requires explicit reconciliation")
		}
		w, err := s.get(ctx, d.WorkID)
		if err != nil {
			return err
		}
		if w.State == "done" || w.State == "cancelled" {
			return invalid("native submission work is already closed")
		}
		if w.PolicyVersion != PolicyVersion(w.Policy) {
			return invalid("native submission pinned policy hash mismatch")
		}
		if d.Action != "pruefen" && d.Action != "stopp" {
			if w.Revision != d.WorkRevision {
				return ErrConflict
			}
			if err := validateWorkExceptions(w, s.now()); err != nil {
				return err
			}
			if err := s.CurrentEvidence(ctx, w); err != nil {
				return err
			}
			if w.State == "parked" || w.State == "waiting_human" || w.State == "blocked" {
				return invalid("native execution submission is not authorized in the current work state")
			}
		}
		return nil
	}
	// An ordinary native turn is not an Instructor dispatch, but a managed
	// corrective worker still has to satisfy its pinned human admission gate.
	w, managed, err := s.WorkForSession(ctx, target)
	if err != nil {
		return err
	}
	if managed && sessionRole(w, target) == "worker" {
		return s.ValidateResume(ctx, target)
	}
	return nil
}

// Caller holds s.mu; native cancellation is bounded by the caller context.
func (s *Service) cancelNativePending(ctx context.Context, w *domain.InstructorWork, events *[]domain.InstructorEvent) error {
	dispatches, err := s.deps.Store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil {
		return err
	}
	for _, d := range dispatches {
		if d.TargetSessionID == "" || d.State != "pending" && d.State != "leased" || d.QueuedAt == nil && d.State != "leased" {
			continue
		}
		var cancelErr error
		if canceller, ok := s.deps.Delivery.(DeliveryCanceller); ok {
			cancelErr = canceller.Cancel(ctx, d)
		} else {
			cancelErr = fmt.Errorf("%w: native queue cancellation is unavailable", ErrDeliveryUncertain)
		}
		if cancelErr == nil {
			*events = append(*events, s.event(*w, "native_queue_cancelled", d.ID))
			continue
		}
		s.markUncertain(w, d, "Stopp gesetzt; native Warteschlange muss abgeglichen werden")
		w.Summary = w.Gate.Title
		w.WaitCondition = "Geparkte Arbeit; weitere Schritte gesperrt, bis die bereits eingereihte Nachricht abgeglichen ist: " + cancelErr.Error()
		*events = append(*events, s.event(*w, "native_queue_cancellation_uncertain", d.ID+": "+cancelErr.Error()))
	}
	return nil
}

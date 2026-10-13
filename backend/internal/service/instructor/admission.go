package instructor

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrExecutionDeferred preserves an ordinary queued turn while its work is
// paused. It conveys no permission to retry a previously submitted operation.
var ErrExecutionDeferred = errors.New("managed work is paused; queued execution deferred")

// ValidateDispatchAdmission is called by the native chat queue immediately
// before submission. Unmanaged messages retain their existing behavior.
func (s *Service) ValidateDispatchAdmission(ctx context.Context, target domain.SessionID, id string) error {
	dispatches, w, managed, err := s.dispatchAdmissionSnapshot(ctx, target, id)
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
		if reader, ok := s.deps.Store.(sessionInstanceReader); ok {
			instance, found, err := reader.InstructorSessionInstance(ctx, target)
			if err != nil {
				return err
			}
			if !found || instance == "" || instance != d.TargetSessionInstanceID {
				return invalid("native submission target session instance changed or is legacy unknown")
			}
		}
		if d.State == "cancelled" || d.State == "failed" {
			return invalid("native submission was cancelled or requires explicit reconciliation")
		}
		for _, previous := range dispatches {
			if previous.BatchID == d.BatchID && previous.Position < d.Position && (previous.State == "failed" || previous.State == "cancelled") {
				return errApprovalCohortStopped
			}
		}
		if w.Cancellation != nil || w.State == "done" || w.State == "cancelled" {
			return invalid("native submission work is already closed")
		}
		if w.PolicyVersion != PolicyVersion(w.Policy) {
			return invalid("native submission pinned policy hash mismatch")
		}
		if d.Action == "worker_result" {
			if w.Phase != d.Phase || w.PhaseSequence != d.PhaseSequence || d.PolicyVersion != w.PolicyVersion {
				return ErrConflict
			}
			if w.State == "parked" || w.State == "waiting_human" || w.State == "blocked" {
				return invalid("worker completion wake-up cannot bypass paused work")
			}
			return nil // Claim delivery requests verification; it does not execute or certify the result.
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
			if containsSession(w, target) {
				if err := s.validateManagedResume(ctx, w, target); err != nil {
					return err
				}
			}
		}
		return nil
	}
	// Ordinary native turns also obey the current managed work authority,
	// including sessions adopted without managed launch provenance.
	if managed {
		if w.Cancellation == nil && (w.State == "parked" || w.State == "waiting_human" || w.State == "blocked") {
			return ErrExecutionDeferred
		}
		return s.validateManagedResume(ctx, w, target)
	}
	return nil
}

// Reports can atomically carry a current directive to the next informational
// revision. Read the directive and Work under one mutation fence, then release
// it before native capability discovery. The immediate native budget/producer
// admission still checks current authority before a provider turn starts.
func (s *Service) dispatchAdmissionSnapshot(ctx context.Context, target domain.SessionID, id string) ([]domain.InstructorDispatch, domain.InstructorWork, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dispatches, err := s.deps.Store.ListInstructorDispatches(ctx, "", "")
	if err != nil {
		return nil, domain.InstructorWork{}, false, err
	}
	for _, d := range dispatches {
		if d.ID == id {
			w, err := s.get(ctx, d.WorkID)
			return dispatches, w, true, err
		}
	}
	w, managed, err := s.WorkForSession(ctx, target)
	return dispatches, w, managed, err
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

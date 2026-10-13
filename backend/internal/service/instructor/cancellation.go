package instructor

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// CancellationObserver supplies actual native quiescence, not session display
// status. Its captured producer epochs are rechecked inside the final writer.
type CancellationObserver interface {
	ObserveCancellation(context.Context, domain.InstructorWork) (domain.InstructorCancellationProof, bool, error)
}

func (s *Service) cancelWorkPhaseActions(w domain.InstructorWork) {
	s.verificationMu.Lock()
	defer s.verificationMu.Unlock()
	if cancel := s.verificationCancels[w.ID]; cancel != nil {
		cancel()
	}
	for _, action := range w.PhaseActions {
		if cancel := s.verificationCancels[action.ID]; cancel != nil {
			cancel()
		}
	}
}

// A delivered interrupt is only admission, not proof that native work stopped.
// Unknown effects remain visible and are never automatically sent a second time.
func (s *Service) observeCancellation(ctx context.Context, captured domain.InstructorWork) error {
	if captured.Cancellation.CompletedAt != nil {
		return nil
	}
	rows, err := s.deps.Store.ListInstructorDispatches(ctx, "", captured.ID)
	if err != nil {
		return err
	}
	inspectNative := false
	for _, d := range rows {
		if d.ID == captured.Cancellation.DispatchID {
			inspectNative = d.State == "delivered" || d.State == "failed" || d.State == "cancelled"
		}
	}
	var proof domain.InstructorCancellationProof
	settled := false
	var observationErr error
	if observer, ok := s.deps.Delivery.(CancellationObserver); inspectNative && ok {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		proof, settled, observationErr = observer.ObserveCancellation(probeCtx, captured)
		cancel()
	}
	for _, action := range captured.PhaseActions {
		if action.State == "running" {
			settled = false
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.get(ctx, captured.ID)
	if err != nil {
		return err
	}
	if w.Revision != captured.Revision || w.Cancellation == nil || w.Cancellation.CompletedAt != nil {
		return nil
	}
	if settled {
		// The same current producers are positively quiet, so failed control attempts
		// no longer represent a live effect. Other (e.g. coordinator queue) uncertainty
		// is preserved and must be reconciled independently.
		for _, d := range rows {
			if d.Action == "abbrechen" {
				s.clearUncertain(&w, d.ID, rows)
			}
		}
		if len(w.UncertainDispatchIDs) == 0 && (w.Gate == nil || w.Gate.Kind != "delivery_uncertain") {
			now := s.now()
			w.Cancellation.CompletedAt, w.Cancellation.Observation = &now, "Native producers positively quiescent; no running mechanical action remains."
			w.State, w.Summary, w.WaitCondition = "cancelled", "Arbeit abgebrochen; native Wirkung bestätigt", ""
			w.WaitUntil = nil
			// Business approval gates are obsolete after permanent cancel.
			// An existing publication violation remains visible as audit debt.
			if w.Gate == nil || w.Gate.Kind != "publication_violation" {
				w.Gate = nil
			}
			if w.SuspendedGate != nil && w.SuspendedGate.Kind == "publication_violation" {
				w.Gate = w.SuspendedGate
			}
			w.SuspendedGate = nil
			store, ok := s.deps.Store.(interface {
				CompleteInstructorCancellation(context.Context, domain.InstructorWork, int64, domain.InstructorEvent, domain.InstructorCancellationProof) (bool, error)
			})
			if !ok {
				return invalid("atomic cancellation proof storage unavailable")
			}
			expected := w.Revision
			w.Revision++
			w.UpdatedAt = now
			w.Actions = nil
			saved, err := store.CompleteInstructorCancellation(ctx, w, expected, s.event(w, "operator_cancel_completed", w.Cancellation.DispatchID), proof)
			if errors.Is(err, domain.ErrInstructorProducerChanged) {
				// Native workers may be quiet while a coordinator directive,
				// reservation or publication still lacks terminal proof. Keep
				// the unchanged durable intent and surface that wait below.
				observationErr = err
				fresh, readErr := s.get(ctx, captured.ID)
				if readErr != nil {
					return readErr
				}
				if fresh.Cancellation == nil || fresh.Cancellation.CompletedAt != nil {
					return nil
				}
				w = fresh
			} else {
				if err != nil {
					return err
				}
				if !saved {
					return ErrConflict
				}
				return nil
			}
		}
	}
	if s.now().Before(w.Cancellation.RequestedAt.Add(2 * time.Minute)) {
		return nil
	}
	reason := "Abbruch bleibt gesperrt: native Ruhe ist nicht bestätigt. TUI-Interrupt allein beweist keine beendete Ausführung; fehlende oder ersetzte Produzenten bleiben unklar."
	if observationErr != nil {
		reason += " " + observationErr.Error()
	}
	if len(w.UncertainDispatchIDs) > 0 {
		reason += " Bereits eingereihte native Aufträge müssen einzeln abgeglichen werden."
	}
	if w.Cancellation.Observation == reason {
		return nil
	}
	w.Cancellation.Observation = reason
	w.Summary, w.WaitCondition = "Abbruch braucht native Bestätigung", reason
	if w.Gate == nil || w.Gate.Kind != "delivery_uncertain" && w.Gate.Kind != "publication_violation" {
		if w.Gate != nil {
			g := *w.Gate
			w.SuspendedGate = &g
		}
		w.Gate = &domain.InstructorGate{ID: "cancel-" + w.Cancellation.DispatchID, Kind: "cancellation_pending", Title: "Native Unterbrechung prüfen; Abbruch bleibt dauerhaft", DispatchID: w.Cancellation.DispatchID, OpenedAt: s.now()}
	}
	_, err = s.save(ctx, w, w.Revision, "operator_cancel_awaiting_native", reason)
	return err
}

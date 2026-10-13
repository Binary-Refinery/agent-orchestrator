package instructor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// observeWorkerWakeups inspects durable native receipts; it never sends again.
// A late genuine receipt can settle an uncertain delivery, but it cannot grant
// a suspended business gate or make a worker result into verified evidence.
func (s *Service) observeWorkerWakeups(ctx context.Context, captured domain.InstructorWork) error {
	if captured.Cancellation != nil {
		return nil
	}
	dispatches, err := s.deps.Store.ListInstructorDispatches(ctx, "", captured.ID)
	if err != nil {
		return err
	}
	for _, d := range dispatches {
		if d.ReportID == "" || d.State == "cancelled" || d.State == "started" {
			continue
		}
		if d.NativeReceipt != nil && d.NativeReceipt.StartedAt != nil {
			s.mu.Lock()
			w, err := s.get(ctx, d.WorkID)
			if err == nil {
				err = s.deps.Store.ReconcileInstructorDispatch(ctx, d.ID, "started", "correlated provider execution observed", s.now())
			}
			if err == nil {
				s.clearUncertain(&w, d.ID, dispatches)
				if strings.HasPrefix(w.WaitCondition, "Orchestrator-Turn") || strings.HasPrefix(w.WaitCondition, "Native Nachricht") || w.Gate == nil && w.State == "waiting_human" {
					if w.State != "parked" {
						w.State = "ready"
						if w.Gate != nil {
							w.State = "waiting_human"
						}
					}
					w.WaitCondition, w.WaitUntil = "", nil
					w.Summary = "Orchestrator-Turn nach Worker-Ergebnis gestartet"
					_, err = s.save(ctx, w, w.Revision, "worker_wakeup_started", d.ID+" providerTurn="+d.NativeReceipt.ProviderTurnID)
				}
			}
			s.mu.Unlock()
			if err != nil {
				return err
			}
			continue
		}
		if d.State != "delivered" && d.State != "accepted" && d.State != "progress" {
			continue
		}
		deadline := d.CreatedAt.Add(2 * time.Minute)
		if d.NativeReceipt != nil && d.NativeReceipt.AcceptedAt != nil {
			deadline = d.NativeReceipt.AcceptedAt.Add(2 * time.Minute)
		}
		if !s.now().Before(deadline) {
			cause := fmt.Errorf("%w: Orchestrator-Turn für report=%s wurde innerhalb 120 Sekunden nicht nativ bestätigt; kein erneutes Senden", ErrDeliveryUncertain, d.ReportID)
			if err := s.deps.Store.SetInstructorDispatch(ctx, d.ID, "failed", cause.Error(), s.now()); err != nil {
				return err
			}
			if err := s.deliveryFailed(ctx, d, cause); err != nil {
				return err
			}
			continue
		}
		s.mu.Lock()
		w, err := s.get(ctx, d.WorkID)
		if err == nil && w.State != "parked" && w.State != "blocked" && w.State != "waiting_human" && w.State != "done" && w.State != "cancelled" && !strings.HasPrefix(w.WaitCondition, "Orchestrator-Turn") {
			w.State, w.WaitCondition, w.WaitUntil = "waiting_external", "Orchestrator-Turn nach Worker-Ergebnis wartet auf nativen Startbeleg", &deadline
			_, err = s.save(ctx, w, w.Revision, "worker_wakeup_awaiting_start", d.ID)
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

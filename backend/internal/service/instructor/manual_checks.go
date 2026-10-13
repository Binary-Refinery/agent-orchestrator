package instructor

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type phaseActionContextKey struct{}

// Direct HTTP/CLI checks use the same durable mechanical-action record as the
// daemon driver. A cancellation therefore sees and interrupts either entry.
func (s *Service) beginManualChecks(ctx context.Context, captured domain.InstructorWork, producer domain.SessionID) (context.Context, func(error) (*domain.InstructorWork, error), error) {
	if ctx.Value(phaseActionContextKey{}) != nil {
		return ctx, func(error) (*domain.InstructorWork, error) { return nil, nil }, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.get(ctx, captured.ID)
	if err != nil {
		return ctx, nil, err
	}
	if w.Cancellation != nil || w.State == "done" || w.State == "cancelled" || w.State == "parked" || w.State == "blocked" || w.State == "waiting_human" || w.PhaseSequence != captured.PhaseSequence || w.PolicyVersion != captured.PolicyVersion || w.SHA != captured.SHA {
		return ctx, nil, ErrConflict
	}
	for _, action := range w.PhaseActions {
		if action.State == "running" && action.Kind == "checks" {
			return ctx, nil, invalid("checks already running or require recovery; no duplicate mechanical execution")
		}
	}
	id := "operator-checks-" + s.deps.NewID()
	runCtx, cancel := context.WithCancel(ctx)
	w.PhaseActions = append(w.PhaseActions, domain.InstructorPhaseAction{ID: id, Kind: "checks", PhaseSequence: w.PhaseSequence, Producer: producer, State: "running", StartedAt: s.now()})
	if _, err := s.save(ctx, w, w.Revision, "phase_action_started", id); err != nil {
		cancel()
		return ctx, nil, err
	}
	s.verificationMu.Lock()
	s.phaseRunning[id] = true
	s.verificationCancels[id] = cancel
	s.verificationMu.Unlock()
	return runCtx, func(runErr error) (*domain.InstructorWork, error) {
		cancel()
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finishCancel()
		s.mu.Lock()
		current, err := s.get(finishCtx, w.ID)
		if err == nil {
			for index := range current.PhaseActions {
				a := &current.PhaseActions[index]
				if a.ID != id {
					continue
				}
				now := s.now()
				a.State, a.CompletedAt = "completed", &now
				if runErr != nil {
					a.State, a.Error = "failed", runErr.Error()
				}
			}
			if runErr != nil && current.Cancellation == nil && (current.State == "ready" || current.State == "running") && current.PhaseSequence == captured.PhaseSequence && current.PolicyVersion == captured.PolicyVersion && current.SHA == captured.SHA {
				current.State, current.Summary, current.WaitCondition = "waiting_human", "Phasenaktion fehlgeschlagen", runErr.Error()
				if current.Gate == nil {
					current.Gate = &domain.InstructorGate{ID: "retry-" + id, Kind: "phase_action_failed", Title: "Fehler beheben und Phasenaktion erneut beauftragen", OpenedAt: s.now()}
				}
			}
			current, err = s.save(finishCtx, current, current.Revision, "phase_action_completed", id)
		}
		s.mu.Unlock()
		s.verificationMu.Lock()
		delete(s.phaseRunning, id)
		delete(s.verificationCancels, id)
		s.verificationMu.Unlock()
		if err != nil {
			return nil, err
		}
		return &current, nil
	}, nil
}

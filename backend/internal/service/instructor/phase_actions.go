package instructor

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// PhaseActions reuses the existing review engine; the Instructor is the only
// business phase reducer. It never asks a second loop to decide transitions.
type PhaseActions interface {
	ReviewLocal(context.Context, domain.InstructorWork, domain.SessionID) error
}

// SetPhaseActions is configured at daemon startup before Tick is started.
func (s *Service) SetPhaseActions(actions PhaseActions) { s.phaseActions = actions }

func (s *Service) schedulePhaseAction(ctx context.Context, id string) error {
	if s.phaseActions == nil {
		return nil
	}
	s.mu.Lock()
	w, err := s.get(ctx, id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if w.Cancellation != nil || w.State != "ready" && w.State != "running" {
		s.mu.Unlock()
		return nil
	}
	dispatches, err := s.deps.Store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	for _, d := range dispatches {
		if d.State == "pending" || d.State == "leased" {
			s.mu.Unlock()
			return nil // Deliver the phase directive before the action audit changes its revision.
		}
	}
	kind := ""
	satisfied := false
	var producer domain.SessionID
	switch w.Phase {
	case "implement", "fix":
		kind = "checks"
		satisfied = hasEvidence(w, "push") && hasEvidence(w, "full")
		for _, d := range dispatches {
			if d.State != "cancelled" && d.ReportID != "" && d.Phase == w.Phase && d.PhaseSequence == w.PhaseSequence && d.NativeReceipt != nil && d.NativeReceipt.StartedAt != nil {
				producer = d.ProducerSessionID
			}
		}
	case "review", "delta":
		kind = "review"
		satisfied = hasEvidence(w, "review-report") && w.Phase == "review" || hasEvidence(w, "delta") && w.Phase == "delta"
		for _, launch := range w.Launches {
			if launch.Role == "worker" && launch.SessionID != "" && (launch.Status == "launched" || launch.Status == "native-config-reported") {
				producer, kind = launch.SessionID, "review"
			}
		}
	}
	if kind == "" {
		s.mu.Unlock()
		return nil
	}
	actionID := fmt.Sprintf("mechanical-%s-%d-%s", w.ID, w.PhaseSequence, kind)
	attempts := 0
	var latest *domain.InstructorPhaseAction
	for index := range w.PhaseActions {
		a := &w.PhaseActions[index]
		if a.PhaseSequence != w.PhaseSequence || a.Kind != kind {
			continue
		}
		attempts++
		latest = a
	}
	if latest != nil && latest.State != "failed" {
		a := latest
		s.verificationMu.Lock()
		live := s.phaseRunning[a.ID]
		s.verificationMu.Unlock()
		if a.State == "running" && !live {
			if kind == "checks" && completedCheckAction(w, *a) {
				now := s.now()
				a.State, a.CompletedAt = "completed", &now
				_, err = s.save(ctx, w, w.Revision, "phase_action_recovered", a.ID+" verified native push/full profiles")
				s.mu.Unlock()
				return err
			}
			if kind == "review" {
				for _, launch := range w.Launches {
					if launch.Role != "reviewer" || launch.PhaseSequence != a.PhaseSequence || launch.SHA != w.SHA || launch.Status != "launched" || launch.ReviewRunID == "" || launch.LaunchedAt.Before(a.StartedAt) {
						continue
					}
					now := s.now()
					a.State, a.CompletedAt = "completed", &now
					_, err = s.save(ctx, w, w.Revision, "phase_action_recovered", a.ID+" existing native review run="+launch.ReviewRunID)
					s.mu.Unlock()
					return err
				}
			}
			w.State, w.Summary, w.WaitCondition = "blocked", "Unterbrochene Phasenaktion muss abgeglichen werden", "Native Wirkung der Aktion "+actionID+" ist nach Neustart unklar; kein automatischer Doppelstart"
			_, err = s.save(ctx, w, w.Revision, "phase_action_uncertain", actionID)
		}
		s.mu.Unlock()
		return err
	}
	if satisfied || producer == "" {
		s.mu.Unlock()
		return nil
	}
	if attempts > 0 {
		actionID += fmt.Sprintf("-attempt%d", attempts+1)
	}
	s.verificationMu.Lock()
	if s.verificationStopped {
		s.verificationMu.Unlock()
		s.mu.Unlock()
		return nil
	}
	select {
	case s.phaseSlots <- struct{}{}:
	default:
		s.verificationMu.Unlock()
		s.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.phaseRunning[actionID] = true
	s.verificationCancels[actionID] = cancel
	s.verificationWG.Add(1)
	s.verificationMu.Unlock()
	w.PhaseActions = append(w.PhaseActions, domain.InstructorPhaseAction{ID: actionID, Kind: kind, PhaseSequence: w.PhaseSequence, Producer: producer, State: "running", StartedAt: s.now()})
	w, err = s.save(ctx, w, w.Revision, "phase_action_started", actionID)
	s.mu.Unlock()
	if err != nil {
		cancel()
		s.finishPhaseSlot(actionID)
		return err
	}
	go func() {
		defer cancel()
		defer s.finishPhaseSlot(actionID)
		var runErr error
		if kind == "checks" {
			_, runErr = s.RunChecks(context.WithValue(runCtx, phaseActionContextKey{}, actionID), w.ID, producer, []string{"push", "full"})
		} else {
			runErr = s.validateLocalReviewWorkspace(runCtx, w, producer)
			if runErr == nil {
				runErr = s.phaseActions.ReviewLocal(runCtx, w, producer)
			}
		}
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finishCancel()
		s.mu.Lock()
		defer s.mu.Unlock()
		current, err := s.get(finishCtx, w.ID)
		if err != nil {
			return
		} // Retained running intent escalates on recovery.
		for index := range current.PhaseActions {
			a := &current.PhaseActions[index]
			if a.ID != actionID {
				continue
			}
			now := s.now()
			a.CompletedAt = &now
			a.State = "completed"
			if runErr != nil {
				a.State, a.Error = "failed", runErr.Error()
			}
		}
		if runErr != nil && current.State != "done" && current.State != "cancelled" && current.State != "parked" && current.State != "waiting_human" {
			current.State, current.Summary, current.WaitCondition = "waiting_human", "Phasenaktion fehlgeschlagen", runErr.Error()
			if current.Gate == nil {
				current.Gate = &domain.InstructorGate{ID: "retry-" + actionID, Kind: "phase_action_failed", Title: "Fehler beheben und Phasenaktion erneut beauftragen", OpenedAt: s.now()}
			}
		}
		_, _ = s.save(finishCtx, current, current.Revision, "phase_action_completed", actionID)
	}()
	return nil
}

// completedCheckAction recovers only native proofs committed for this action's
// producer, phase and current SHA. It never reruns an interrupted profile.
func completedCheckAction(w domain.InstructorWork, action domain.InstructorPhaseAction) bool {
	for _, profile := range []string{"push", "full"} {
		found := false
		for _, evidence := range w.Evidence {
			if evidence.Verified && !evidence.Revoked && evidence.Kind == "check" && evidence.Profile == profile && evidence.Producer == action.Producer && evidence.WorkID == w.ID && evidence.PolicyVersion == w.PolicyVersion && evidence.SHA == w.SHA && evidence.PhaseSequence == action.PhaseSequence && !evidence.CreatedAt.Before(action.StartedAt) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *Service) finishPhaseSlot(id string) {
	s.verificationMu.Lock()
	delete(s.phaseRunning, id)
	delete(s.verificationCancels, id)
	s.verificationMu.Unlock()
	<-s.phaseSlots
	s.verificationWG.Done()
}

func (s *Service) validateLocalReviewWorkspace(ctx context.Context, w domain.InstructorWork, producer domain.SessionID) error {
	bound, managed, err := s.WorkForSession(ctx, producer)
	if err != nil {
		return err
	}
	if !managed || bound.ID != w.ID || bound.PhaseSequence != w.PhaseSequence || bound.SHA != w.SHA {
		return ErrConflict
	}
	if s.deps.Workspace == nil {
		return invalid("local review workspace resolver unavailable")
	}
	path, err := s.deps.Workspace.WorkspaceForSession(ctx, producer)
	if err != nil {
		return err
	}
	sha, err := gitFact(ctx, path, "rev-parse", "HEAD")
	if err != nil || sha != w.SHA {
		return invalid("local review checkout does not match the exact work commit")
	}
	dirty, err := gitFact(ctx, path, "status", "--porcelain")
	if err != nil || dirty != "" {
		return invalid("local review requires a clean immutable worktree")
	}
	return nil
}

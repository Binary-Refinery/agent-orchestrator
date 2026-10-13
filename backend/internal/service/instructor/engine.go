package instructor

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// advanceWork is the daemon-owned bounded workflow reducer. It changes one
// business phase and queues the project orchestrator; it launches no worker.
func (s *Service) advanceWork(ctx context.Context, captured domain.InstructorWork) error {
	if captured.Cancellation != nil {
		return nil
	}
	if captured.ApprovalDispatchID != "" && captured.State != "done" && captured.State != "cancelled" && captured.State != "parked" {
		ready, err := s.approvalCohortReady(ctx, captured.ApprovalDispatchID)
		if errors.Is(err, errApprovalCohortStopped) {
			return s.fenceStoppedCohort(ctx, captured)
		}
		if err != nil {
			return err
		}
		if !ready {
			return nil
		}
	}
	if captured.State != "ready" && captured.State != "running" {
		return nil
	}
	if cause := validateWorkExceptions(captured, s.now()); cause != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		w, err := s.get(ctx, captured.ID)
		if err != nil {
			return err
		}
		if w.Revision != captured.Revision {
			return nil
		}
		w.State, w.Summary, w.WaitCondition = "blocked", "Befristete Ausnahme abgelaufen", cause.Error()
		_, err = s.save(ctx, w, w.Revision, "exception_expired", cause.Error())
		return err
	}
	for _, action := range captured.PhaseActions {
		if action.PhaseSequence == captured.PhaseSequence && action.State == "running" {
			return nil // Finish or recover its audit before freezing a new phase directive.
		}
	}
	ds, err := s.deps.Store.ListInstructorDispatches(ctx, "", captured.ID)
	if err != nil {
		return err
	}
	for _, d := range ds {
		if d.State == "cancelled" {
			continue // A fenced cancellation cannot start and no longer owns a wake-up obligation.
		}
		if d.State == "pending" || d.State == "leased" {
			return nil
		}
		if d.ReportID != "" && d.PhaseSequence == captured.PhaseSequence && (d.NativeReceipt == nil || d.NativeReceipt.StartedAt == nil) {
			return nil // Neither queue nor send/agent acknowledgment is an executing coordinator turn.
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.get(ctx, captured.ID)
	if err != nil {
		return err
	}
	if w.Revision != captured.Revision {
		return nil
	}
	// A terminal remote CI failure re-enters the same governed triage/fix/delta
	// loop before acceptance; publication never implies a successful closeout.
	if w.Phase == "closeout" && !hasEvidence(w, "ci-failed") {
		if err := s.CurrentEvidence(ctx, w); err != nil {
			return nil
		}
		if !HasPublicationGrant(w) && publicationReadiness(w) == nil {
			w.State = "waiting_human"
			w.Gate = &domain.InstructorGate{ID: "gate-" + s.deps.NewID(), Kind: "publication", Title: "Veröffentlichung des geprüften Commits freigeben", ExactApproval: "PUBLISH-GO " + w.Branch, OpenedAt: s.now()}
			w.Summary = "Lokale Checks und Review belegt; Veröffentlichung braucht exaktes Go"
			w.WaitCondition = "PUBLISH-GO für diesen Snapshot, diese Policy, Branch und SHA"
			_, err = s.save(ctx, w, w.Revision, "gate_opened", "publication "+w.Branch+" sha="+w.SHA)
			return err
		}
		if canClose(w) != nil {
			return nil
		}
		w.State = "done"
		w.Summary = "Mit vollständigen Belegen abgeschlossen"
		_, err = s.save(ctx, w, w.Revision, "closed", "policy evidence accepted")
		return err
	}
	for _, t := range w.Policy.Transitions {
		if t.From != w.Phase {
			continue
		}
		eligible := true
		for _, need := range t.Requires {
			if t.HumanGate && need == "triage" {
				continue
			}
			if !hasEvidence(w, need) {
				eligible = false
			}
		}
		if !eligible {
			continue
		}
		if len(t.Requires) > 0 {
			if err := s.CurrentEvidence(ctx, w); err != nil {
				return nil
			}
		}
		if t.HumanGate {
			granted := false
			for _, g := range w.ApprovedTransitions {
				if g == t.From+"->"+t.To {
					granted = true
				}
			}
			if !granted {
				w.State = "waiting_human"
				w.Gate = &domain.InstructorGate{ID: "gate-" + s.deps.NewID(), Kind: "transition", Title: t.From + " → " + t.To, TransitionTo: t.To, OpenedAt: s.now()}
				w.Summary = "Entscheidung für nächsten Schritt nötig"
				_, err = s.save(ctx, w, w.Revision, "gate_opened", t.From+"->"+t.To)
				return err
			}
		}
		if canAdvance(w, t.To, "instructor-engine") != nil {
			continue
		}
		if t.To == "fix" {
			if w.FixRounds >= w.Policy.MaxFixRounds {
				w.State = "blocked"
				w.Summary = "Korrekturbudget ausgeschöpft"
				_, err = s.save(ctx, w, w.Revision, "budget_exhausted", "fix rounds")
				return err
			}
			w.FixRounds++
		}
		w.Phase = t.To
		w.PhaseStartedAt = s.now()
		w.PhaseSequence++
		if t.To == "fix" {
			w.FixPhaseSequence = w.PhaseSequence
		}
		if t.To == "delta" {
			w.DeltaPhaseSequence = w.PhaseSequence
		}
		w.ApprovedTransitions = nil
		w.State = "ready"
		w.Next = t.To
		w.Summary = "Nächste Phase beauftragt"
		w.Revision++
		w.UpdatedAt = s.now()
		role := w.Policy.Roles[t.Role]
		id := fmt.Sprintf("phase-%s-%d", w.ID, w.Revision)
		message := fmt.Sprintf("Instructor workflow work=%s phase=%s policy=%s. Coordinate phase %s for goal: %s. Required execution role=%s harness=%s model=%s effort=%s. Preserve WorkID when spawning; use the pinned role and approved native effort mapping. Report accepted and progress with dispatch ID %s. No phase/closeout succeeds without required verified evidence.", w.ID, w.Phase, w.PolicyVersion, w.Phase, w.Goal, t.Role, role.Harness, role.Model, role.Effort, id)
		d := domain.InstructorDispatch{ID: id, BatchID: id, WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: w.Revision, Action: "weiter", Message: message, State: "pending", CreatedAt: s.now(), UpdatedAt: s.now()}
		return s.deps.Store.CommitInstructorBatch(ctx, []domain.InstructorDispatch{d}, []domain.InstructorWork{w}, []domain.InstructorEvent{s.event(w, "phase_advanced", t.From+"->"+t.To)}, nil)
	}
	return nil
}

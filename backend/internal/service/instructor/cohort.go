package instructor

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

var errApprovalCohortStopped = fmt.Errorf("%w: ordered approval cohort stopped; capture a new snapshot and explicitly approve again", ErrConflict)

func (s *Service) approvalCohortReady(ctx context.Context, dispatchID string) (bool, error) {
	if dispatchID == "" {
		return true, nil
	}
	all, err := s.deps.Store.ListInstructorDispatches(ctx, "", "")
	if err != nil {
		return false, err
	}
	var own *domain.InstructorDispatch
	for i := range all {
		if all[i].ID == dispatchID {
			own = &all[i]
			break
		}
	}
	if own == nil {
		return false, invalid("ordered approval dispatch is unavailable")
	}
	for _, d := range all {
		if d.BatchID != own.BatchID || d.Position > own.Position {
			continue
		}
		if d.State == "failed" || d.State == "cancelled" {
			return false, errApprovalCohortStopped
		}
		if d.State != "delivered" && d.State != "accepted" && d.State != "started" && d.State != "progress" {
			return false, nil
		}
	}
	return true, nil
}

// fenceStoppedCohort restores the exact captured gate and revokes only approval
// grants that never crossed their native admission position. Earlier admitted
// works keep their receipts, decisions and effects in the audit.
func (s *Service) fenceStoppedCohort(ctx context.Context, captured domain.InstructorWork) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.get(ctx, captured.ID)
	if err != nil {
		return err
	}
	if w.Revision != captured.Revision {
		return nil
	}
	if w.State == "waiting_human" && w.WaitCondition == errApprovalCohortStopped.Error() {
		return nil
	}
	ds, err := s.deps.Store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil {
		return err
	}
	var own *domain.InstructorDispatch
	for i := range ds {
		if ds[i].ID == w.ApprovalDispatchID {
			own = &ds[i]
			break
		}
	}
	if own == nil {
		return invalid("stopped approval dispatch is unavailable")
	}
	snapshot, found, err := s.deps.Store.GetInstructorSnapshot(ctx, own.SnapshotID)
	if err != nil {
		return err
	}
	if found {
		for _, original := range snapshot.Works {
			if original.ID != w.ID {
				continue
			}
			if original.Gate != nil {
				gate := *original.Gate
				if w.Gate != nil && w.Gate.Kind == "delivery_uncertain" {
					w.SuspendedGate = &gate
				} else {
					w.Gate = &gate
				}
			}
			w.ApprovedTransitions = append([]string(nil), original.ApprovedTransitions...)
			break
		}
	}
	if w.Gate == nil {
		w.Gate = &domain.InstructorGate{ID: "cohort-" + own.ID, Kind: "delivery", Title: "Sammelfreigabe angehalten", OpenedAt: s.now()}
	}
	for i := range w.PublicationGrants {
		if w.PublicationGrants[i].DispatchID == own.ID {
			w.PublicationGrants[i].Revoked = true
		}
	}
	w.State, w.Summary, w.WaitCondition = "waiting_human", "Sammelfreigabe angehalten; Entscheidung erneut prüfen", errApprovalCohortStopped.Error()
	_, err = s.save(ctx, w, w.Revision, "approval_cohort_stopped", fmt.Sprintf("batch=%s dispatch=%s", own.BatchID, own.ID))
	return err
}

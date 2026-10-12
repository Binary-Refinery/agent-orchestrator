package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func (d instructorDelivery) stopManagedWork(ctx context.Context, dispatch domain.InstructorDispatch) error {
	reader, ok := d.store.(interface {
		GetInstructorWork(context.Context, string) (domain.InstructorWork, bool, error)
		ListInstructorSessionMembers(context.Context, string) ([]domain.InstructorSessionMembership, error)
		InstructorSessionForMembership(context.Context, domain.SessionID) (domain.SessionRecord, string, bool, error)
	})
	control, canControl := d.sessions.(interface {
		InterruptInstructorProducer(context.Context, domain.SessionID, string) error
	})
	if !ok || !canControl {
		return fmt.Errorf("%w: native work pause capability unavailable", instructorsvc.ErrDeliveryUncertain)
	}
	w, found, err := reader.GetInstructorWork(ctx, dispatch.WorkID)
	if err != nil {
		return err
	}
	if !found || w.ProjectID != dispatch.ProjectID {
		return instructorsvc.ErrConflict
	}
	if w.State != "parked" {
		if w.Revision != dispatch.WorkRevision {
			// Work changed after protected reload, before any member/interrupt
			// operation. Discard only this superseded attempt, not the new intent.
			return instructorsvc.ErrStopSuperseded
		}
		return instructorsvc.ErrConflict
	}
	if w.Revision != dispatch.WorkRevision {
		if reader, ok := d.store.(interface {
			ListInstructorDispatches(context.Context, string, string) ([]domain.InstructorDispatch, error)
		}); ok {
			rows, err := reader.ListInstructorDispatches(ctx, "", w.ID)
			if err != nil {
				return err
			}
			for _, current := range rows {
				if current.ID == dispatch.ID && current.Action == "stopp" && current.State == "leased" && current.ProjectID == w.ProjectID && current.WorkRevision == w.Revision {
					return fmt.Errorf("%w: same stop was preserved across its terminal action audit; no interrupt attempted", instructorsvc.ErrDeliveryRetryable)
				}
			}
		}
		return instructorsvc.ErrConflict
	}
	members, err := reader.ListInstructorSessionMembers(ctx, w.ID)
	if err != nil {
		return err
	}
	var failures []error
	for _, member := range members {
		rec, instance, found, err := reader.InstructorSessionForMembership(ctx, member.SessionID)
		if err != nil {
			failures = append(failures, fmt.Errorf("read native pause member %s: %w", member.SessionID, err))
			continue
		}
		if !found || member.InstanceID != instance || rec.Kind == domain.KindOrchestrator || rec.IsTerminated || rec.Activity.State == domain.ActivityExited {
			continue
		}
		if rec.ProjectID != w.ProjectID {
			failures = append(failures, fmt.Errorf("native pause member %s: %w", rec.ID, instructorsvc.ErrConflict))
			continue
		}
		if err := control.InterruptInstructorProducer(ctx, rec.ID, instance); err != nil {
			failures = append(failures, fmt.Errorf("native pause %s: %w", rec.ID, err))
		}
	}
	if err := errors.Join(failures...); err != nil {
		return fmt.Errorf("%w: %w", instructorsvc.ErrDeliveryUncertain, err)
	}
	return nil
}

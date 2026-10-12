package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

// ObserveCancellation reads only current, exactly admitted native producers.
// Missing/reused rows cannot prove an old process stopped and remain uncertain.
func (d instructorDelivery) ObserveCancellation(ctx context.Context, w domain.InstructorWork) (domain.InstructorCancellationProof, bool, error) {
	proof := domain.InstructorCancellationProof{}
	reader, ok := d.store.(interface {
		ListInstructorSessionMembers(context.Context, string) ([]domain.InstructorSessionMembership, error)
		InstructorSessionForMembership(context.Context, domain.SessionID) (domain.SessionRecord, string, bool, error)
	})
	control, canObserve := d.sessions.(interface {
		InstructorProducerQuiescent(context.Context, domain.SessionID, string) (bool, error)
	})
	if !ok {
		return proof, false, fmt.Errorf("%w: cancellation member observation unavailable", instructorsvc.ErrDeliveryUncertain)
	}
	members, err := reader.ListInstructorSessionMembers(ctx, w.ID)
	if err != nil {
		return proof, false, err
	}
	seen := map[domain.SessionID]bool{}
	for _, member := range members {
		seen[member.SessionID] = true
		if member.Role == string(domain.KindOrchestrator) {
			continue
		}
		rec, instance, found, err := reader.InstructorSessionForMembership(ctx, member.SessionID)
		if err != nil {
			return proof, false, err
		}
		if !found || instance != member.InstanceID || rec.ProjectID != w.ProjectID || member.Provenance != "admitted" {
			return proof, false, fmt.Errorf("%w: cancellation producer %s is missing, replaced or not explicitly admitted", instructorsvc.ErrDeliveryUncertain, member.SessionID)
		}
		quiet := rec.IsTerminated || rec.Activity.State == domain.ActivityExited
		if !quiet {
			if !canObserve {
				return proof, false, fmt.Errorf("%w: native cancellation observation unavailable", instructorsvc.ErrDeliveryUncertain)
			}
			quiet, err = control.InstructorProducerQuiescent(ctx, rec.ID, instance)
			if err != nil {
				return proof, false, err
			}
		}
		if !quiet {
			return proof, false, nil
		}
		runtime := rec.Metadata.RuntimeLaunchID
		if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
			runtime = "chat:" + rec.Metadata.ControllerGeneration
		}
		proof.Producers = append(proof.Producers, domain.InstructorReportAdmission{WorkID: w.ID, SessionID: rec.ID, InstanceID: instance, Member: true, RuntimeID: runtime})
	}
	for _, sid := range w.SessionIDs {
		if !seen[sid] {
			return proof, false, fmt.Errorf("%w: legacy cancellation producer %s has no incarnation proof", instructorsvc.ErrDeliveryUncertain, sid)
		}
	}
	reviewReader, canReview := d.store.(interface {
		GetReviewByID(context.Context, string) (domain.Review, bool, error)
	})
	seenReviews := map[string]bool{}
	for i := len(w.Launches) - 1; i >= 0; i-- {
		launch := w.Launches[i]
		if launch.ReviewID == "" || seenReviews[launch.ReviewID] {
			continue
		}
		seenReviews[launch.ReviewID] = true
		if !canReview {
			return proof, false, fmt.Errorf("%w: native reviewer observation unavailable", instructorsvc.ErrDeliveryUncertain)
		}
		review, found, err := reviewReader.GetReviewByID(ctx, launch.ReviewID)
		if err != nil {
			return proof, false, err
		}
		runtime := review.ReviewerLaunchID
		if runtime == "" {
			runtime = review.ControllerGeneration
		}
		if !found || review.ProjectID != w.ProjectID || launch.ReviewCreatedAt == nil || !review.CreatedAt.Equal(*launch.ReviewCreatedAt) || runtime != launch.RuntimeGeneration {
			return proof, false, fmt.Errorf("%w: native reviewer %s changed", instructorsvc.ErrDeliveryUncertain, launch.ReviewID)
		}
		quiet := review.ReviewerActivityState == domain.ActivityExited
		if !quiet && d.reviews != nil {
			quiet, err = d.reviews.InstructorReviewerQuiescent(ctx, review)
			if err != nil {
				return proof, false, err
			}
		}
		if !quiet {
			return proof, false, nil
		}
		proof.Reviews = append(proof.Reviews, domain.InstructorCancellationReviewProof{ID: review.ID, CreatedAt: review.CreatedAt, RuntimeID: runtime, ControllerGeneration: review.ControllerGeneration, HandleID: review.ReviewerHandleID, InterfaceMode: review.InterfaceMode, NativeQuiet: true})
	}
	return proof, true, nil
}

// The optional bridge is exact-review control, not cancellation of every review
// associated with a display session number.
type instructorReviewerControl interface {
	InterruptInstructorReviewer(context.Context, domain.Review) error
	InstructorReviewerQuiescent(context.Context, domain.Review) (bool, error)
}

func instructorReviewControl(v any) instructorReviewerControl {
	c, _ := v.(instructorReviewerControl)
	return c
}
func (d instructorDelivery) interruptWorkReviewers(ctx context.Context, w domain.InstructorWork) error {
	reader, ok := d.store.(interface {
		GetReviewByID(context.Context, string) (domain.Review, bool, error)
	})
	seen := map[string]bool{}
	var failures []error
	for i := len(w.Launches) - 1; i >= 0; i-- {
		launch := w.Launches[i]
		if launch.ReviewID == "" || seen[launch.ReviewID] {
			continue
		}
		seen[launch.ReviewID] = true
		if !ok || d.reviews == nil {
			failures = append(failures, errors.New("native reviewer cancellation unavailable"))
			continue
		}
		review, found, err := reader.GetReviewByID(ctx, launch.ReviewID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		runtime := review.ReviewerLaunchID
		if runtime == "" {
			runtime = review.ControllerGeneration
		}
		if !found || review.ProjectID != w.ProjectID || launch.ReviewCreatedAt == nil || !review.CreatedAt.Equal(*launch.ReviewCreatedAt) || runtime != launch.RuntimeGeneration {
			failures = append(failures, fmt.Errorf("native reviewer %s incarnation changed", launch.ReviewID))
			continue
		}
		if review.ReviewerActivityState == domain.ActivityExited {
			continue
		}
		if err := d.reviews.InterruptInstructorReviewer(ctx, review); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

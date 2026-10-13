package review

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// currentInstructorReviewer runs under the same worker lock as reviewer launch,
// recovery and switching. A replacement may never inherit a captured cancel.
func (e *Engine) currentInstructorReviewer(ctx context.Context, captured domain.Review) (domain.Review, error) {
	current, found, err := e.store.GetReviewBySessionAndHarness(ctx, captured.SessionID, captured.Harness)
	if err != nil {
		return current, err
	}
	if !found || current.ID != captured.ID || current.ProjectID != captured.ProjectID || !current.CreatedAt.Equal(captured.CreatedAt) || current.ReviewerLaunchID != captured.ReviewerLaunchID || current.ControllerGeneration != captured.ControllerGeneration || current.ReviewerHandleID != captured.ReviewerHandleID || current.InterfaceMode != captured.InterfaceMode {
		return current, domain.ErrInstructorProducerChanged
	}
	return current, nil
}

// InterruptInstructorReviewer invokes native cancellation for one admitted
// incarnation. It retains all rows and does not certify native completion.
func (e *Engine) InterruptInstructorReviewer(ctx context.Context, captured domain.Review) error {
	unlock := e.lockWorker(captured.SessionID)
	defer unlock()
	current, err := e.currentInstructorReviewer(ctx, captured)
	if err != nil {
		return err
	}
	if current.ReviewerActivityState == domain.ActivityExited {
		return nil
	}
	if current.ReviewerHandleID == "" {
		return ErrNotFound
	}
	return e.launcher.Cancel(ctx, current.ReviewerHandleID, current.Harness)
}

// InstructorReviewerQuiescent verifies quiet for the captured reviewer incarnation.
func (e *Engine) InstructorReviewerQuiescent(ctx context.Context, captured domain.Review) (bool, error) {
	unlock := e.lockWorker(captured.SessionID)
	defer unlock()
	current, err := e.currentInstructorReviewer(ctx, captured)
	if err != nil {
		return false, err
	}
	if current.ReviewerActivityState == domain.ActivityExited {
		return true, nil
	}
	if current.InterfaceMode != domain.ReviewerInterfaceChat || current.ControllerGeneration == "" {
		return false, nil
	}
	observer, ok := e.launcher.(interface {
		InstructorReviewerQuiescent(context.Context, domain.Review) (bool, error)
	})
	if !ok {
		return false, nil
	}
	return observer.InstructorReviewerQuiescent(ctx, current)
}

func (l *agentLauncher) InstructorReviewerQuiescent(ctx context.Context, captured domain.Review) (bool, error) {
	observer, ok := l.chat.(interface {
		InstructorReviewChatQuiescent(context.Context, string, string) (bool, error)
	})
	if !ok {
		return false, nil
	}
	return observer.InstructorReviewChatQuiescent(ctx, captured.ID, captured.ControllerGeneration)
}

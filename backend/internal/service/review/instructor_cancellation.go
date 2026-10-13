package review

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// InterruptInstructorReviewer interrupts only the captured native reviewer.
func (s *Service) InterruptInstructorReviewer(ctx context.Context, captured domain.Review) error {
	return s.engine.InterruptInstructorReviewer(ctx, captured)
}

// InstructorReviewerQuiescent observes the captured reviewer without deleting it.
func (s *Service) InstructorReviewerQuiescent(ctx context.Context, captured domain.Review) (bool, error) {
	return s.engine.InstructorReviewerQuiescent(ctx, captured)
}

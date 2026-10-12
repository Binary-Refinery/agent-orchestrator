package review

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
)

// SetInstructorGovernance keeps role enforcement at the actual controller
// launch/recovery boundary, after the engine resolves all reviewer overrides.
func (s *Service) SetInstructorGovernance(governance reviewcore.ReviewerGovernance) {
	s.engine.SetInstructorGovernance(governance)
}

// TriggerManagedLocal keeps local Instructor passes on the canonical engine.
func (s *Service) TriggerManagedLocal(ctx context.Context, id domain.SessionID, harness domain.ReviewerHarness, config domain.AgentConfig, sha string) (reviewcore.TriggerResult, error) {
	return s.engine.TriggerManagedLocal(ctx, id, harness, config, sha)
}

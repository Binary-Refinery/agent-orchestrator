package review

import reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"

// SetInstructorGovernance keeps role enforcement at the actual controller
// launch/recovery boundary, after the engine resolves all reviewer overrides.
func (s *Service) SetInstructorGovernance(governance reviewcore.ReviewerGovernance) {
	s.engine.SetInstructorGovernance(governance)
}

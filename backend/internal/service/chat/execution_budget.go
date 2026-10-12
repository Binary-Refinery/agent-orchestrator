package chat

import "github.com/aoagents/agent-orchestrator/backend/internal/ports"

// SetExecutionBudgetGuard installs native admission before recovery. Controllers
// apply it to all actual turns, including automation, relays, retries and queue
// promotion. Merely accepting a queued message does not spend a native turn.
func (s *Service) SetExecutionBudgetGuard(guard ports.ManagedExecutionBudgetGuard) {
	s.executionBudget = guard
}

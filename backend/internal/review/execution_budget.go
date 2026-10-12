package review

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (l *agentLauncher) executionBudget() ports.ManagedExecutionBudgetGuard {
	guard, _ := l.governance.(ports.ManagedExecutionBudgetGuard)
	return guard
}

func (l *agentLauncher) beginInstructorExecution(ctx context.Context, spec LaunchSpec, includeTurn bool) ([]string, error) {
	guard := l.executionBudget()
	if guard == nil {
		return nil, nil
	}
	owner := domain.ReviewConversationOwner(spec.ReviewSessionID)
	sessionToken, err := guard.ReserveManagedSession(ctx, owner, spec.WorkerID)
	if err != nil {
		return nil, err
	}
	tokens := []string{sessionToken}
	if includeTurn {
		turnToken, err := guard.ReserveManagedTurn(ctx, owner, "review:"+spec.RunID+":"+spec.LaunchID, "")
		if err != nil {
			_ = guard.SetManagedBudgetState(ctx, sessionToken, "cancelled")
			return nil, err
		}
		tokens = append(tokens, turnToken)
	}
	for _, token := range tokens {
		if err := guard.SetManagedBudgetState(ctx, token, "uncertain"); err != nil {
			return nil, err
		}
	}
	return tokens, nil
}

func (l *agentLauncher) recordInstructorExecution(ctx context.Context, tokens []string) error {
	for _, token := range tokens {
		if err := l.executionBudget().SetManagedBudgetState(ctx, token, "started"); err != nil {
			return err
		}
	}
	return nil
}

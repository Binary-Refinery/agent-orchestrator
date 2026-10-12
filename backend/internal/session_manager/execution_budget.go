package sessionmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (m *Manager) executionBudget() ports.ManagedExecutionBudgetGuard {
	guard, _ := m.instructor.(ports.ManagedExecutionBudgetGuard)
	return guard
}

func (m *Manager) reserveInstructorSessionBudget(ctx context.Context, id, parent domain.SessionID) (string, error) {
	if guard := m.executionBudget(); guard != nil {
		return guard.ReserveManagedSession(ctx, domain.SessionConversationOwner(id), parent)
	}
	return "", nil
}

func (m *Manager) setInstructorBudgetState(ctx context.Context, token, state string) error {
	if token == "" {
		return nil
	}
	return m.executionBudget().SetManagedBudgetState(ctx, token, state)
}

func (m *Manager) beginInstructorNativeController(ctx context.Context, id, parent domain.SessionID, turnKey string) ([]string, error) {
	tokens, err := m.reserveInstructorNativeController(ctx, id, parent, turnKey)
	if err != nil {
		return nil, err
	}
	if err := m.fenceInstructorNativeController(ctx, tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

func (m *Manager) reserveInstructorNativeController(ctx context.Context, id, parent domain.SessionID, turnKey string) ([]string, error) {
	guard := m.executionBudget()
	if guard == nil {
		return nil, nil
	}
	sessionToken, err := m.reserveInstructorSessionBudget(ctx, id, parent)
	if err != nil {
		return nil, err
	}
	tokens := []string{sessionToken}
	if turnKey != "" {
		turnToken, err := guard.ReserveManagedTurn(ctx, domain.SessionConversationOwner(id), turnKey, "")
		if err != nil {
			_ = guard.SetManagedBudgetState(ctx, sessionToken, "cancelled")
			return nil, err
		}
		tokens = append(tokens, turnToken)
	}
	return tokens, nil
}

func (m *Manager) fenceInstructorNativeController(ctx context.Context, tokens []string) error {
	for index, token := range tokens {
		if err := m.setInstructorBudgetState(ctx, token, "uncertain"); err != nil {
			// No native invocation follows a failed fence. Refund only claims
			// acquired by this call, never the token claimed by another attempt.
			cleanupCtx, cancel := switchDurableContext(ctx)
			for _, claimed := range tokens[:index] {
				if claimed != "" {
					_ = m.executionBudget().CancelManagedBudgetBeforeAdmission(cleanupCtx, claimed)
				}
			}
			cancel()
			return err
		}
	}
	return nil
}

func (m *Manager) finishInstructorNativeController(ctx context.Context, tokens []string) error {
	for _, token := range tokens {
		if err := m.setInstructorBudgetState(ctx, token, "started"); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) reserveInstructorTUITurn(ctx context.Context, id domain.SessionID, clientID string) (string, error) {
	guard := m.executionBudget()
	if guard == nil {
		return "", nil
	}
	key := clientID
	if key == "" {
		key = "send:" + m.newLaunchID()
	}
	token, err := guard.ReserveManagedTurn(ctx, domain.SessionConversationOwner(id), key, clientID)
	if err != nil {
		return "", err
	}
	if err := guard.SetManagedBudgetState(ctx, token, "uncertain"); err != nil {
		return "", err
	}
	return token, nil
}

// A successful native write consumes the turn. A failed receipt write leaves its
// durable uncertain reservation consumed and never permits an automatic replay.
func (m *Manager) completeInstructorTUITurn(ctx context.Context, token string, err error) error {
	if err != nil {
		return err
	}
	if recordErr := m.setInstructorBudgetState(ctx, token, "started"); recordErr != nil {
		return errors.Join(err, fmt.Errorf("persist turn budget: %w", recordErr))
	}
	return nil
}

package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ManagedExecutionBudgetGuard is installed before controller recovery. Native
// session/turn entry points reserve durable capacity before executing anything.
// A token stays consumed when the native effect cannot be established.
type ManagedExecutionBudgetGuard interface {
	ReserveManagedSession(context.Context, domain.ConversationOwner, domain.SessionID) (string, error)
	ReserveManagedTurn(context.Context, domain.ConversationOwner, string, string) (string, error)
	SetManagedBudgetState(context.Context, string, string) error
	FinishManagedSession(context.Context, domain.ConversationOwner) error
	CancelManagedBudgetBeforeAdmission(context.Context, string) error
}

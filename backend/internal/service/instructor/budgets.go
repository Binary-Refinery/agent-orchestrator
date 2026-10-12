package instructor

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type budgetStore interface {
	ReserveInstructorBudget(context.Context, domain.InstructorWork, domain.InstructorBudgetEntry) (domain.InstructorBudgetEntry, error)
	SetInstructorBudgetState(context.Context, string, string, time.Time) error
	InstructorBudgetUsage(context.Context, string) (domain.InstructorBudgetUsage, error)
}

// ReserveBudget applies the pinned limits at the actual execution boundary.
// Callers supply daemon-owned instance/turn identities, never request counters.
func (s *Service) ReserveBudget(ctx context.Context, workID, kind, resourceKey string, sessionID domain.SessionID, reviewID string, delegated bool) (domain.InstructorBudgetEntry, error) {
	work, err := s.get(ctx, workID)
	if err != nil {
		return domain.InstructorBudgetEntry{}, err
	}
	if err := validateWorkExceptions(work, s.now()); err != nil {
		return domain.InstructorBudgetEntry{}, err
	}
	if err := ValidatePolicy(work.Policy); err != nil {
		return domain.InstructorBudgetEntry{}, err
	}
	if work.PolicyVersion != PolicyVersion(work.Policy) {
		return domain.InstructorBudgetEntry{}, invalid("pinned work policy hash mismatch")
	}
	store, ok := s.deps.Store.(budgetStore)
	if !ok {
		if work.Policy.Budgets == nil {
			return domain.InstructorBudgetEntry{}, nil
		}
		return domain.InstructorBudgetEntry{}, invalid("durable budget storage unavailable")
	}
	if kind != "session" && kind != "turn" || resourceKey == "" {
		return domain.InstructorBudgetEntry{}, invalid("budget reservation needs a native resource identity")
	}
	entry := domain.InstructorBudgetEntry{ID: domain.InstructorBudgetReservationID(workID, kind, resourceKey), WorkID: work.ID, PolicyVersion: work.PolicyVersion, Kind: kind, ResourceKey: resourceKey, SessionID: sessionID, ReviewID: reviewID, Delegated: delegated, State: "reserved", CreatedAt: s.now(), UpdatedAt: s.now()}
	return store.ReserveInstructorBudget(ctx, work, entry)
}

// SetBudgetState records only internally established native-effect facts.
func (s *Service) SetBudgetState(ctx context.Context, token, state string) error {
	if token == "" {
		return nil
	}
	store, ok := s.deps.Store.(budgetStore)
	if !ok {
		return invalid("durable budget storage unavailable")
	}
	return store.SetInstructorBudgetState(ctx, token, state, s.now())
}

// BudgetUsage exposes durable counters for a work without changing its policy.
func (s *Service) BudgetUsage(ctx context.Context, workID string) (domain.InstructorBudgetUsage, error) {
	if _, err := s.get(ctx, workID); err != nil {
		return domain.InstructorBudgetUsage{}, err
	}
	store, ok := s.deps.Store.(budgetStore)
	if !ok {
		return domain.InstructorBudgetUsage{}, invalid("durable budget storage unavailable")
	}
	return store.InstructorBudgetUsage(ctx, workID)
}

// WorkForDispatchTarget attributes a governed native directive to its exact work
// and verifies the target session incarnation before reserving a budget.
func (s *Service) WorkForDispatchTarget(ctx context.Context, dispatchID string, target domain.SessionID) (domain.InstructorWork, string, bool, error) {
	reader, ok := s.deps.Store.(interface {
		InstructorWorkForDispatchTarget(context.Context, string, domain.SessionID) (domain.InstructorWork, string, bool, error)
	})
	if !ok {
		return domain.InstructorWork{}, "", false, nil
	}
	return reader.InstructorWorkForDispatchTarget(ctx, dispatchID, target)
}

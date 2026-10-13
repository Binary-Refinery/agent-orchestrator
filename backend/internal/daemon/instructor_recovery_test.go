package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type recoveryDispatchStore struct {
	instructorTestStore
	dispatches []domain.InstructorDispatch
}

func (s recoveryDispatchStore) ListInstructorDispatches(context.Context, string, string) ([]domain.InstructorDispatch, error) {
	return s.dispatches, nil
}

func TestInstructorRecoveryUsesCurrentCoordinatorAfterCompletedDispatch(t *testing.T) {
	for _, state := range []string{"delivered", "failed", "cancelled", "accepted", "progress", "started"} {
		t.Run(state, func(t *testing.T) {
			current := domain.SessionRecord{ID: "current", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: time.Unix(10, 0)}
			st := recoveryDispatchStore{instructorTestStore: instructorTestStore{sessions: []domain.SessionRecord{
				current,
				{ID: "newer-exited", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: time.Unix(20, 0), Activity: domain.Activity{State: domain.ActivityExited}},
			}}, dispatches: []domain.InstructorDispatch{{ID: "old-effect", State: state, TargetSessionID: "deleted-coordinator"}}}
			target, err := (instructorDelivery{store: st}).recoveryTarget(context.Background(), domain.InstructorWork{ID: "w", ProjectID: "p"})
			if err != nil || target.ID != current.ID {
				t.Fatalf("completed history blocked current coordinator: target=%s err=%v", target.ID, err)
			}
		})
	}
}

func TestInstructorRecoveryKeepsOutstandingDispatchBound(t *testing.T) {
	for _, state := range []string{"pending", "leased", "queued", "uncertain", "failed"} {
		t.Run(state, func(t *testing.T) {
			old := domain.SessionRecord{ID: "bound", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: time.Unix(10, 0), IsTerminated: true}
			st := recoveryDispatchStore{instructorTestStore: instructorTestStore{sessions: []domain.SessionRecord{
				old, {ID: "current", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: time.Unix(20, 0)},
			}}, dispatches: []domain.InstructorDispatch{{ID: "effect", State: state, TargetSessionID: old.ID, TargetCreatedAt: &old.CreatedAt}}}
			w := domain.InstructorWork{ID: "w", ProjectID: "p"}
			if state == "failed" {
				w.UncertainDispatchIDs = []string{"effect"}
			}
			d := instructorDelivery{store: st}
			target, err := d.recoveryTarget(context.Background(), w)
			if err != nil || target.ID != old.ID {
				t.Fatalf("outstanding effect retargeted: target=%s err=%v", target.ID, err)
			}
			st.sessions[0].CreatedAt = time.Unix(30, 0)
			d.store = st
			if _, err := d.recoveryTarget(context.Background(), w); !errors.Is(err, instructorsvc.ErrConflict) {
				t.Fatalf("reused session identity accepted: %v", err)
			}
		})
	}
}

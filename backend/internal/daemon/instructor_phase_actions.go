package daemon

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
)

type instructorLocalReviews interface {
	TriggerManagedLocal(context.Context, domain.SessionID, domain.ReviewerHarness, domain.AgentConfig, string) (reviewcore.TriggerResult, error)
}

type instructorPhaseActions struct{ reviews instructorLocalReviews }

func (a instructorPhaseActions) ReviewLocal(ctx context.Context, w domain.InstructorWork, producer domain.SessionID) error {
	role, ok := w.Policy.Roles["reviewer"]
	if !ok || !role.Independent || role.Model == "" {
		return errors.New("local review requires a configured independent native reviewer model")
	}
	effort := role.Effort
	if effort == "maximum" {
		effort = role.EffortByModel[role.Model]
	}
	if effort == "" || effort == "configured" || a.reviews == nil {
		return errors.New("local review requires the pinned model-native effort and review engine")
	}
	_, err := a.reviews.TriggerManagedLocal(ctx, producer, domain.ReviewerHarness(role.Harness), domain.AgentConfig{Model: role.Model, Effort: effort}, w.SHA)
	return err
}

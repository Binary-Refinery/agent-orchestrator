package daemon

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

// ValidateControllerRecovery preserves exact managed ownership and native
// configuration while permitting reconnect at a human gate. It does not admit
// any queued or new turn; those retain their ordinary execution guards.
func (g instructorGovernance) ValidateControllerRecovery(ctx context.Context, id domain.SessionID) error {
	work, managed, err := g.svc.WorkForSession(ctx, id)
	if err != nil {
		return err
	}
	if !managed {
		return g.ValidateResume(ctx, id)
	}
	if err := g.svc.ValidateControllerRecovery(ctx, id); err != nil {
		return err
	}
	rec, name, _, err := g.managedSessionRole(ctx, work, id)
	if err != nil {
		return err
	}
	if len(work.Policy.Backbone) != 0 {
		binding, _, err := g.svc.PrepareBackboneController(ctx, id, rec.Metadata.WorkspacePath,
			domain.NormalizeSessionMode(rec.Mode), rec.Metadata.Model, rec.Metadata.Permissions)
		if err != nil {
			return err
		}
		if binding != nil {
			// Native discovery belongs to the controlled ACP boundary after its
			// static input checks. Other roles retain the ordinary validator.
			return nil
		}
	}
	role, found := work.Policy.Roles[name]
	if !found || g.validator == nil {
		return fmt.Errorf("%w: managed controller role cannot be validated", instructorsvc.ErrValidation)
	}
	return g.validator.ValidateInstructorAgentConfig(ctx, work.ProjectID, role, string(rec.Harness), rec.Metadata.Model, rec.Metadata.Effort)
}

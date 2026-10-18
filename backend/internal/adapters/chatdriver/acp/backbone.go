package acp

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func validateBackboneStart(harness domain.AgentHarness, binding *ports.ChatBackboneConfig, model, effort string, permissions ports.PermissionMode, servers []ports.ChatMCPServerConfig, directories []string) error {
	if binding == nil {
		return nil
	}
	if harness != domain.HarnessOpenCode || binding.Model == "" || binding.Model != model || binding.PolicyVersion == "" || len(binding.Binding) == 0 || binding.InstructorRole == "" || effort != binding.Effort || ports.NormalizePermissionMode(permissions) != ports.PermissionModeDefault || len(servers) != 0 || len(directories) != 0 {
		return fmt.Errorf("%w: governed OpenCode requires the pinned model, default permissions and no additional native inputs", ports.ErrChatConfigOptionInvalid)
	}
	return nil
}

// validateBackboneSelection uses provider-reported current values, never an
// acknowledged setter, as the final session/new or session/load selection gate.
func (c *conversation) validateBackboneSelection() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	model, mode, effort := false, false, c.backboneEffort == ""
	for _, option := range c.configOptions {
		if option.ID == "model" {
			model = option.CurrentReported && option.Current.Select == c.backboneModel
		}
		if option.ID == "effort" {
			effort = option.CurrentReported && option.Current.Select == c.backboneEffort
		}
		if option.ID == "mode" {
			mode = option.CurrentReported && option.Current.Select == "ao-default"
		}
	}
	if !model || !mode || !effort {
		return fmt.Errorf("%w: governed OpenCode did not report the pinned model and primary agent", ports.ErrChatConfigOptionInvalid)
	}
	return nil
}

func (c *conversation) configureBackboneSession(ctx context.Context, settings ports.ChatTurnSettings) error {
	if err := c.applyNativeTurnSettings(ctx, settings); err != nil {
		return err
	}
	return c.validateBackboneSelection()
}

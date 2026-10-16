package chat

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/backbone"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func cloneBackbone(binding *ports.ChatBackboneConfig) *ports.ChatBackboneConfig {
	if binding == nil {
		return nil
	}
	cloned := *binding
	cloned.Binding = bytes.Clone(binding.Binding)
	return &cloned
}

// SetBackboneValidator compares the controller's immutable admission with the
// current work policy at start and each queued dispatch. Installed before recovery.
func (s *Service) SetBackboneValidator(validator func(context.Context, domain.SessionID, *ports.ChatBackboneConfig) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validateBackbone = validator
	for _, controller := range s.ownerControllers {
		controller.mu.Lock()
		controller.validateBackbone = validator
		controller.mu.Unlock()
	}
}

func validateBackboneStart(ctx context.Context, cfg StartConfig) error {
	if cfg.Backbone == nil {
		return nil
	}
	if cfg.Harness != domain.HarnessOpenCode || cfg.ReadOnly || ports.NormalizePermissionMode(cfg.Permissions) != ports.PermissionModeDefault || cfg.Model != cfg.Backbone.Model || cfg.Effort != cfg.Backbone.Effort || len(cfg.AdditionalDirectories) != 0 || len(cfg.MCPServers) != 0 {
		return fmt.Errorf("governed OpenCode requires its exact Chat model/default permission/workspace contract")
	}
	binding, err := backbone.ValidateBinding(cfg.Backbone.Binding, nil)
	if err != nil {
		return err
	}
	verified, err := backbone.Verify(ctx, cfg.WorkspacePath, binding, cfg.Backbone.InstructorRole)
	if err != nil {
		return err
	}
	if verified.Prompt == "" || !strings.HasSuffix(cfg.SystemPrompt, verified.Prompt) {
		return fmt.Errorf("governed OpenCode standing prompt does not contain verified context")
	}
	return nil
}

func (c *Controller) backboneImmutable() error {
	if c.backbone != nil {
		return fmt.Errorf("%w: governed OpenCode configuration is immutable; replace its controlled process", ports.ErrChatConfigOptionInvalid)
	}
	return nil
}

func (c *Controller) validateBackboneDispatch(ctx context.Context) error {
	c.mu.Lock()
	validator := c.validateBackbone
	c.mu.Unlock()
	if validator != nil {
		if err := validator(ctx, c.sessionID, c.backbone); err != nil {
			return err
		}
	}
	if c.backbone == nil {
		return nil
	}
	binding, err := backbone.ValidateBinding(c.backbone.Binding, nil)
	if err != nil {
		return err
	}
	_, err = backbone.Verify(ctx, c.backboneWorkspace, binding, c.backbone.InstructorRole)
	return err
}

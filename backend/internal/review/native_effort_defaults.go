package review

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// NativeModelCatalog is the launch-fresh harness catalog, scoped to the project.
type NativeModelCatalog interface {
	Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error)
}

// SetModelCatalog validates resolved reviewer models and efforts against the
// project-scoped native catalog at launch and restore.
func (e *Engine) SetModelCatalog(catalog NativeModelCatalog) { e.modelCatalog = catalog }

func reviewerRuntimeConfig(harness domain.ReviewerHarness, config domain.AgentConfig) domain.AgentConfig {
	if config.Effort == "" {
		if effort, ok := config.NativeEffort(string(harness), config.Model); ok {
			config.Effort = effort
		}
	}
	config.NativeEffortDefaults = nil
	return config
}

func (e *Engine) resolveNativeReviewerConfig(ctx context.Context, harness domain.ReviewerHarness, projectID domain.ProjectID, config domain.AgentConfig) (domain.AgentConfig, error) {
	profiled := config.NativeEffortDefaults != nil && len(*config.NativeEffortDefaults) > 0
	resolved := reviewerRuntimeConfig(harness, config)
	if profiled && resolved.Effort == "" {
		return domain.AgentConfig{}, fmt.Errorf("%w: choose an explicit native effort for reviewer harness %q model %q", ports.ErrUnsupportedEffort, harness, resolved.Model)
	}
	if resolved.Effort == "" || (harness != domain.ReviewerCodex && harness != domain.ReviewerOpenCode && harness != domain.ReviewerClaudeCode) {
		return resolved, nil
	}
	if e.modelCatalog == nil {
		if profiled {
			return domain.AgentConfig{}, fmt.Errorf("%w: reviewer native effort catalog unavailable", ports.ErrModelCapabilitiesUnavailable)
		}
		return resolved, nil
	}
	catalog, err := e.modelCatalog.Models(ctx, string(harness), string(projectID), true)
	if err != nil || catalog.Stale {
		return domain.AgentConfig{}, fmt.Errorf("%w: reviewer native effort catalog is unavailable or stale", ports.ErrModelCapabilitiesUnavailable)
	}
	modelID := resolved.Model
	if modelID == "" {
		for _, model := range catalog.Models {
			if !model.IsDefault {
				continue
			}
			if modelID != "" && modelID != model.ID {
				return domain.AgentConfig{}, fmt.Errorf("%w: reviewer provider default model is ambiguous", ports.ErrModelCapabilitiesUnavailable)
			}
			modelID = model.ID
		}
	}
	for _, model := range catalog.Models {
		if model.ID != modelID {
			continue
		}
		if model.EffortSupport == "unknown" {
			return domain.AgentConfig{}, fmt.Errorf("%w: reviewer native efforts were not reported", ports.ErrModelCapabilitiesUnavailable)
		}
		for _, effort := range model.Efforts {
			if effort == resolved.Effort {
				return resolved, nil
			}
		}
		return domain.AgentConfig{}, fmt.Errorf("%w %q for reviewer model %q", ports.ErrUnsupportedEffort, resolved.Effort, resolved.Model)
	}
	return domain.AgentConfig{}, fmt.Errorf("%w: reviewer model %q is not in the active catalog", ports.ErrModelCapabilitiesUnavailable, resolved.Model)
}

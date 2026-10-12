package sessionmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (m *Manager) resolveAgentConfig(ctx context.Context, cfg ports.SpawnConfig, project domain.ProjectConfig) (ports.AgentConfig, error) {
	base := effectiveAgentConfig(cfg.Harness, cfg.Kind, project)
	requested := cfg.AgentConfig
	resolved := applySpawnAgentConfig(base, requested)
	preferences := domain.MergeNativeEffortDefaults(base.NativeEffortDefaults, requested.NativeEffortDefaults)
	// Preferences are project state, never part of a native launch identity or
	// provider-confirmed configuration. Runtime comparisons stay value based.
	resolved.NativeEffortDefaults = nil
	profiled := preferences != nil && len(*preferences) > 0
	defaultApplied := false
	modelChangedWithoutExplicitEffort := requested.Model != "" && requested.Model != base.Model &&
		!cfg.EffortOverride && requested.Effort == ""
	if cfg.EffortOverride {
		resolved.Effort = requested.Effort
	}
	if cfg.Harness != domain.HarnessCodex && cfg.Harness != domain.HarnessClaudeCode && cfg.Harness != domain.HarnessOpenCode {
		resolved.Effort = ""
		return resolved, nil
	}
	modelID := strings.TrimSpace(resolved.Model)
	if profiled && !cfg.EffortOverride && requested.Effort == "" && (modelChangedWithoutExplicitEffort || resolved.Effort == "") {
		choice := domain.AgentConfig{NativeEffortDefaults: preferences}
		effort, ok := choice.NativeEffort(string(cfg.Harness), modelID)
		if !ok {
			return ports.AgentConfig{}, fmt.Errorf("%w: choose an explicit native effort for harness %q model %q", ports.ErrUnsupportedEffort, cfg.Harness, modelID)
		}
		resolved.Effort, defaultApplied = effort, true
	}
	validateClaudeModel := cfg.Harness == domain.HarnessClaudeCode && modelID != ""
	if resolved.Effort == "" && !validateClaudeModel {
		return resolved, nil
	}
	if m.modelCatalog == nil {
		if profiled || validateClaudeModel || (cfg.Harness == domain.HarnessOpenCode && resolved.Effort != "") {
			return ports.AgentConfig{}, fmt.Errorf("%w: model catalog is unavailable", ports.ErrModelCapabilitiesUnavailable)
		}
		return resolved, nil
	}
	catalog, err := m.modelCatalog.Models(ctx, string(cfg.Harness), string(cfg.ProjectID), true)
	if err != nil {
		if modelChangedWithoutExplicitEffort && !profiled && cfg.Harness != domain.HarnessOpenCode {
			resolved.Effort = ""
		}
		if validateClaudeModel || resolved.Effort != "" {
			return ports.AgentConfig{}, fmt.Errorf("%w: %w", ports.ErrModelCapabilitiesUnavailable, err)
		}
		return resolved, nil
	}
	if modelID == "" {
		if cfg.Harness == domain.HarnessOpenCode {
			var err error
			modelID, err = configuredOpenCodeModel(catalog)
			if err != nil {
				return ports.AgentConfig{}, err
			}
			resolved.Model = modelID
		} else {
			for _, item := range catalog.Models {
				if item.IsDefault {
					modelID = item.ID
					break
				}
			}
		}
	}
	if catalog.Stale && validateClaudeModel {
		return ports.AgentConfig{}, fmt.Errorf("%w for model %q: catalog is stale", ports.ErrModelCapabilitiesUnavailable, modelID)
	}
	var selected *ports.AgentModelInfo
	for i := range catalog.Models {
		if catalog.Models[i].ID == modelID {
			selected = &catalog.Models[i]
			break
		}
	}
	if validateClaudeModel && selected == nil {
		return ports.AgentConfig{}, fmt.Errorf("%w: model %q is not in the active provider catalog", ErrUnsupportedModel, modelID)
	}
	if modelChangedWithoutExplicitEffort && !defaultApplied && cfg.Harness != domain.HarnessOpenCode {
		if selected == nil || !containsString(selected.Efforts, base.Effort) {
			resolved.Effort = ""
		}
	}
	if resolved.Effort == "" {
		return resolved, nil
	}
	if catalog.Stale || selected == nil {
		return ports.AgentConfig{}, fmt.Errorf("%w for model %q", ports.ErrModelCapabilitiesUnavailable, modelID)
	}
	if cfg.Harness == domain.HarnessOpenCode && selected.EffortSupport == "unknown" {
		return ports.AgentConfig{}, fmt.Errorf("%w for model %q: native variants were not reported", ports.ErrModelCapabilitiesUnavailable, modelID)
	}
	if resolved.Effort != "" && !containsString(selected.Efforts, resolved.Effort) {
		return ports.AgentConfig{}, fmt.Errorf("%w %q for model %q", ports.ErrUnsupportedEffort, resolved.Effort, modelID)
	}
	return resolved, nil
}

func effectiveAgentConfig(harness domain.AgentHarness, kind domain.SessionKind, cfg domain.ProjectConfig) ports.AgentConfig {
	merged := cfg.AgentConfig
	role := roleOverride(kind, cfg)
	override := role.AgentConfig
	harnessMatches := role.Harness == "" || role.Harness == harness
	if harnessMatches {
		merged.NativeEffortDefaults = domain.MergeNativeEffortDefaults(merged.NativeEffortDefaults, override.NativeEffortDefaults)
	}
	if harnessMatches && override.Model != "" {
		if override.Model != merged.Model && override.Effort == "" && merged.NativeEffortDefaults != nil {
			merged.Effort = ""
		}
		merged.Model = override.Model
	}
	if harnessMatches && override.Effort != "" {
		merged.Effort = override.Effort
	}
	if harnessMatches && override.Mode != "" {
		merged.Mode = override.Mode
	}
	if override.Permissions != "" {
		merged.Permissions = override.Permissions
	}
	return merged
}

func restoredAgentConfig(rec domain.SessionRecord, cfg domain.ProjectConfig) ports.AgentConfig {
	merged := effectiveAgentConfig(rec.Harness, rec.Kind, cfg)
	merged.NativeEffortDefaults = nil
	if rec.Harness == domain.HarnessClaudeCode || rec.Harness == domain.HarnessOpenCode || rec.Harness == domain.HarnessCodex {
		merged.Model = rec.Metadata.Model
		merged.Effort = rec.Metadata.Effort
	}
	return merged
}

func (m *Manager) validateRestoredAgentConfig(ctx context.Context, rec domain.SessionRecord, cfg ports.AgentConfig) (ports.AgentConfig, error) {
	return m.resolveAgentConfig(ctx, ports.SpawnConfig{
		ProjectID: rec.ProjectID, Kind: rec.Kind, Harness: rec.Harness,
		AgentConfig: cfg, EffortOverride: true,
	}, domain.ProjectConfig{})
}

func applySpawnAgentConfig(base, override ports.AgentConfig) ports.AgentConfig {
	if override.Model != "" {
		base.Model = override.Model
	}
	if override.Effort != "" {
		base.Effort = override.Effort
	}
	if override.Mode != "" {
		base.Mode = override.Mode
	}
	if override.Permissions != "" {
		base.Permissions = override.Permissions
	}
	if base.Permissions == "" {
		base.Permissions = ports.PermissionModeAuto
	}
	return base
}

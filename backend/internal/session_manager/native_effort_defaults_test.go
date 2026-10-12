package sessionmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestResolveSavedNativeEffortDefaultsForModelOverride(t *testing.T) {
	for _, harness := range []domain.AgentHarness{domain.HarnessCodex, domain.HarnessOpenCode} {
		for _, kind := range []domain.SessionKind{domain.KindWorker, domain.KindOrchestrator} {
			t.Run(string(harness)+"/"+string(kind), func(t *testing.T) {
				defaults := domain.NativeEffortDefaults{string(harness): {"provider/a": "native-high", "provider/b": "contributor"}, "other-harness": {"provider/b": "minimal"}}
				role := domain.RoleOverride{Harness: harness, AgentConfig: domain.AgentConfig{Model: "provider/a", Effort: "native-high", NativeEffortDefaults: &defaults}}
				project := domain.ProjectConfig{Worker: role, Orchestrator: role}
				m := &Manager{modelCatalog: tuningCatalog{catalog: ports.AgentModelCatalog{Models: []ports.AgentModelInfo{{ID: "provider/a", Efforts: []string{"native-high"}, EffortSupport: "supported"}, {ID: "provider/b", Efforts: []string{"minimal", "contributor"}, EffortSupport: "supported"}}}}}
				config, err := m.resolveAgentConfig(context.Background(), ports.SpawnConfig{Harness: harness, Kind: kind, ProjectID: "p", AgentConfig: domain.AgentConfig{Model: "provider/b"}}, project)
				if err != nil || config.Effort != "contributor" || config.NativeEffortDefaults != nil {
					t.Fatalf("config=%+v err=%v", config, err)
				}
				_, err = m.resolveAgentConfig(context.Background(), ports.SpawnConfig{Harness: harness, Kind: kind, ProjectID: "p", AgentConfig: domain.AgentConfig{Model: "provider/unknown"}}, project)
				if !errors.Is(err, ports.ErrUnsupportedEffort) {
					t.Fatalf("unknown model silently inherited an effort: %v", err)
				}
				m.modelCatalog = tuningCatalog{catalog: ports.AgentModelCatalog{Stale: true}}
				_, err = m.resolveAgentConfig(context.Background(), ports.SpawnConfig{Harness: harness, Kind: kind, ProjectID: "p", AgentConfig: domain.AgentConfig{Model: "provider/b"}}, project)
				if !errors.Is(err, ports.ErrModelCapabilitiesUnavailable) {
					t.Fatalf("stale mapping was accepted: %v", err)
				}
			})
		}
	}
}

func TestRestoreKeepsNativeSelectionInsteadOfNewProjectDefaults(t *testing.T) {
	defaults := domain.NativeEffortDefaults{"opencode": {"provider/a": "minimal"}}
	project := domain.ProjectConfig{Worker: domain.RoleOverride{Harness: domain.HarnessOpenCode, AgentConfig: domain.AgentConfig{Model: "provider/a", Effort: "minimal", NativeEffortDefaults: &defaults}}}
	record := domain.SessionRecord{ID: "s", ProjectID: "p", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode}
	record.Metadata.Model, record.Metadata.Effort = "provider/a", "contributor"
	config := restoredAgentConfig(record, project)
	if config.Effort != "contributor" || config.NativeEffortDefaults != nil {
		t.Fatalf("restore applied project default: %+v", config)
	}
	m := &Manager{modelCatalog: tuningCatalog{catalog: ports.AgentModelCatalog{Models: []ports.AgentModelInfo{{ID: "provider/a", Efforts: []string{"minimal"}, EffortSupport: "supported"}}}}}
	if _, err := m.validateRestoredAgentConfig(context.Background(), record, config); !errors.Is(err, ports.ErrUnsupportedEffort) {
		t.Fatalf("unsupported pinned effort downgraded: %v", err)
	}
}

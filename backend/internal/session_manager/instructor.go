package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ErrInstructorUnavailable rejects an explicit governed launch when the feature is off.
var ErrInstructorUnavailable = errors.New("instructor governance is unavailable")

// Governance refusals are actionable conflicts, rather than opaque launch failures.
var (
	ErrInstructorResumeBlocked = errors.New("instructor governance blocks session recovery")
	ErrInstructorLaunchBlocked = errors.New("instructor governance blocks launch")
)

// InstructorGovernance is the optional managed-work boundary. It runs before
// an agent controller launches, including direct manager callers and recovery.
// Registering a work or attaching a controller does not imply work acceptance.
type InstructorGovernance interface {
	PrepareLaunch(context.Context, ports.SpawnConfig, domain.ProjectRecord) (string, error)
	AttachSession(context.Context, string, domain.SessionID, ports.SpawnConfig) error
	ValidateResume(context.Context, domain.SessionID) error
	SpawnFailed(context.Context, string, error) error
}

// SetInstructorGovernance is wired at daemon startup before any recovery or
// scheduler starts. A nil collaborator preserves the existing unmanaged mode.
func (m *Manager) SetInstructorGovernance(governance InstructorGovernance) {
	m.instructor = governance
}

func (m *Manager) validateInstructorResume(ctx context.Context, id domain.SessionID) error {
	if m.instructor == nil {
		return nil
	}
	if err := m.instructor.ValidateResume(ctx, id); err != nil {
		return fmt.Errorf("%w: %w", ErrInstructorResumeBlocked, err)
	}
	return nil
}

// Ordinary Chat recovery restores observation of the same native conversation.
// Native turn admission remains independently guarded immediately before send.
// TUI starts, switches and interface handoffs retain ValidateResume's execution gate.
func (m *Manager) validateInstructorControllerRecovery(ctx context.Context, id domain.SessionID) error {
	guard, supported := m.instructor.(interface {
		ValidateControllerRecovery(context.Context, domain.SessionID) error
	})
	if !supported {
		return m.validateInstructorResume(ctx, id)
	}
	rec, found, err := m.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if !found || domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeChat ||
		strings.TrimSpace(rec.Metadata.ProviderConversationID) == "" {
		return m.validateInstructorResume(ctx, id)
	}
	active, err := m.hasActiveInterfaceTransition(ctx, id)
	if err != nil {
		return err
	}
	if active {
		return m.validateInstructorResume(ctx, id)
	}
	if err := guard.ValidateControllerRecovery(ctx, id); err != nil {
		return fmt.Errorf("%w: %w", ErrInstructorResumeBlocked, err)
	}
	return nil
}

func (m *Manager) instructorControllerStarted(ctx context.Context, cfg ports.SpawnConfig, id domain.SessionID) error {
	if cfg.WorkID == "" {
		return nil
	}
	if observer, ok := m.instructor.(interface {
		ControllerStarted(context.Context, string, domain.SessionID, ports.SpawnConfig) error
	}); ok {
		if err := observer.ControllerStarted(ctx, cfg.WorkID, id, cfg); err != nil {
			// A bookkeeping failure after a real start must not leave invisible
			// autonomous work running. Preserve the workspace for recovery.
			cleanupCtx, cancel := spawnRollbackContext(ctx)
			defer cancel()
			record, found, readErr := m.store.GetSession(cleanupCtx, id)
			if readErr != nil {
				return fmt.Errorf("record launch: %w; reload controller: %w", err, readErr)
			}
			if found {
				if domain.NormalizeSessionMode(record.Mode) == domain.SessionModeChat {
					m.stopChatBestEffort(cleanupCtx, id)
				} else if record.Metadata.RuntimeHandleID != "" {
					if stopErr := m.runtime.Destroy(cleanupCtx, ports.RuntimeHandle{ID: record.Metadata.RuntimeHandleID}); stopErr != nil {
						return fmt.Errorf("record launch: %w; stop controller: %w", err, stopErr)
					}
				}
				m.markSpawnFailedTerminatedAfterFailure(cleanupCtx, id, false)
			}
			return err
		}
	}
	return nil
}

func (m *Manager) validateInstructorAgentChange(ctx context.Context, id domain.SessionID, harness domain.AgentHarness, config ports.AgentConfig) error {
	if guard, ok := m.instructor.(interface {
		ValidateAgentChange(context.Context, domain.SessionID, domain.AgentHarness, ports.AgentConfig) error
	}); ok {
		return guard.ValidateAgentChange(ctx, id, harness, config)
	}
	return nil
}

// ValidateInstructorAgentConfig validates a role's model-native choice at the
// actual adapter capability boundary. Native variants have no universal order:
// a maximum expression needs an explicit policy mapping for this exact model.
func (m *Manager) ValidateInstructorAgentConfig(ctx context.Context, projectID domain.ProjectID, role domain.InstructorRole, harness, modelID, effort string) error {
	if role.Harness != "" && role.Harness != harness {
		return fmt.Errorf("role requires harness %s, got %s", role.Harness, harness)
	}
	if role.Model != "" && role.Model != modelID {
		return fmt.Errorf("role requires model %s, got %s", role.Model, modelID)
	}
	if role.Effort == "" {
		return nil
	}
	if strings.TrimSpace(modelID) == "" || strings.TrimSpace(effort) == "" {
		return fmt.Errorf("%w: managed role needs an explicit model and native effort", ports.ErrUnsupportedEffort)
	}
	expected := role.Effort
	switch expected {
	case "configured":
		expected = effort
	case "maximum":
		expected = role.EffortByModel[modelID]
		if expected == "" {
			return fmt.Errorf("%w: policy has no approved maximum effort for model %s", ports.ErrUnsupportedEffort, modelID)
		}
	}
	if expected != effort {
		return fmt.Errorf("%w: model %s requires native effort %s, got %s", ports.ErrUnsupportedEffort, modelID, expected, effort)
	}
	if m.modelCatalog == nil {
		return ports.ErrModelCapabilitiesUnavailable
	}
	catalog, err := m.modelCatalog.Models(ctx, harness, string(projectID), true)
	if err != nil {
		return fmt.Errorf("%w: %w", ports.ErrModelCapabilitiesUnavailable, err)
	}
	if catalog.Stale {
		return fmt.Errorf("%w: current native effort metadata is required", ports.ErrModelCapabilitiesUnavailable)
	}
	for _, model := range catalog.Models {
		if model.ID != modelID {
			continue
		}
		if model.EffortSupport == "unknown" {
			return fmt.Errorf("%w: model %s effort capabilities are unknown", ports.ErrModelCapabilitiesUnavailable, modelID)
		}
		for _, supported := range model.Efforts {
			if supported == effort {
				return nil
			}
		}
		return fmt.Errorf("%w: model %s does not advertise effort %s", ports.ErrUnsupportedEffort, modelID, effort)
	}
	return fmt.Errorf("%w: model %s is absent from the native catalog", ErrUnsupportedModel, modelID)
}

package sessionmanager

import (
	"context"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (m *Manager) backbonePreflight(ctx context.Context, cfg ports.SpawnConfig, project domain.ProjectRecord, mode domain.SessionMode) (bool, error) {
	guard, ok := m.instructor.(interface {
		BackbonePreflight(context.Context, ports.SpawnConfig, domain.ProjectRecord, domain.SessionMode) (bool, error)
	})
	if !ok {
		return false, nil
	}
	return guard.BackbonePreflight(ctx, cfg, project, mode)
}
func (m *Manager) prepareBackboneController(ctx context.Context, id domain.SessionID, workspace string, mode domain.SessionMode, model string, permissions ports.PermissionMode) (*ports.ChatBackboneConfig, string, error) {
	guard, ok := m.instructor.(interface {
		PrepareBackboneController(context.Context, domain.SessionID, string, domain.SessionMode, string, ports.PermissionMode) (*ports.ChatBackboneConfig, string, error)
	})
	if !ok {
		return nil, "", nil
	}
	return guard.PrepareBackboneController(ctx, id, workspace, mode, model, permissions)
}

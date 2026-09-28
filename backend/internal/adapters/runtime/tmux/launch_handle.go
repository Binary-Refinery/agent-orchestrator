package tmux

import (
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (r *Runtime) LaunchHandles(id domain.SessionID) ([]ports.RuntimeHandle, error) {
	name, err := tmuxSessionName(id)
	if err != nil {
		return nil, err
	}
	return []ports.RuntimeHandle{{ID: name}}, nil
}

var _ ports.RuntimeLaunchHandleResolver = (*Runtime)(nil)

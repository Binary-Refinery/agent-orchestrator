package sessionmanager

import "github.com/aoagents/agent-orchestrator/backend/internal/domain"

// InstructorControllerReady is a live observation, never a persisted session
// status. In particular a retained Chat row is not proof of a live controller.
func (m *Manager) InstructorControllerReady(rec domain.SessionRecord) bool {
	if rec.IsTerminated || rec.Activity.State == domain.ActivityExited {
		return false
	}
	if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
		return m.chat != nil && m.chat.HasLiveChatController(rec.ID)
	}
	return m.SessionStatusReadiness(rec) == "ready"
}

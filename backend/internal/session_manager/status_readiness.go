package sessionmanager

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// A slow or unavailable provider must not leave the board loading forever.
// This is a verification deadline, not an artificial delay before publication.
const statusVerificationLimit = 30 * time.Second

type statusRecovery struct {
	pending    bool
	failed     *domain.SessionRecord
	reasonCode string
	reason     string
}

func (r statusRecovery) matchesFailure(rec domain.SessionRecord) bool {
	return !r.pending && r.failed != nil && !rec.IsTerminated &&
		rec.ControllerOwner() == r.failed.ControllerOwner() && rec.Activity == r.failed.Activity
}

// StatusRecoveryRevision fences API snapshots read concurrently with recovery.
func (m *Manager) StatusRecoveryRevision() uint64 {
	m.statusRecoveryMu.RLock()
	defer m.statusRecoveryMu.RUnlock()
	return m.statusRecoveryRevision
}

// SessionStatusReadiness describes this daemon's recovery observation. It is
// deliberately not durable: a new daemon must verify the sessions again.
func (m *Manager) SessionStatusReadiness(rec domain.SessionRecord) string {
	m.statusRecoveryMu.RLock()
	defer m.statusRecoveryMu.RUnlock()
	result, found := m.statusRecoveries[rec.ID]
	if found {
		// A deadline cancels the recovery context, but the dependency must return
		// before another attempt can safely own this session. Keep the UI checking
		// until the operation gate has actually been released.
		if result.pending {
			return "checking"
		}
		if result.matchesFailure(rec) && rec.Activity.State != domain.ActivityExited {
			return "unavailable"
		}
		return "ready"
	}
	select {
	case <-m.startupBackgroundReconcileDone:
		if m.statusRecoveryFailed {
			return "unavailable"
		}
		return "ready"
	default:
	}
	return "checking"
}

// SessionStatusRecoveryReason returns a bounded explanation for this daemon's
// failed recovery, never durable status or proof that the provider is dead.
func (m *Manager) SessionStatusRecoveryReason(rec domain.SessionRecord) (code, reason string) {
	m.statusRecoveryMu.RLock()
	defer m.statusRecoveryMu.RUnlock()
	if result, found := m.statusRecoveries[rec.ID]; found {
		if result.matchesFailure(rec) {
			return result.reasonCode, result.reason
		}
		return "", ""
	}
	select {
	case <-m.startupBackgroundReconcileDone:
		if m.statusRecoveryFailed {
			return "SESSION_RECOVERY_DISCOVERY_FAILED", "AO could not read the startup session inventory. Check daemon storage access and retry recovery."
		}
	default:
	}
	return "", ""
}

func (m *Manager) beginStatusRecovery(id domain.SessionID) {
	m.statusRecoveryMu.Lock()
	defer m.statusRecoveryMu.Unlock()
	m.statusRecoveries[id] = statusRecovery{pending: true}
	m.statusRecoveryRevision++
}

// markFreshSessionStatusReady keeps a session created by this daemon outside
// the startup snapshot's failure state. A concurrent startup recovery remains
// authoritative once it has already claimed the same session.
func (m *Manager) markFreshSessionStatusReady(id domain.SessionID) {
	m.statusRecoveryMu.Lock()
	defer m.statusRecoveryMu.Unlock()
	select {
	case <-m.startupBackgroundReconcileDone:
		// After a successful startup, an untracked session already reads as ready;
		// avoid turning ordinary spawns into recovery-revision changes.
		if !m.statusRecoveryFailed {
			return
		}
	default:
	}
	if result, found := m.statusRecoveries[id]; found && result.pending {
		return
	}
	m.statusRecoveries[id] = statusRecovery{}
	m.statusRecoveryRevision++
}

func (m *Manager) finishStatusRecovery(ctx context.Context, before domain.SessionRecord, recoveryErr error) {
	result := statusRecovery{}
	if recoveryErr != nil {
		current, found, err := m.store.GetSession(ctx, before.ID)
		if err == nil && found && current.ControllerOwner() == before.ControllerOwner() {
			before = current
		}
		result.failed = &before
		result.reasonCode, result.reason = statusRecoveryReason(recoveryErr)
	}
	m.statusRecoveryMu.Lock()
	m.statusRecoveries[before.ID] = result
	m.statusRecoveryRevision++
	m.statusRecoveryMu.Unlock()
}

func statusRecoveryReason(err error) (code, reason string) {
	code = "SESSION_RECOVERY_FAILED"
	prefix := "Session recovery could not finish. Check the reported cause before retrying. "
	switch {
	case errors.Is(err, ports.ErrChatRecoveryInconclusive):
		code = "CHAT_RECOVERY_INCONCLUSIVE"
		prefix = "The existing provider host may still own work. Verify its connection before retrying; do not start a duplicate controller. "
	case errors.Is(err, ports.ErrUnsupportedEffort):
		code = "UNSUPPORTED_EFFORT"
		prefix = "Review the project's model and native effort selection. "
	case errors.Is(err, ports.ErrModelCapabilitiesUnavailable):
		code = "MODEL_CAPABILITIES_UNAVAILABLE"
		prefix = "Model capabilities could not be verified. Refresh the provider model catalog before retrying. "
	case errors.Is(err, ErrUnsupportedModel):
		code = "UNSUPPORTED_MODEL"
		prefix = "Review the project's selected model before retrying recovery. "
	case errors.Is(err, ErrInstructorResumeBlocked):
		code = "INSTRUCTOR_RESUME_BLOCKED"
		prefix = "Resolve the work gate or project role configuration before retrying recovery. "
	case errors.Is(err, ports.ErrChatResumeFailed):
		code = "CHAT_RESUME_FAILED"
		prefix = "The saved provider conversation could not be resumed. Check the provider connection. "
	case errors.Is(err, context.DeadlineExceeded):
		code = "SESSION_RECOVERY_TIMEOUT"
		prefix = "Recovery verification timed out. Check the provider connection and retry. "
	}
	// Driver errors may contain long output or line breaks. Keep the board's
	// explanation compact without splitting multibyte characters.
	runes := []rune(prefix + strings.Join(strings.Fields(err.Error()), " "))
	const maxReasonRunes = 512
	if len(runes) > maxReasonRunes {
		runes = append(runes[:maxReasonRunes-1], '…')
	}
	return code, string(runes)
}

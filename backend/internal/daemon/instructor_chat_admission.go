package daemon

import (
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

// instructorChatAdmissionError separates known governance denials from provider
// and persistence failures before their reason enters a durable Chat timeline.
func instructorChatAdmissionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, instructorsvc.ErrEvidencePending) || errors.Is(err, instructorsvc.ErrExecutionDeferred) {
		return fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryDeferred, err)
	}
	reason := "Work admission could not be verified. Check controller readiness and refresh the work before assigning a task again."
	switch {
	case errors.Is(err, ports.ErrModelCapabilitiesUnavailable):
		reason = "Current native model capabilities could not be verified. Refresh provider discovery and check the project's model and effort settings."
	case errors.Is(err, instructorsvc.ErrValidation), errors.Is(err, ports.ErrUnsupportedEffort):
		// These denials are produced by fixed policy/selection checks; native
		// catalog errors carry ErrModelCapabilitiesUnavailable and stay above.
		reason = fmt.Sprintf("%.2048s", err.Error())
	case errors.Is(err, sessionmanager.ErrUnsupportedModel):
		reason = "The selected model is absent from the native catalog. Check the project's model settings."
	case errors.Is(err, instructorsvc.ErrConflict):
		reason = instructorsvc.ErrConflict.Error()
	case errors.Is(err, instructorsvc.ErrNotFound):
		reason = instructorsvc.ErrNotFound.Error()
	}
	return &ports.ChatAdmissionError{Reason: reason}
}

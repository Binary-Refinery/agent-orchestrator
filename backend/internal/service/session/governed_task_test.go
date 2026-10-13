package session

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

func TestGovernedTaskRequiredIsActionableInputError(t *testing.T) {
	err := toSpawnAPIError(fmt.Errorf("spawn: %w: %w", sessionmanager.ErrInstructorLaunchBlocked, ports.ErrGovernedTaskRequired))
	var apiError *apierr.Error
	if !errors.As(err, &apiError) || apiError.Kind != apierr.KindInvalid || apiError.Code != "GOVERNED_TASK_REQUIRED" || !strings.Contains(apiError.Message, "concrete task prompt or issue") {
		t.Fatalf("missing-task error lost its corrective action: %v", err)
	}
}

package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

func TestRestoreAndResumeExposeActionableRecoveryErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		code   string
		kind   apierr.Kind
		detail string
	}{
		{"role mismatch", fmt.Errorf("%w: role requires harness opencode, got codex", sessionmanager.ErrInstructorResumeBlocked), "INSTRUCTOR_RESUME_BLOCKED", apierr.KindConflict, "role requires harness opencode, got codex"},
		{"paused work", fmt.Errorf("%w: work is waiting_human", sessionmanager.ErrInstructorResumeBlocked), "INSTRUCTOR_RESUME_BLOCKED", apierr.KindConflict, "work is waiting_human"},
		{"missing model", fmt.Errorf("%w: %w: managed role needs an explicit model and native effort", sessionmanager.ErrInstructorResumeBlocked, ports.ErrUnsupportedEffort), "UNSUPPORTED_EFFORT", apierr.KindInvalid, "explicit model"},
		{"unknown host", fmt.Errorf("%w: host connection unavailable", ports.ErrChatRecoveryInconclusive), "CHAT_RECOVERY_INCONCLUSIVE", apierr.KindConflict, "host connection unavailable"},
		{"history and unknown host", errors.Join(ports.ErrChatHistoryUnsettled, ports.ErrChatRecoveryInconclusive), "CHAT_RECOVERY_INCONCLUSIVE", apierr.KindConflict, "cannot be started safely"},
		{"provider resume failed", fmt.Errorf("%w: conversation unavailable", ports.ErrChatResumeFailed), "CHAT_RESUME_FAILED", apierr.KindConflict, "conversation unavailable"},
		{"provider history load failed", fmt.Errorf("%w: transcript load rejected", ports.ErrChatHistoryLoadFailed), "CHAT_HISTORY_LOAD_FAILED", apierr.KindConflict, "transcript load rejected"},
		{"stale recovery plan", domain.ErrSessionRecoveryChanged, "SESSION_RECOVERY_CHANGED", apierr.KindConflict, "refresh and try again"},
	} {
		for _, operation := range []string{"restore", "resume"} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				svc := NewWithDeps(Deps{Manager: &fakeCommander{restoreErr: tc.err}, Store: newFakeStore()})
				var err error
				if operation == "restore" {
					_, err = svc.Restore(context.Background(), "s1")
				} else {
					_, err = svc.ResumeAgent(context.Background(), "s1")
				}
				var mapped *apierr.Error
				if !errors.As(err, &mapped) || mapped.Code != tc.code || mapped.Kind != tc.kind {
					t.Fatalf("error = %v, want kind %d code %s", err, tc.kind, tc.code)
				}
				if !strings.Contains(mapped.Message, tc.detail) {
					t.Fatalf("message %q omitted %q", mapped.Message, tc.detail)
				}
			})
		}
	}
}

func TestSpawnGovernanceRejectionKeepsCauseAndSpecificValidationCode(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{fmt.Errorf("spawn: %w: role requires harness opencode, got codex", sessionmanager.ErrInstructorLaunchBlocked), "INSTRUCTOR_LAUNCH_BLOCKED"},
		{fmt.Errorf("spawn: %w: %w", sessionmanager.ErrInstructorLaunchBlocked, ports.ErrUnsupportedEffort), "UNSUPPORTED_EFFORT"},
	} {
		mapped := toSpawnAPIError(tc.err)
		var apiError *apierr.Error
		if !errors.As(mapped, &apiError) || apiError.Code != tc.code {
			t.Fatalf("error = %v, want %s", mapped, tc.code)
		}
		if !strings.Contains(apiError.Message, tc.err.Error()) {
			t.Fatalf("message %q omitted original cause", apiError.Message)
		}
	}
}

func TestSendExplainsDeferredAndRejectedGovernedExecution(t *testing.T) {
	for _, tc := range []struct {
		cause  error
		code   string
		detail string
	}{
		{ports.ErrSemanticDeliveryDeferred, "CHAT_EXECUTION_DEFERRED", "saved in the queue"},
		{ports.ErrSemanticDeliveryRejected, "CHAT_EXECUTION_REJECTED", "cancelled before provider dispatch"},
		{fmt.Errorf("%w: role requires harness opencode, got codex", ports.ErrSemanticDeliveryRejected), "CHAT_EXECUTION_REJECTED", "role requires harness opencode, got codex"},
	} {
		for _, withOptions := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/options=%t", tc.code, withOptions), func(t *testing.T) {
				svc := NewWithDeps(Deps{Manager: &fakeCommander{sendErr: fmt.Errorf("send s1: %w", tc.cause)}, Store: newFakeStore()})
				var err error
				if withOptions {
					err = svc.SendWithOptions(context.Background(), "s1", "Continue", nil, ports.MessageDeliveryOptions{})
				} else {
					err = svc.Send(context.Background(), "s1", "Continue", nil)
				}
				var mapped *apierr.Error
				if !errors.As(err, &mapped) || mapped.Kind != apierr.KindConflict || mapped.Code != tc.code || !strings.Contains(mapped.Message, tc.detail) {
					t.Fatalf("error = %v, want 409 %s explaining %q", err, tc.code, tc.detail)
				}
			})
		}
	}
}

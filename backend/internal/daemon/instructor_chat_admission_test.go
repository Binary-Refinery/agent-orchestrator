package daemon

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

func TestInstructorChatAdmissionError(t *testing.T) {
	if got := instructorChatAdmissionError(nil); got != nil {
		t.Fatalf("successful admission became an error: %v", got)
	}
	const privateToken = "private-native-token-do-not-display"
	const roleReason = "role requires harness opencode, got codex"
	const capabilitiesReason = "Current native model capabilities could not be verified. Refresh provider discovery and check the project's model and effort settings."
	const unexpectedReason = "Work admission could not be verified. Check controller readiness and refresh the work before assigning a task again."
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "known role validation stays actionable",
			err:  fmt.Errorf("%w: %s", instructorsvc.ErrValidation, roleReason),
			want: instructorsvc.ErrValidation.Error() + ": " + roleReason,
		},
		{
			name: "native capability failure hides provider output",
			err:  fmt.Errorf("%w: %s", ports.ErrModelCapabilitiesUnavailable, privateToken),
			want: capabilitiesReason,
		},
		{
			name: "capability failure wins over nested unsupported effort",
			err:  fmt.Errorf("%w: %w", ports.ErrModelCapabilitiesUnavailable, fmt.Errorf("%w: %s", ports.ErrUnsupportedEffort, privateToken)),
			want: capabilitiesReason,
		},
		{
			name: "capability failure wins over outer validation",
			err:  fmt.Errorf("%w: %w: %s", instructorsvc.ErrValidation, ports.ErrModelCapabilitiesUnavailable, privateToken),
			want: capabilitiesReason,
		},
		{
			name: "unexpected store failure hides private data",
			err:  fmt.Errorf("read admission record: %s", privateToken),
			want: unexpectedReason,
		},
		{
			name: "unexpected native failure hides private data",
			err:  fmt.Errorf("provider model lookup stderr: %s", privateToken),
			want: unexpectedReason,
		},
		{
			name: "conflict hides nested details",
			err:  fmt.Errorf("%w: %s", instructorsvc.ErrConflict, privateToken),
			want: instructorsvc.ErrConflict.Error(),
		},
		{
			name: "unsupported model has static guidance",
			err:  fmt.Errorf("%w: %s", sessionmanager.ErrUnsupportedModel, privateToken),
			want: "The selected model is absent from the native catalog. Check the project's model settings.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := instructorChatAdmissionError(tc.err)
			var admission *ports.ChatAdmissionError
			if !errors.As(got, &admission) {
				t.Fatalf("admission denial is not safe for the durable timeline: %T", got)
			}
			if admission.Reason != tc.want || got.Error() != tc.want {
				t.Fatalf("actionable reason = %q, want %q", admission.Reason, tc.want)
			}
			if strings.Contains(got.Error(), privateToken) {
				t.Fatal("private error details escaped into the admission reason")
			}
			if errors.Is(got, ports.ErrSemanticDeliveryDeferred) {
				t.Fatal("terminal admission denial became a deferred queue item")
			}
		})
	}
}

func TestInstructorChatAdmissionErrorBoundsValidationReason(t *testing.T) {
	const actionable = "role requires harness opencode, got codex: "
	err := fmt.Errorf("%w: %s%s", instructorsvc.ErrValidation, actionable, strings.Repeat("ü", 4096))
	got := instructorChatAdmissionError(err)
	var admission *ports.ChatAdmissionError
	if !errors.As(got, &admission) {
		t.Fatalf("expected safe admission reason: %T", got)
	}
	if !strings.HasPrefix(admission.Reason, instructorsvc.ErrValidation.Error()+": "+actionable) {
		t.Fatal("bounded reason lost actionable role mismatch")
	}
	if !utf8.ValidString(admission.Reason) || utf8.RuneCountInString(admission.Reason) != 2048 {
		t.Fatalf("reason is not bounded valid UTF-8: runes=%d", utf8.RuneCountInString(admission.Reason))
	}
}

func TestInstructorChatAdmissionErrorPreservesDeferredExecution(t *testing.T) {
	for _, cause := range []error{instructorsvc.ErrEvidencePending, instructorsvc.ErrExecutionDeferred} {
		t.Run(cause.Error(), func(t *testing.T) {
			got := instructorChatAdmissionError(fmt.Errorf("dispatch admission: %w", cause))
			if !errors.Is(got, ports.ErrSemanticDeliveryDeferred) || !errors.Is(got, cause) {
				t.Fatalf("deferred execution lost its queue-preserving identity: %v", got)
			}
			var admission *ports.ChatAdmissionError
			if errors.As(got, &admission) {
				t.Fatal("deferred execution became a terminal admission rejection")
			}
		})
	}
}

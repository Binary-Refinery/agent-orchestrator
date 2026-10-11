package acp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestNativeACPReadOnlyRefusesBeforeLaunchingOrAdoptingProvider(t *testing.T) {
	for _, harness := range []domain.AgentHarness{domain.HarnessOpenCode, domain.HarnessClaudeCode} {
		t.Run(string(harness), func(t *testing.T) {
			launches := 0
			driver := New(Config{Harness: harness, Launch: func(context.Context, LaunchConfig) (Launch, error) {
				launches++
				return Launch{}, errors.New("must not launch")
			}}, nil)
			_, startErr := driver.Start(context.Background(), ports.ChatStartConfig{WorkspacePath: t.TempDir(), ReadOnly: true})
			_, resumeErr := driver.Resume(context.Background(), ports.ChatResumeConfig{WorkspacePath: t.TempDir(), ProviderConversationID: "old-writable-native", ReadOnly: true})
			for _, err := range []error{startErr, resumeErr} {
				if !errors.Is(err, ports.ErrChatConfigOptionInvalid) || !strings.Contains(err.Error(), "enforceable read-only sandbox") {
					t.Fatalf("unsupported sandbox was silently ignored: %v", err)
				}
			}
			if launches != 0 {
				t.Fatalf("read-only request reached provider: %d", launches)
			}
		})
	}
}

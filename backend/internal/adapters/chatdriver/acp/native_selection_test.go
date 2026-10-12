package acp

import (
	"context"
	"errors"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestRequiredNativeSelectionRefusesSuccessfulClampsBeforeStartResumeOrPrompt(t *testing.T) {
	for _, stage := range []string{"start", "resume", "turn"} {
		for _, choice := range []string{"model", "effort"} {
			t.Run(stage+"/"+choice, func(t *testing.T) {
				ctx := context.Background()
				agent := &fakeAgent{newConfig: []acpsdk.SessionConfigOption{selectConfigOption(choice, choice, choice, "low", "low", "required")}, setConfig: []acpsdk.SessionConfigOption{selectConfigOption(choice, choice, choice, "low", "low", "required")}}
				driver := New(Config{Harness: domain.HarnessOpenCode, RequireSessionOptions: true, Probe: func(context.Context) error { return nil }, Launch: func(context.Context, LaunchConfig) (Launch, error) { return Launch{Command: "fake"}, nil }, SessionOptions: func(settings ports.ChatTurnSettings) []SessionOption {
					value := settings.Effort
					if choice == "model" {
						value = settings.Model
					}
					return []SessionOption{{ID: choice, Value: value}}
				}}, nil)
				driver.useTestProcess(fakeSpawn(agent))
				settings := ports.ChatTurnSettings{}
				if choice == "model" {
					settings.Model = "required"
				} else {
					settings.Effort = "required"
				}
				var err error
				switch stage {
				case "start":
					_, err = driver.Start(ctx, ports.ChatStartConfig{WorkspacePath: t.TempDir(), Model: settings.Model, Effort: settings.Effort})
				case "resume":
					_, err = driver.Resume(ctx, ports.ChatResumeConfig{WorkspacePath: t.TempDir(), ProviderConversationID: "s1", Model: settings.Model, Effort: settings.Effort})
				case "turn":
					conv, startErr := driver.Start(ctx, ports.ChatStartConfig{WorkspacePath: t.TempDir()})
					if startErr != nil {
						t.Fatal(startErr)
					}
					defer conv.Close()
					_, err = conv.SendTurn(ctx, ports.ChatUserMessage{Text: "must not reach inference", Settings: settings})
				}
				if !errors.Is(err, ports.ErrChatConfigOptionInvalid) {
					t.Fatalf("successful native clamp silently accepted: %v", err)
				}
				agent.mu.Lock()
				promptSession := agent.promptParams.SessionId
				agent.mu.Unlock()
				if promptSession != "" {
					t.Fatalf("clamped native choice reached inference: %s", promptSession)
				}
			})
		}
	}
}

func TestRequiredNativeSelectionRequiresActualReadbackAndAcceptsNativeNames(t *testing.T) {
	for _, reported := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing readback refuses", true: "contributor readback accepted"}[reported], func(t *testing.T) {
			agent := &fakeAgent{}
			if reported {
				agent.setConfig = []acpsdk.SessionConfigOption{selectConfigOption("effort", "Effort", "thought_level", "contributor", "contributor")}
			}
			driver := New(Config{Harness: domain.HarnessOpenCode, RequireSessionOptions: true, Probe: func(context.Context) error { return nil }, Launch: func(context.Context, LaunchConfig) (Launch, error) { return Launch{Command: "fake"}, nil }, SessionOptions: func(settings ports.ChatTurnSettings) []SessionOption {
				return []SessionOption{{ID: "effort", Value: settings.Effort}}
			}}, nil)
			driver.useTestProcess(fakeSpawn(agent))
			conv, err := driver.Start(context.Background(), ports.ChatStartConfig{WorkspacePath: t.TempDir(), Effort: "contributor"})
			if reported {
				if err != nil {
					t.Fatal(err)
				}
				_ = conv.Close()
			} else if !errors.Is(err, ports.ErrChatConfigOptionInvalid) {
				t.Fatalf("unknown native result claimed accepted: %v", err)
			}
		})
	}
}

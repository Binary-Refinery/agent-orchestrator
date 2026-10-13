package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/persistenthost"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestLiveACPRequiredSettingsWaitForOwnedReplayReader(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		active     bool
		turnID     string
		clamp      bool
		unanswered bool
		wantErr    error
		wantCalls  int
	}{
		{name: "idle OpenCode", wantCalls: 2},
		{name: "active output keeps durable turn", active: true, turnID: "durable-turn", wantCalls: 2},
		{name: "native clamp still refused", clamp: true, wantErr: ports.ErrChatConfigOptionInvalid, wantCalls: 1},
		{name: "unanswered setter is bounded", unanswered: true, wantErr: ports.ErrChatResumeFailed, wantCalls: 1},
		{name: "missing ownership keeps gate closed", active: true, wantErr: ports.ErrChatRecoveryInconclusive},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			initialize, err := json.Marshal(acpsdk.InitializeResponse{ProtocolVersion: acpsdk.ProtocolVersionNumber})
			if err != nil {
				t.Fatal(err)
			}
			options := []acpsdk.SessionConfigOption{
				selectConfigOption("model", "Model", "model", "provider/worker", "provider/worker"),
				selectConfigOption("effort", "Effort", "thought_level", "high", "high", "low"),
			}
			session, err := json.Marshal(acpsdk.NewSessionResponse{SessionId: "same-native-session", ConfigOptions: options})
			if err != nil {
				t.Fatal(err)
			}
			daemon, host := net.Pipe()
			var mu sync.Mutex
			var methods []string
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				if scenario.active {
					// Host replay is ready before Resume returns. It must not be
					// consumed until activation installs the durable turn identity.
					if _, err := fmt.Fprintln(host, `{"jsonrpc":"2.0","method":"session/update","params":{"_meta":{"ao.persistentEventId":"acp-host:owned-replay"},"sessionId":"same-native-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"retained native output"}}}}`); err != nil {
						return
					}
				}
				decoder, encoder := json.NewDecoder(host), json.NewEncoder(host)
				for {
					var request struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
					}
					if err := decoder.Decode(&request); err != nil {
						return
					}
					mu.Lock()
					methods = append(methods, request.Method)
					mu.Unlock()
					if scenario.unanswered {
						continue
					}
					result := options
					if scenario.clamp {
						result = []acpsdk.SessionConfigOption{selectConfigOption("model", "Model", "model", "different", "different", "provider/worker")}
					}
					if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": acpsdk.SetSessionConfigOptionResponse{ConfigOptions: result}}); err != nil {
						return
					}
				}
			}()
			t.Cleanup(func() {
				_ = daemon.Close()
				_ = host.Close()
				select {
				case <-serverDone:
				case <-time.After(time.Second):
					t.Error("protocol fixture did not stop")
				}
			})
			driver := New(Config{
				Harness: domain.HarnessOpenCode, RequireSessionOptions: true,
				Launch: func(context.Context, LaunchConfig) (Launch, error) {
					return Launch{Command: "unused-live-provider"}, nil
				},
				SessionOptions: func(settings ports.ChatTurnSettings) []SessionOption {
					return []SessionOption{{ID: "model", Value: settings.Model}, {ID: "effort", Value: settings.Effort}}
				},
			}, nil)
			driver.connectHost = func(context.Context, persistenthost.Config) (*persistenthost.Transport, error) {
				return &persistenthost.Transport{Stdin: daemon, Stdout: daemon, Reconnected: true, ACPState: &persistenthost.ACPState{
					InitializeResult: initialize, SessionResult: session, SessionID: "same-native-session", ActivePrompt: scenario.active,
				}}, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			opened, err := driver.Resume(ctx, ports.ChatResumeConfig{
				SessionID: "retained-worker", DataDir: t.TempDir(), WorkspacePath: t.TempDir(), ProviderScopeID: "retained-scope",
				ProviderConversationID: "same-native-session", Model: "provider/worker", Effort: "high",
			})
			if err != nil {
				t.Fatalf("Resume blocked on a native response before replay activation: %v", err)
			}
			conv := opened.(*conversation)
			t.Cleanup(func() { _ = conv.Close() })
			mu.Lock()
			before := len(methods)
			mu.Unlock()
			if before != 0 || conv.ProviderConversationID() != "same-native-session" {
				t.Fatalf("reconnect wrote setup/settings before durable ownership: calls=%d session=%s", before, conv.ProviderConversationID())
			}
			_ = nextEvent(t, conv.Events()) // Cached setup only; no replay yet.
			select {
			case event := <-conv.Events():
				t.Fatalf("native replay escaped ownership gate: %+v", event)
			default:
			}
			activationCtx := ctx
			if scenario.unanswered {
				var stop context.CancelFunc
				activationCtx, stop = context.WithTimeout(ctx, 50*time.Millisecond)
				defer stop()
			}
			err = conv.ActivateLiveReconnect(activationCtx, scenario.turnID)
			if scenario.wantErr != nil {
				if !errors.Is(err, scenario.wantErr) {
					t.Fatalf("activation error = %v, want %v", err, scenario.wantErr)
				}
			} else if err != nil {
				t.Fatalf("activation could not read its native settings response: %v", err)
			}
			if scenario.unanswered && !errors.Is(activationCtx.Err(), context.DeadlineExceeded) {
				t.Fatal("unanswered native setter did not terminate at its deadline")
			}
			mu.Lock()
			calls := append([]string(nil), methods...)
			mu.Unlock()
			if len(calls) != scenario.wantCalls {
				t.Fatalf("native settings calls = %v, want %d", calls, scenario.wantCalls)
			}
			for _, method := range calls {
				if method != "session/set_config_option" {
					t.Fatalf("reconnect started setup or native inference: %s", method)
				}
			}
			if scenario.active && scenario.turnID != "" {
				event := nextEvent(t, conv.Events())
				if event.Kind != ports.ChatEventMessageDelta || event.Delta != "retained native output" || event.ProviderTurnID != scenario.turnID || event.ProviderEventID != "acp-host:owned-replay:0" {
					t.Fatalf("settings activation lost durable replay correlation: %+v", event)
				}
			}
		})
	}
}

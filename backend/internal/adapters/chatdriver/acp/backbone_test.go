package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func backboneConversation() *conversation {
	c := &conversation{sessionID: "native-session", backboneModel: "provider/model", backboneEffort: "high"}
	for key, value := range map[string]string{"model": "provider/model", "mode": "ao-default", "effort": "high"} {
		c.configOptions = append(c.configOptions, ports.ChatConfigOption{ID: key, CurrentReported: true, Current: ports.ChatConfigOptionValue{Select: value}})
	}
	return c
}
func TestBackboneRefusesMutableNativeSettings(t *testing.T) {
	c := backboneConversation()
	for _, settings := range []ports.ChatTurnSettings{{Model: "other/model"}, {Effort: "low"}, {Approval: ports.PermissionModeBypassPermissions}} {
		if err := c.applyTurnSettings(context.Background(), settings); !errors.Is(err, ports.ErrChatConfigOptionInvalid) {
			t.Fatalf("settings reached native transport: %v", err)
		}
	}
	if _, err := c.SetConfigOption(context.Background(), "model", ports.ChatConfigOptionValue{Select: "provider/model"}); !errors.Is(err, ports.ErrChatConfigOptionInvalid) {
		t.Fatalf("native setter accepted: %v", err)
	}
	if _, err := c.Compact(context.Background()); !errors.Is(err, ports.ErrChatConfigOptionInvalid) {
		t.Fatalf("unqualified compaction accepted: %v", err)
	}
	if _, err := c.SendTurn(context.Background(), ports.ChatUserMessage{Text: "/review"}); !errors.Is(err, ports.ErrChatConfigOptionInvalid) {
		t.Fatalf("native slash command accepted: %v", err)
	}
	if err := c.applyTurnSettings(context.Background(), ports.ChatTurnSettings{Model: "provider/model", Effort: "high", Approval: ports.PermissionModeDefault}); err != nil {
		t.Fatal(err)
	}
	c.configOptions[0].CurrentReported = false
	if err := c.applyTurnSettings(context.Background(), ports.ChatTurnSettings{}); err == nil {
		t.Fatal("unreported native selection accepted")
	}
}
func TestBackboneRevalidatesDeferredDispatch(t *testing.T) {
	c := backboneConversation()
	forbidden := filepath.Join(t.TempDir(), "SKILL.md")
	refusal := errors.New("native source changed")
	c.proc = &process{validateBeforeSend: func(context.Context) error {
		if _, err := os.Stat(forbidden); err == nil {
			return refusal
		}
		return nil
	}}
	turn, err := c.SendTurn(context.Background(), ports.ChatUserMessage{Text: "implement the approved task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(forbidden, []byte("data-only new source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.StartDeferredTurn(turn.ProviderTurnID); !errors.Is(err, refusal) {
		t.Fatalf("deferred source change reached native dispatch: %v", err)
	}
	if c.prepared == nil || c.prepared.id != turn.ProviderTurnID || c.activeTurn != "" {
		t.Fatal("refused dispatch changed durable turn preparation")
	}
}
func TestBackboneRejectsLiveHostBeforeProtocol(t *testing.T) {
	stopped, terminated := false, false
	proc := &process{reconnected: true, stop: func() error { stopped = true; return nil }, terminate: func() error { terminated = true; return nil }}
	driver := New(Config{Harness: domain.HarnessOpenCode}, nil)
	_, _, _, err := driver.initialize(context.Background(), LaunchConfig{Backbone: &ports.ChatBackboneConfig{Model: "provider/model"}}, proc)
	if !errors.Is(err, ports.ErrChatRecoveryInconclusive) || !stopped || terminated {
		t.Fatalf("live provider was not safely detached: %v", err)
	}
}
func TestBackboneAdmissionLeavesUnboundSettingsUnchanged(t *testing.T) {
	if err := validateBackboneStart(domain.HarnessOpenCode, nil, "provider/model", "high", ports.PermissionModeBypassPermissions, nil, nil); err != nil {
		t.Fatal(err)
	}
	binding := &ports.ChatBackboneConfig{Binding: []byte(`{}`), InstructorRole: "worker", PolicyVersion: "approved", Model: "provider/model", Effort: "high"}
	if err := validateBackboneStart(domain.HarnessOpenCode, binding, "provider/model", "high", ports.PermissionModeDefault, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := validateBackboneStart(domain.HarnessOpenCode, binding, "provider/model", "high", ports.PermissionModeDefault, []ports.ChatMCPServerConfig{{Name: "unqualified"}}, nil); err == nil {
		t.Fatal("unqualified native MCP admitted")
	}
}

package codexappserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/codexappserver/codexproto"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestNativeExternalToolIsolationRequiresExplicitEmptyThreadInventories(t *testing.T) {
	for _, test := range []struct {
		name, mcp, apps string
		want            bool
	}{
		{"empty", `{"data":[],"nextCursor":null}`, `{"apps":[]}`, true},
		{"failed configured MCP", `{"data":[{"name":"failed","authStatus":"notLoggedIn","tools":{}}]}`, `{"apps":[]}`, false},
		{"missing MCP inventory", `{}`, `{"apps":[]}`, false},
		{"null MCP inventory", `{"data":null}`, `{"apps":[]}`, false},
		{"installed connector", `{"data":[]}`, `{"apps":[{"id":"external","name":"External","tools":[]}]}`, false},
		{"missing connector inventory", `{"data":[]}`, `{}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, srv := newTestDriver(t)
			srv.respondTo(codexproto.MethodMcpServerStatusList, test.mcp)
			srv.respondTo(codexproto.MethodAppInstalled, test.apps)
			conv, err := d.Start(context.Background(), ports.ChatStartConfig{WorkspacePath: t.TempDir(), ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conv.Close() }()
			err = conv.(ports.ChatExternalToolIsolationValidator).ValidateNoExternalTools(context.Background())
			if (err == nil) != test.want {
				t.Fatalf("isolation result=%v want success=%v", err, test.want)
			}
			srv.mu.Lock()
			defer srv.mu.Unlock()
			for _, req := range srv.seen {
				switch req.Method {
				case codexproto.MethodMcpServerStatusList:
					var params codexproto.ListMcpServerStatusParams
					if json.Unmarshal(req.Params, &params) != nil || params.ThreadID == nil || *params.ThreadID != "thread-1" {
						t.Fatal("MCP isolation used global inventory instead of actual thread")
					}
				case codexproto.MethodAppInstalled:
					var params codexproto.AppsInstalledParams
					if json.Unmarshal(req.Params, &params) != nil || params.ThreadID == nil || *params.ThreadID != "thread-1" || params.ForceRefresh == nil || *params.ForceRefresh {
						t.Fatal("connector isolation refreshed or inspected wrong thread")
					}
				case codexproto.MethodConfigMcpServerReload:
					t.Fatal("isolation mutated native tool configuration")
				}
			}
		})
	}
}

func TestNativeAgentSelectionReadbackUsesThreadOpenResponseOnly(t *testing.T) {
	for _, test := range []struct{ name, response, model, effort string }{
		{"reported", `{"thread":{"id":"thread-1"},"model":"actual-model","reasoningEffort":"high"}`, "actual-model", "high"},
		{"missing effort", `{"thread":{"id":"thread-1"},"model":"actual-model"}`, "actual-model", ""},
		{"unreported", `{"thread":{"id":"thread-1"}}`, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, srv := newTestDriver(t)
			srv.respondTo(codexproto.MethodThreadStart, test.response)
			conv, err := d.Start(context.Background(), ports.ChatStartConfig{WorkspacePath: t.TempDir(), Model: "requested-model", Effort: "maximum", ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conv.Close() }()
			model, effort, err := conv.(ports.ChatNativeAgentSelectionReader).ReadNativeAgentSelection(context.Background())
			if err != nil || model != test.model || effort != test.effort {
				t.Fatalf("sent configuration masqueraded as native readback: %s %s %v", model, effort, err)
			}
		})
	}
}

func TestNativeExternalToolIsolationRefusesUnavailableOrUnboundedInventory(t *testing.T) {
	for _, kind := range []string{"unavailable", "repeated-cursor", "writable"} {
		t.Run(kind, func(t *testing.T) {
			d, srv := newTestDriver(t)
			if kind == "unavailable" {
				srv.replyError(codexproto.MethodMcpServerStatusList, -32601, "unsupported")
			} else {
				srv.respondTo(codexproto.MethodMcpServerStatusList, `{"data":[],"nextCursor":"repeat"}`)
			}
			conv, err := d.Start(context.Background(), ports.ChatStartConfig{WorkspacePath: t.TempDir(), ReadOnly: kind != "writable"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conv.Close() }()
			if err := conv.(ports.ChatExternalToolIsolationValidator).ValidateNoExternalTools(context.Background()); err == nil {
				t.Fatal("unknown native external tool authority was accepted")
			}
		})
	}
}

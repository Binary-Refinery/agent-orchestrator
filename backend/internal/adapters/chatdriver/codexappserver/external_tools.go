package codexappserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/codexappserver/codexproto"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.ChatExternalToolIsolationValidator = (*conversation)(nil)
var _ ports.ChatNativeAgentSelectionReader = (*conversation)(nil)

// ReadNativeAgentSelection returns values actually reported by the native thread.
func (c *conversation) ReadNativeAgentSelection(context.Context) (string, string, error) {
	return c.threadModel, c.threadEffort, nil
}

// ValidateNoExternalTools reads the effective thread's committed inventories.
// It neither reloads servers nor refreshes connectors. A configured failed MCP
// server is still external authority, so only explicit empty inventories pass.
func (c *conversation) ValidateNoExternalTools(ctx context.Context) error {
	if !c.readOnly || c.threadID == "" {
		return errors.New("external tool isolation requires a loaded read-only native thread")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	detail := codexproto.McpServerStatusDetailToolsAndAuthOnly
	threadID := c.threadID
	var cursor *string
	seen := make(map[string]bool)
	for page := 0; page < 16; page++ {
		var raw json.RawMessage
		params := codexproto.ListMcpServerStatusParams{ThreadID: &threadID, Detail: &detail, Cursor: cursor}
		if err := c.conn.request(ctx, codexproto.MethodMcpServerStatusList, params, &raw); err != nil {
			return fmt.Errorf("cannot verify Instructor MCP isolation: %w", err)
		}
		var response codexproto.ListMcpServerStatusResponse
		if err := decodeExplicitNativeInventory(raw, "data", &response); err != nil {
			return err
		}
		if len(response.Data) != 0 {
			return errors.New("instructor planning requires a native thread with no configured MCP servers")
		}
		if response.NextCursor == nil || *response.NextCursor == "" {
			cursor = nil
			break
		}
		if seen[*response.NextCursor] {
			return errors.New("instructor MCP inventory repeated its cursor")
		}
		seen[*response.NextCursor] = true
		cursor = response.NextCursor
	}
	if cursor != nil {
		return errors.New("instructor MCP inventory exceeded its pagination bound")
	}
	refresh := false
	var raw json.RawMessage
	if err := c.conn.request(ctx, codexproto.MethodAppInstalled, codexproto.AppsInstalledParams{ThreadID: &threadID, ForceRefresh: &refresh}, &raw); err != nil {
		return fmt.Errorf("cannot verify Instructor connector isolation: %w", err)
	}
	var response codexproto.AppsInstalledResponse
	if err := decodeExplicitNativeInventory(raw, "apps", &response); err != nil {
		return err
	}
	if len(response.Apps) != 0 {
		return errors.New("instructor planning requires a native thread with no installed external connectors")
	}
	return nil
}

func decodeExplicitNativeInventory(raw json.RawMessage, field string, result any) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("invalid Instructor native tool inventory: %w", err)
	}
	value, ok := envelope[field]
	if !ok || !strings.HasPrefix(strings.TrimSpace(string(value)), "[") {
		return fmt.Errorf("instructor native tool inventory does not explicitly report %s", field)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("invalid Instructor native tool inventory: %w", err)
	}
	return nil
}

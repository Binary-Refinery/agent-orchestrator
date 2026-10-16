// Package opencodeacp binds the user's own OpenCode installation to AO's
// reusable ACP Chat transport.
package opencodeacp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	acpsdk "github.com/coder/acp-go-sdk"

	"reflect"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/opencode"
	acpdriver "github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/acp"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/nativeacp"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/backbone"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// New launches `opencode acp` from the exact binary resolved by the existing
// OpenCode agent plugin. AO adds only a per-session inline overlay for its
// standing instructions and an explicit bypass-permissions choice.
func New(plugin nativeacp.Plugin, log *slog.Logger) ports.ChatDriver {
	return nativeacp.New(plugin, nativeacp.Config{
		Harness:               domain.HarnessOpenCode,
		Configure:             configure,
		SessionOptions:        sessionOptions,
		RequireSessionOptions: true,
		PermissionPolicy:      permissionPolicy,
		ValidateTurnSettings:  validateTurnSettings,
		BeforeLaunch:          verifyBackboneLaunch,
		BeforeSend:            verifyBackboneSend,
	}, log)
}

func configure(ctx context.Context, cfg acpdriver.LaunchConfig) ([]string, map[string]string, error) {
	if cfg.Backbone != nil {
		verified, err := verifiedBackbone(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
		env, err := opencode.PrepareBackboneEnvironment(cfg.DataDir, string(cfg.SessionID), cfg.Backbone, cfg.Env)
		if err != nil {
			return nil, nil, err
		}
		if err := opencode.ValidateBackboneSources(ctx, cfg.WorkspacePath, env, verified); err != nil {
			return nil, nil, err
		}
		content, err := opencode.PrepareBackboneACPConfigContent(verified.Documents["opencode.json"], verified.Documents[".opencode/agents/backbone-implementer.md"], verified.Documents, cfg.SystemPrompt, cfg.Model, cfg.Effort, env["XDG_DATA_HOME"], cfg.WorkspacePath)
		if err != nil {
			return nil, nil, err
		}
		configPath, err := opencode.WriteBackboneConfig(env, content)
		if err != nil {
			return nil, nil, err
		}
		env["OPENCODE_CONFIG"] = configPath
		env["OPENCODE_CONFIG_CONTENT"] = ""
		return []string{"acp"}, env, nil
	}
	content, err := opencode.PrepareACPConfigContent(
		cfg.Env["OPENCODE_CONFIG_CONTENT"], cfg.SystemPrompt, cfg.Permissions)
	if err != nil {
		return nil, nil, err
	}
	return []string{"acp"}, map[string]string{"OPENCODE_CONFIG_CONTENT": content}, nil
}

func verifiedBackbone(ctx context.Context, cfg acpdriver.LaunchConfig) (*backbone.VerifiedContext, error) {
	if cfg.Backbone == nil || cfg.Model != cfg.Backbone.Model || cfg.Effort != cfg.Backbone.Effort || ports.NormalizePermissionMode(cfg.Permissions) != ports.PermissionModeDefault {
		return nil, fmt.Errorf("%w: governed OpenCode launch selection mismatch", ports.ErrChatConfigOptionInvalid)
	}
	binding, err := backbone.ValidateBinding(cfg.Backbone.Binding, nil)
	if err != nil {
		return nil, err
	}
	verified, err := backbone.Verify(ctx, cfg.WorkspacePath, binding, cfg.Backbone.InstructorRole)
	if err != nil {
		return nil, err
	}
	if verified.Context.Role != "implementer" || verified.Prompt == "" || !strings.HasSuffix(cfg.SystemPrompt, verified.Prompt) {
		return nil, fmt.Errorf("%w: governed OpenCode standing context mismatch", ports.ErrChatConfigOptionInvalid)
	}
	return verified, nil
}

func verifyBackboneLaunch(ctx context.Context, binary string, cfg acpdriver.LaunchConfig, env map[string]string) error {
	if cfg.Backbone == nil {
		return nil
	}
	_, expected, err := configure(ctx, cfg)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, env) {
		return fmt.Errorf("%w: governed OpenCode launch environment changed", ports.ErrChatConfigOptionInvalid)
	}

	verified, err := verifiedBackbone(ctx, cfg)
	if err != nil {
		return err
	}
	content, err := opencode.PrepareBackboneACPConfigContent(verified.Documents["opencode.json"], verified.Documents[".opencode/agents/backbone-implementer.md"], verified.Documents, cfg.SystemPrompt, cfg.Model, cfg.Effort, env["XDG_DATA_HOME"], cfg.WorkspacePath)
	if err != nil {
		return err
	}
	return opencode.VerifyBackboneNative(ctx, opencode.BackboneNativeConfig{
		Binary: binary, WorkspacePath: cfg.WorkspacePath, Env: env, Content: content, SystemPrompt: cfg.SystemPrompt, Model: cfg.Model, Effort: cfg.Effort,
		ValidateSources: func(checkCtx context.Context) error {
			return verifyBackboneSend(checkCtx, cfg, env)
		},
	})
}

func sessionOptions(settings ports.ChatTurnSettings) []acpdriver.SessionOption {
	options := make([]acpdriver.SessionOption, 0, 2)
	if settings.Model != "" {
		options = append(options, acpdriver.SessionOption{ID: "model", Value: settings.Model})
	}
	// OpenCode advertises effort as id "effort" (category "thought_level") with
	// the model's variant names as values. The model setter resets the variant
	// to "default" else the first variant when no explicit variant is given,
	// so the model must be applied first and the effort second.
	if settings.Effort != "" {
		options = append(options, acpdriver.SessionOption{ID: "effort", Value: settings.Effort})
	}
	if len(options) == 0 {
		return nil
	}
	return options
}

// OpenCode parses model overrides as provider/model. A provider display name is
// not an alias for its configured default model; keep that default only when
// the override is empty. Leave model availability to the user's OpenCode.
func validateTurnSettings(_ ports.PermissionMode, settings ports.ChatTurnSettings) error {
	if settings.Model == "" {
		return nil
	}
	provider, model, found := strings.Cut(settings.Model, "/")
	if !found || strings.TrimSpace(provider) == "" || strings.TrimSpace(model) == "" {
		return fmt.Errorf("%w: OpenCode model %q must use provider/model format (for example, anthropic/claude-sonnet); select a full model ID from `opencode models`, or clear the model override to use agent settings", ports.ErrChatConfigOptionInvalid, settings.Model)
	}
	return nil
}

// permissionPolicy is AO's side of accept-edits and auto, and mirrors how
// OpenCode implements --auto: reply to each permission request rather than
// granting anything up front. A tool the user or repository denied never
// raises a request, so provider policy stays authoritative and AO never has to
// reconstruct it. Bypass needs no entry — its agent allows everything, so
// nothing is asked.
func permissionPolicy(
	mode ports.PermissionMode,
	params acpsdk.RequestPermissionRequest,
) (acpsdk.PermissionOptionId, bool) {
	mode = ports.NormalizePermissionMode(mode)
	kind := params.ToolCall.Kind
	if mode != ports.PermissionModeAuto && (mode != ports.PermissionModeAcceptEdits || kind == nil ||
		(*kind != acpsdk.ToolKindEdit && *kind != acpsdk.ToolKindDelete && *kind != acpsdk.ToolKindMove)) {
		return "", false
	}
	// Once, not always: --auto answers each request and persists nothing.
	for _, option := range params.Options {
		if option.Kind == acpsdk.PermissionOptionKindAllowOnce {
			return option.OptionId, true
		}
	}
	return "", false
}

func verifyBackboneSend(ctx context.Context, cfg acpdriver.LaunchConfig, env map[string]string) error {
	if cfg.Backbone == nil {
		return nil
	}
	verified, err := verifiedBackbone(ctx, cfg)
	if err != nil {
		return err
	}
	content, err := opencode.PrepareBackboneACPConfigContent(verified.Documents["opencode.json"], verified.Documents[".opencode/agents/backbone-implementer.md"], verified.Documents, cfg.SystemPrompt, cfg.Model, cfg.Effort, env["XDG_DATA_HOME"], cfg.WorkspacePath)
	if err != nil {
		return err
	}
	if err := opencode.CheckBackboneConfig(env, content); err != nil {
		return err
	}
	return opencode.ValidateBackboneSources(ctx, cfg.WorkspacePath, env, verified)
}

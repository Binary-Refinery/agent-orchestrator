package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/policy/backbone"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func nativeBackboneFixture(t *testing.T) (string, *backbone.VerifiedContext, *ports.ChatBackboneConfig) {
	t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "..", "policy", "backbone", "testdata", "producer"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(cwd string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		argv := []string{"--no-replace-objects", "--no-lazy-fetch", "-c", "core.hooksPath=" + os.DevNull, "-c", "init.templateDir=", "-C", cwd}
		cmd := exec.CommandContext(ctx, "git", append(argv, args...)...)
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v %s", err, output)
		}
	}
	git(root, "clone", "--no-checkout", "-q", filepath.Join(fixture, "workspace.bundle"), "workspace")
	workspace := filepath.Join(root, "workspace")
	git(workspace, "-c", "core.autocrlf=false", "checkout", "-q", "--detach", "c77601c5fcd96188ddeca71b3cc9429a2d692a51")
	raw, err := os.ReadFile(filepath.Join(fixture, "context.json"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := os.ReadFile(filepath.Join(fixture, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packet struct {
		TaskPacket string `json:"task_packet"`
	}
	if err := json.Unmarshal(request, &packet); err != nil {
		t.Fatal(err)
	}
	companion, err := os.ReadFile(filepath.Join(workspace, ".engineering-backbone", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	binding := backbone.Binding{SchemaVersion: "1", AdapterSHA256: fmt.Sprintf("%x", sha256.Sum256(companion)), Contexts: map[string]backbone.RoleBinding{"worker": {Role: "implementer", TaskPacket: packet.TaskPacket, Context: raw}}}
	raw, err = json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := backbone.Verify(context.Background(), workspace, &binding, "worker")
	if err != nil {
		t.Fatal(err)
	}
	return workspace, verified, &ports.ChatBackboneConfig{Binding: raw, InstructorRole: "worker", PolicyVersion: "pinned-test", Model: "openai/gpt-4.1", Effort: "high"}
}
func fixtureEnv(t *testing.T, binding *ports.ChatBackboneConfig) map[string]string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env, err := PrepareBackboneEnvironment(root, "session", binding, nil)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestBackboneActualProducerNativeProjection(t *testing.T) {
	workspace, verified, binding := nativeBackboneFixture(t)
	env := fixtureEnv(t, binding)
	if err := ValidateBackboneSources(context.Background(), workspace, env, verified); err != nil {
		t.Fatal(err)
	}
	prompt := "AO standing prompt\n" + verified.Prompt + " literal {env:SECRET} {file:unbound}"
	content, err := PrepareBackboneACPConfigContent(verified.Documents["opencode.json"], verified.Documents[".opencode/agents/backbone-implementer.md"], verified.Documents, prompt, binding.Model, binding.Effort, env["XDG_DATA_HOME"], workspace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "{env:") || strings.Contains(content, "{file:") {
		t.Fatal("standing context retained native interpolation syntax")
	}
	rules, err := expectedBackboneRules(content)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]string{"*": "deny", "bash": "ask", "edit": "ask", "task": "deny"}
	for _, rule := range rules {
		if want, ok := wanted[rule.Permission]; ok {
			if rule.Action != want {
				t.Fatalf("%s=%s", rule.Permission, rule.Action)
			}
			delete(wanted, rule.Permission)
		}
	}
	if len(wanted) > 0 {
		t.Fatal("missing required permission rules")
	}
	response := map[string]any{"name": "ao-default", "mode": "primary", "prompt": prompt, "variant": "high", "model": map[string]string{"providerID": "openai", "modelID": "gpt-4.1"}, "permission": append([]backbonePermissionRule{{"*", "*", "allow"}}, rules...)}
	raw, _ := json.Marshal(response)
	cfg := BackboneNativeConfig{SystemPrompt: prompt, Model: binding.Model, Effort: binding.Effort, Content: content}
	if err := verifyBackboneAgent(raw, cfg); err != nil {
		t.Fatal(err)
	}
	response["permission"] = append(rules, backbonePermissionRule{"bash", "*", "allow"})
	raw, _ = json.Marshal(response)
	if err := verifyBackboneAgent(raw, cfg); err == nil {
		t.Fatal("later shell allow accepted")
	}
}

func TestBackboneProjectionRetainsExplicitDenies(t *testing.T) {
	_, verified, binding := nativeBackboneFixture(t)
	var project map[string]any
	if err := json.Unmarshal(verified.Documents["opencode.json"], &project); err != nil {
		t.Fatal(err)
	}
	project["permission"] = map[string]any{"*": "deny", "bash": "deny", "read": map[string]string{"*": "allow", "secret*": "deny", "secret-public*": "ask"}}
	raw, _ := json.Marshal(project)
	content, err := PrepareBackboneACPConfigContent(raw, verified.Documents[".opencode/agents/backbone-implementer.md"], verified.Documents, "prompt", binding.Model, binding.Effort, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rules, _ := expectedBackboneRules(content)
	lastRead := ""
	for _, rule := range rules {
		if rule.Permission == "bash" && rule.Action != "deny" {
			t.Fatal("explicit shell deny weakened")
		}
		if rule.Permission == "read" {
			lastRead = rule.Action
		}
	}
	if lastRead != "deny" {
		t.Fatal("overlapping explicit read deny must remain last")
	}
}

func TestBackboneRejectsNativeSourceLayers(t *testing.T) {
	for _, relative := range []string{".opencode/modes/ao-default.md", ".opencode/Mode/other.md", ".opencode/skills/code-review/nested/SKILL.md", ".opencode/plugins/plugin.js", ".agents/skills/extra/SKILL.md", "nested/AGENTS.md", "opencode.jsonc", ".npmrc"} {
		t.Run(relative, func(t *testing.T) {
			workspace, verified, binding := nativeBackboneFixture(t)
			env := fixtureEnv(t, binding)
			path := filepath.Join(workspace, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("data-only unbound input"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := ValidateBackboneSources(context.Background(), workspace, env, verified); err == nil {
				t.Fatal("unbound native source accepted")
			}
		})
	}
}

func TestBackboneIsolatedEnvironmentAndOwnership(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "selected-credential")
	t.Setenv("ANTHROPIC_API_KEY", "unselected-credential")
	t.Setenv("NODE_OPTIONS", "unsafe-inherited-option")
	binding := &ports.ChatBackboneConfig{Binding: []byte(`{}`), Model: "openai/gpt-4.1", PolicyVersion: "approved"}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env, err := PrepareBackboneEnvironment(root, "../../session", binding, map[string]string{"home": "uncontrolled"})
	if err != nil {
		t.Fatal(err)
	}
	if env["OPENAI_API_KEY"] != "selected-credential" || env["ANTHROPIC_API_KEY"] != "" || env["NODE_OPTIONS"] != "" {
		t.Fatal("native credential/runtime environment not isolated")
	}
	if !strings.HasPrefix(env["HOME"], root+string(filepath.Separator)) || env["HOME"] != env["USERPROFILE"] {
		t.Fatal("native home escaped owned state")
	}
	if runtime.GOOS == "windows" {
		if _, ok := env["home"]; ok {
			t.Fatal("case alias can override isolated HOME")
		}
	}
	same, err := PrepareBackboneEnvironment(root, "../../session", binding, nil)
	if err != nil || same["HOME"] != env["HOME"] {
		t.Fatal("fresh resume does not retain controlled state")
	}
	if _, err := PrepareBackboneEnvironment(root, "session", binding, map[string]string{"OPENCODE_PERMISSION": "allow"}); err == nil {
		t.Fatal("native permission override accepted")
	}
}

func TestBackboneNativeProbeChecksSourcesBeforeExecution(t *testing.T) {
	sentinel := errors.New("source check refused")
	called := false
	_, err := backboneProbe(context.Background(), BackboneNativeConfig{Binary: "must-not-run", ValidateSources: func(context.Context) error { called = true; return sentinel }}, "--version")
	if !called || !errors.Is(err, sentinel) {
		t.Fatal("source gate did not precede native execution")
	}

}

// This optional positive readback sends no inference request and does not
// constitute model qualification. CI does not infer a pass when it is skipped.
func TestBackboneNativePositiveReadback(t *testing.T) {
	binary := os.Getenv("AO_BACKBONE_NATIVE_BINARY")
	if binary == "" {
		t.Skip("exact native binary not supplied; data-only tests cover admission")
	}
	workspace, verified, binding := nativeBackboneFixture(t)
	binding.Model = "openai/gpt-5"
	binding.Effort = "medium"
	t.Setenv("OPENAI_API_KEY", "positive-readback-fixture-no-inference")
	env := fixtureEnv(t, binding)
	prompt := "AO standing prompt\n" + verified.Prompt
	content, err := PrepareBackboneACPConfigContent(verified.Documents["opencode.json"], verified.Documents[".opencode/agents/backbone-implementer.md"], verified.Documents, prompt, binding.Model, binding.Effort, env["XDG_DATA_HOME"], workspace)
	if err != nil {
		t.Fatal(err)
	}
	configPath, err := WriteBackboneConfig(env, content)
	if err != nil {
		t.Fatal(err)
	}
	env["OPENCODE_CONFIG"] = configPath
	env["OPENCODE_CONFIG_CONTENT"] = ""
	check := func(ctx context.Context) error { return ValidateBackboneSources(ctx, workspace, env, verified) }
	if err := VerifyBackboneNative(context.Background(), BackboneNativeConfig{Binary: binary, WorkspacePath: workspace, SystemPrompt: prompt, Model: binding.Model, Effort: binding.Effort, Env: env, Content: content, ValidateSources: check}); err != nil {
		t.Fatal(err)
	}
	if err := check(context.Background()); err != nil {
		t.Fatalf("native readback created an unbound input: %v", err)
	}
}

func TestBackboneNativeProbeOutputBound(t *testing.T) {
	if os.Getenv("AO_TEST_BACKBONE_OUTPUT") == "1" {
		_, _ = os.Stdout.Write(make([]byte, 8<<20))
		os.Exit(0)
	}
	_, err := backboneProbe(context.Background(), BackboneNativeConfig{Binary: os.Args[0], WorkspacePath: t.TempDir(), Env: map[string]string{"AO_TEST_BACKBONE_OUTPUT": "1", "TMPDIR": t.TempDir()}, ValidateSources: func(context.Context) error { return nil }}, "-test.run=^TestBackboneNativeProbeOutputBound$")
	if err == nil || !strings.Contains(err.Error(), "output bound") {
		t.Fatalf("large native output was not refused: %v", err)
	}
}

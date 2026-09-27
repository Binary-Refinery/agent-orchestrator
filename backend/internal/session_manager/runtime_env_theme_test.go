package sessionmanager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/termtheme"
)

// Every agent gets the terminal appearance hints, not only adapters that ask
// for them: CLIs that follow the terminal theme fall back to dark without one.
func TestAugmentAgentRuntimeEnvAppliesTerminalTheme(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, termtheme.FileName), []byte("light\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{dataDir: dataDir}

	env := map[string]string{}
	m.augmentAgentRuntimeEnv(fakeAgent{}, env)
	if env[termtheme.EnvTheme] != "light" || env[termtheme.EnvColorFgBg] != "0;15" {
		t.Fatalf("env = %v, want light theme hints", env)
	}

	// An explicit project value wins over the desktop's theme.
	env = map[string]string{termtheme.EnvColorFgBg: "15;0"}
	m.augmentAgentRuntimeEnv(envAugmentingAgent{key: "X", value: "y"}, env)
	if env[termtheme.EnvColorFgBg] != "15;0" {
		t.Fatalf("COLORFGBG = %q, want project value kept", env[termtheme.EnvColorFgBg])
	}
	if env["X"] != filepath.Join(dataDir, "y") {
		t.Fatalf("adapter augmentation lost: %v", env)
	}
}

// Without a theme file (headless daemon, no desktop) nothing is guessed.
func TestAugmentAgentRuntimeEnvLeavesThemeUnsetWithoutHint(t *testing.T) {
	m := &Manager{dataDir: t.TempDir()}
	env := map[string]string{}
	m.augmentAgentRuntimeEnv(fakeAgent{}, env)
	if _, ok := env[termtheme.EnvTheme]; ok {
		t.Fatalf("env = %v, want no theme hints", env)
	}
	if _, ok := env[termtheme.EnvColorFgBg]; ok {
		t.Fatalf("env = %v, want no theme hints", env)
	}
}

package modelcatalog

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
)

func buildOpenCodeProbe(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "opencode-probe")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, "./testdata/opencodeprobe")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build native command fixture: %v\n%s", err, output)
	}
	models, err := filepath.Abs("testdata/opencodeprobe/models.txt")
	if err != nil {
		t.Fatal(err)
	}
	return binary, models
}

func probeEnvironment(models, root string) map[string]string {
	return map[string]string{"AO_PROBE_MODELS": models, "AO_PROBE_CALLS": filepath.Join(root, "calls"), "HOME": root, "USERPROFILE": root, "XDG_CONFIG_HOME": root, "OPENCODE_CONFIG_CONTENT": "", "OPENCODE_CONFIG_DIR": "", "OPENCODE_CONFIG": ""}
}

func TestOpenCodeDiscoveryUsesResolvedProjectConfigInsteadOfAbsentDefaultFields(t *testing.T) {
	binary, models := buildOpenCodeProbe(t)
	for _, test := range []struct{ name, config, inline, wanted string }{
		{"project A", `{"model":"custom/configured","agent":{"build":{"model":"custom/a-first"}},"provider":{"token":"secret-sentinel"}}`, "", "custom/configured"},
		{"project B", `{"model":"custom/no-max"}`, "", "custom/no-max"},
		{"native inline override", `{"model":"custom/configured"}`, `{"model":"custom/a-first"}`, "custom/a-first"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "native-config.json"), []byte(test.config), 0600); err != nil {
				t.Fatal(err)
			}
			env := probeEnvironment(models, root)
			env["OPENCODE_CONFIG_CONTENT"] = test.inline
			catalog, err := Discover(context.Background(), "opencode", binary, root, env)
			if err != nil {
				t.Fatal(err)
			}
			defaults := 0
			for _, model := range catalog.Models {
				if model.IsDefault {
					defaults++
					if model.ID != test.wanted {
						t.Fatalf("model-name heuristic or wrong project selected %s, want %s", model.ID, test.wanted)
					}
				}
				if model.DefaultEffort != "" {
					t.Fatal("invented native defaultVariant")
				}
			}
			if defaults != 1 || catalog.Warning != "" {
				t.Fatalf("configured selection unavailable: %+v", catalog)
			}
			raw, err := json.Marshal(catalog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "secret-sentinel") {
				t.Fatal("config secrets escaped into catalog")
			}
			calls, err := os.ReadFile(env["AO_PROBE_CALLS"])
			if err != nil {
				t.Fatal(err)
			}
			if string(calls) != "models --verbose\ndebug config\n" {
				t.Fatalf("discovery requested a server/turn/tool or unsupported surface: %s", calls)
			}
		})
	}
}

func TestOpenCodeUnknownDefaultsKeepExplicitCatalogUsableWithoutGuessing(t *testing.T) {
	binary, models := buildOpenCodeProbe(t)
	for _, test := range []struct{ name, config, mode string }{
		{"implicit recent or sorted fallback", `{"agent":{"build":{"model":"custom/configured"}}}`, ""},
		{"configured model absent from catalog", `{"model":"custom/missing"}`, ""},
		{"unsupported debug command", `{}`, "unsupported"},
		{"failed config command", `{}`, "error"},
		{"malformed sensitive output", `{"model":"secret-sentinel"`, ""},
		{"optional config timed out", `{}`, "slow"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "native-config.json"), []byte(test.config), 0600); err != nil {
				t.Fatal(err)
			}
			env := probeEnvironment(models, root)
			env["AO_PROBE_CONFIG_MODE"] = test.mode
			catalog, err := Discover(context.Background(), "opencode", binary, root, env)
			if err != nil || len(catalog.Models) != 3 || catalog.Stale {
				t.Fatalf("unknown default blocked explicit native model choices: %+v %v", catalog, err)
			}
			for _, model := range catalog.Models {
				if model.IsDefault {
					t.Fatalf("unknown default guessed from name/order/agent metadata: %+v", model)
				}
			}
			if !strings.Contains(catalog.Warning, "explicit OpenCode model") || strings.Contains(catalog.Warning, "secret-sentinel") {
				t.Fatalf("unsafe/unactionable refusal: %s", catalog.Warning)
			}
		})
	}
}

func TestOpenCodeConfigCaptureIsBoundedAndDoesNotExposeNativeSecrets(t *testing.T) {
	binary, models := buildOpenCodeProbe(t)
	for _, mode := range []string{"error", "large", "slow"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			env := probeEnvironment(models, root)
			env["AO_PROBE_CONFIG_MODE"] = mode
			timeout := 2 * time.Second
			if mode == "slow" {
				timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			_, err := openCodeConfiguredModel(ctx, binary, root, env)
			if err == nil || strings.Contains(err.Error(), "secret-sentinel") {
				t.Fatalf("unbounded/sensitive config lookup accepted: %v", err)
			}
			if mode == "large" && !errors.Is(err, errOpenCodeConfigOutputLimit) {
				t.Fatalf("actual process output did not exercise the capture bound: %v", err)
			}
			if mode == "slow" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("caller cancellation was not preserved: %v", err)
			}
		})
	}
	for _, raw := range []string{`null`, `[]`, `"secret-sentinel"`, `{"model":"custom/configured"} {}`, `{"model":17}`, `{"model":"custom/"}`, strings.Repeat("x", openCodeConfigOutputLimit+1)} {
		if _, err := parseOpenCodeConfiguredModel([]byte(raw)); err == nil || strings.Contains(err.Error(), "secret-sentinel") {
			t.Fatalf("invalid native shape accepted/exposed: %v", err)
		}
	}
	if model, err := parseOpenCodeConfiguredModel([]byte(`{"model":"openrouter/anthropic/claude-native"}`)); err != nil || model != "openrouter/anthropic/claude-native" {
		t.Fatalf("native nested model identity changed: %q %v", model, err)
	}
}

func TestOpenCodeConfiguredDefaultFingerprintInvalidatesOldMetadataAndConfigChanges(t *testing.T) {
	root := t.TempDir()
	env := probeEnvironment("", root)
	ctx := context.Background()
	initial := CatalogFingerprint(ctx, "opencode", "", root, env)
	old := sha256.Sum256([]byte("\x00\x00opencode-native-variants-v1"))
	if initial == fmt.Sprintf("%x", old[:8]) {
		t.Fatal("old absent-default catalog remains cache-valid")
	}
	path := filepath.Join(root, "opencode.json")
	if err := os.WriteFile(path, []byte(`{"model":"custom/configured"}`), 0600); err != nil {
		t.Fatal(err)
	}
	first := CatalogFingerprint(ctx, "opencode", "", root, env)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"model":"custom/no-max"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	second := CatalogFingerprint(ctx, "opencode", "", root, env)
	if initial == first || first == second {
		t.Fatal("native default config content did not invalidate metadata")
	}
	global := filepath.Join(root, "opencode", "opencode.jsonc")
	if err := os.MkdirAll(filepath.Dir(global), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte(`{"model":"custom/configured"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if second == CatalogFingerprint(ctx, "opencode", "", root, env) {
		t.Fatal("global native config change was not fingerprinted")
	}
	before := CatalogFingerprint(ctx, "opencode", "", root, env)
	env["OPENCODE_CONFIG_CONTENT"] = `{"model":"custom/a-first"}`
	if before == CatalogFingerprint(ctx, "opencode", "", root, env) {
		t.Fatal("inline native override was not fingerprinted")
	}
}

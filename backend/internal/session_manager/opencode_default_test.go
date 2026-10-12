package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/modelcatalog"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type nativeOpenCodeCatalog struct {
	binary   string
	projects map[string]domain.ProjectRecord
	calls    int
}

func (c *nativeOpenCodeCatalog) Models(ctx context.Context, harness, projectID string, refresh bool) (ports.AgentModelCatalog, error) {
	if harness != "opencode" || !refresh {
		return ports.AgentModelCatalog{}, fmt.Errorf("required native lookup did not refresh the correct harness")
	}
	project, ok := c.projects[projectID]
	if !ok {
		return ports.AgentModelCatalog{}, fmt.Errorf("missing native project scope")
	}
	c.calls++
	return (modelcatalog.Discoverer{}).Discover(ctx, ports.AgentModelDiscoveryRequest{AgentID: harness, Binary: c.binary, WorkingDir: project.Path, Env: project.Config.Env})
}

func nativeOpenCodeFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "opencode-probe")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	path := filepath.Join("..", "adapters", "agent", "modelcatalog", "testdata", "opencodeprobe")
	command := exec.Command("go", "build", "-o", binary, path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build native command fixture: %v\n%s", err, output)
	}
	models, err := filepath.Abs(filepath.Join(path, "models.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return binary, models
}

func configureNativeOpenCodeProject(t *testing.T, store *fakeStore, models, config, mode string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "native-config.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	project := store.projects["mer"]
	project.Path = root
	project.Config.Env = map[string]string{"AO_PROBE_MODELS": models, "AO_PROBE_CONFIG_MODE": mode, "OPENCODE_CONFIG_CONTENT": ""}
	store.projects["mer"] = project
}

func TestOpenCodeConfiguredDefaultIsPinnedThroughNativeFloorAndLaunch(t *testing.T) {
	binary, models := nativeOpenCodeFixture(t)
	for _, test := range []struct {
		name                                     string
		mode                                     domain.SessionMode
		model, effort, wantedModel, wantedEffort string
		automation, disabled                     bool
	}{
		{name: "TUI provider default", mode: domain.SessionModeTUI, wantedModel: "custom/configured", wantedEffort: "max"},
		{name: "Chat provider default", mode: domain.SessionModeChat, wantedModel: "custom/configured", wantedEffort: "max"},
		{name: "automation provider default", mode: domain.SessionModeChat, automation: true, wantedModel: "custom/configured", wantedEffort: "max"},
		{name: "explicit model and variant", mode: domain.SessionModeTUI, model: "custom/no-max", effort: "contributor", wantedModel: "custom/no-max", wantedEffort: "contributor"},
		{name: "default model with explicit variant", mode: domain.SessionModeChat, effort: "contributor", wantedModel: "custom/configured", wantedEffort: "contributor"},
		{name: "disabled floor preserves provider defaults", mode: domain.SessionModeTUI, disabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.disabled {
				t.Setenv("AO_OPENCODE_EFFORT_FLOOR", "")
			} else {
				t.Setenv("AO_OPENCODE_EFFORT_FLOOR", "1")
			}
			launcher := &recordingLauncher{}
			manager, store, _ := newChatManager(launcher)
			manager.dataDir = t.TempDir()
			agent := &recordingAgent{}
			manager.agents = singleAgent{agent: agent}
			configureNativeOpenCodeProject(t, store, models, `{"model":"custom/configured","agent":{"build":{"model":"custom/a-first"}}}`, "")
			catalog := &nativeOpenCodeCatalog{binary: binary, projects: store.projects}
			manager.SetModelCatalog(catalog)
			cfg := ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, RequestedMode: test.mode, AgentConfig: ports.AgentConfig{Model: test.model, Effort: test.effort}}
			if test.automation {
				run := domain.AutomationRunID("native-floor-run")
				cfg.AutomationRunID = &run
			}
			record, _, _, err := manager.Spawn(context.Background(), cfg)
			if err != nil {
				t.Fatalf("configured native default failed: %v", err)
			}
			if record.Metadata.Model != test.wantedModel || record.Metadata.Effort != test.wantedEffort {
				t.Fatalf("durable configuration drifted: %+v", record.Metadata)
			}
			if test.mode == domain.SessionModeChat {
				if len(launcher.started) != 1 || launcher.started[0].Model != test.wantedModel || launcher.started[0].Effort != test.wantedEffort {
					t.Fatalf("native Chat choice differs from validated model: %+v", launcher.started)
				}
			} else if agent.lastLaunch.Config.Model != test.wantedModel || agent.lastLaunch.Config.Effort != test.wantedEffort {
				t.Fatalf("native TUI choice differs from validated model: %+v", agent.lastLaunch.Config)
			}
			if test.disabled && catalog.calls != 0 {
				t.Fatal("default-off behavior unexpectedly required configured-native discovery")
			}
		})
	}
}

func TestOpenCodeUnknownConfiguredDefaultRefusesBeforeLaunchButExplicitChoiceStillWorks(t *testing.T) {
	binary, models := nativeOpenCodeFixture(t)
	t.Setenv("AO_OPENCODE_EFFORT_FLOOR", "1")
	for _, test := range []struct {
		name, config, mode string
		want               error
	}{
		{"implicit fallback", `{}`, "", ports.ErrModelCapabilitiesUnavailable},
		{"debug unsupported", `{}`, "unsupported", ports.ErrModelCapabilitiesUnavailable},
		{"no native max", `{"model":"custom/no-max"}`, "", ports.ErrUnsupportedEffort},
	} {
		t.Run(test.name, func(t *testing.T) {
			launcher := &recordingLauncher{}
			manager, store, rt := newChatManager(launcher)
			manager.dataDir = t.TempDir()
			configureNativeOpenCodeProject(t, store, models, test.config, test.mode)
			manager.SetModelCatalog(&nativeOpenCodeCatalog{binary: binary, projects: store.projects})
			cfg := ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, RequestedMode: domain.SessionModeChat}
			_, _, _, err := manager.Spawn(context.Background(), cfg)
			if !errors.Is(err, test.want) {
				t.Fatalf("unknown native default/floor escaped: %v", err)
			}
			if errors.Is(test.want, ports.ErrModelCapabilitiesUnavailable) && !strings.Contains(err.Error(), "explicit OpenCode model") {
				t.Fatalf("unactionable default refusal: %v", err)
			}
			if len(launcher.started) != 0 || rt.created != 0 || len(store.sessions) != 0 {
				t.Fatal("native provider-default refusal created work or started a controller")
			}
			cfg.AgentConfig = ports.AgentConfig{Model: "custom/no-max", Effort: "contributor"}
			if _, _, _, err := manager.Spawn(context.Background(), cfg); err != nil {
				t.Fatalf("explicit native choice depends on optional debug config: %v", err)
			}
			if len(launcher.started) != 1 || launcher.started[0].Model != "custom/no-max" || launcher.started[0].Effort != "contributor" {
				t.Fatal("explicit native profile silently changed")
			}
		})
	}
}

package review

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type defaultsCatalog struct {
	stale   bool
	efforts []string
}

func (c defaultsCatalog) Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error) {
	return ports.AgentModelCatalog{Stale: c.stale, Models: []ports.AgentModelInfo{{ID: "provider/a", Efforts: c.efforts, EffortSupport: "supported"}, {ID: "provider/b", Efforts: []string{"contributor"}, EffortSupport: "supported"}}}, nil
}

func TestReviewerNativeModelDefaultsAndMissingChoice(t *testing.T) {
	defaults := domain.NativeEffortDefaults{"codex": {"provider/a": "native-high", "provider/b": "contributor"}}
	base := domain.AgentConfig{Model: "provider/a", Effort: "native-high", NativeEffortDefaults: &defaults}
	e := &Engine{modelCatalog: defaultsCatalog{efforts: []string{"native-high"}}}
	changed := mergeReviewerAgentConfig(base, domain.AgentConfig{Model: "provider/b"})
	config, err := e.resolveNativeReviewerConfig(context.Background(), domain.ReviewerCodex, "p", changed)
	if err != nil || config.Model != "provider/b" || config.Effort != "contributor" || config.NativeEffortDefaults != nil {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	unknown := mergeReviewerAgentConfig(base, domain.AgentConfig{Model: "provider/unknown"})
	if _, err := e.resolveNativeReviewerConfig(context.Background(), domain.ReviewerCodex, "p", unknown); !errors.Is(err, ports.ErrUnsupportedEffort) {
		t.Fatalf("unmapped model inherited old effort: %v", err)
	}
	e.modelCatalog = defaultsCatalog{stale: true}
	if _, err := e.resolveNativeReviewerConfig(context.Background(), domain.ReviewerCodex, "p", changed); !errors.Is(err, ports.ErrModelCapabilitiesUnavailable) {
		t.Fatalf("stale mapping accepted: %v", err)
	}
}

func TestReviewerReparsedDefaultsAndUnselectedChangesDoNotRestartController(t *testing.T) {
	ctx := context.Background()
	defaults := domain.NativeEffortDefaults{"codex": {"provider/a": "native-high", "provider/b": "contributor"}}
	config := domain.AgentConfig{Model: "provider/a", Effort: "native-high", NativeEffortDefaults: &defaults}
	store := &fakeStore{}
	first := &fakeLauncher{handle: "review-1", agentSessionID: "native-project-a"}
	projects := fakeProjects{cfg: domain.ProjectConfig{Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex, AgentConfig: config}}}}
	e := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), projects, first)
	e.SetModelCatalog(defaultsCatalog{efforts: []string{"native-high"}})
	if _, err := e.Trigger(ctx, "mer-1", "", domain.AgentConfig{}); err != nil {
		t.Fatal(err)
	}
	for i := range store.runs {
		store.runs[i].Status = domain.ReviewRunComplete
	}
	data, err := json.Marshal(projects.cfg)
	if err != nil {
		t.Fatal(err)
	}
	var reparsed domain.ProjectConfig
	if err := json.Unmarshal(data, &reparsed); err != nil {
		t.Fatal(err)
	}
	(*reparsed.Reviewers[0].AgentConfig.NativeEffortDefaults)["codex"]["provider/b"] = "different-unselected-choice"
	second := &fakeLauncher{alive: true, handle: "review-1"}
	e = newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha2"), fakeProjects{cfg: reparsed}, second)
	e.SetModelCatalog(defaultsCatalog{efforts: []string{"native-high"}})
	if _, err := e.Trigger(ctx, "mer-1", "", domain.AgentConfig{}); err != nil {
		t.Fatal(err)
	}
	if second.spawned || !second.notified || second.gotSpec.AgentConfig.NativeEffortDefaults != nil {
		t.Fatalf("project-only map changed native launch identity: %+v", second)
	}
}

type defaultReviewerCatalog struct{}

func (defaultReviewerCatalog) Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error) {
	return ports.AgentModelCatalog{Models: []ports.AgentModelInfo{{ID: "provider-default", IsDefault: true, Efforts: []string{"high"}, EffortSupport: "supported"}}}, nil
}

func TestReviewerEffortOnlyValidatesReportedProviderDefault(t *testing.T) {
	e := &Engine{modelCatalog: defaultReviewerCatalog{}}
	config, err := e.resolveNativeReviewerConfig(context.Background(), domain.ReviewerCodex, "p", domain.AgentConfig{Effort: "high"})
	if err != nil || config.Effort != "high" || config.Model != "" {
		t.Fatalf("valid default-model effort lost: %+v %v", config, err)
	}
	if _, err := e.resolveNativeReviewerConfig(context.Background(), domain.ReviewerCodex, "p", domain.AgentConfig{Effort: "unsupported"}); !errors.Is(err, ports.ErrUnsupportedEffort) {
		t.Fatalf("invalid default-model effort accepted: %v", err)
	}
}

func TestSameReviewerSelectionWithReparsedMapsPreservesNativeSession(t *testing.T) {
	defaults := domain.NativeEffortDefaults{"codex": {"provider/a": "native-high", "provider/b": "contributor"}}
	projects := fakeProjects{cfg: domain.ProjectConfig{Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex, AgentConfig: domain.AgentConfig{Model: "provider/a", Effort: "native-high", NativeEffortDefaults: &defaults}}}}}
	launch := domain.AgentConfig{Model: "provider/a", Effort: "native-high"}
	store := &fakeStore{review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "existing-pane", AgentSessionID: "existing-native", LaunchAgentConfig: &launch}}
	launcher := &fakeLauncher{alive: true, handle: "existing-pane"}
	e := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), projects, launcher)
	e.SetModelCatalog(defaultsCatalog{efforts: []string{"native-high"}})
	if _, err := e.SwitchReviewer(context.Background(), "mer-1", domain.ReviewerCodex, domain.AgentConfig{Model: "provider/a", Effort: "native-high"}); err != nil {
		t.Fatal(err)
	}
	if launcher.destroyed || launcher.restored || launcher.spawned || len(store.agentSessionUpdates) != 0 {
		t.Fatalf("unchanged native choice reset its controller: %+v", launcher)
	}
	if store.review.AgentSessionID != "existing-native" {
		t.Fatalf("native session lost: %+v", store.review)
	}
}

package chat_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
)

type uncertainNativeConfigConversation struct {
	*partialNativeConfigConversation
	failure error
}

func (c *uncertainNativeConfigConversation) SetConfigOption(context.Context, string, ports.ChatConfigOptionValue) ([]ports.ChatConfigOption, error) {
	c.options = []ports.ChatConfigOption{{ID: "effort", Current: ports.ChatConfigOptionValue{Select: "low"}}}
	return nil, c.failure // The provider mutated its selection before losing its response.
}

func TestGovernedNativeSetterUncertaintyBlocksControllerAfterMutation(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	failure := errors.New("native setter response lost")
	conv := &uncertainNativeConfigConversation{partialNativeConfigConversation: &partialNativeConfigConversation{fakeConversation: newFakeConversation()}, failure: failure}
	svc := chatsvc.New(chatsvc.Options{Store: store, Sessions: store, Drivers: fakeRegistry{driver: fakeDriver{conv: conv}}, NewID: func() string { return "uncertain-native-config" }})
	t.Cleanup(func() { _ = svc.Stop(ctx, testSession) })
	if _, err := svc.Start(ctx, chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(), Model: "required-model", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	svc.SetAgentConfigValidator(func(context.Context, domain.SessionID, string, string) error { return nil })
	if _, err := svc.SetConfigOption(ctx, testSession, "effort", ports.ChatConfigOptionValue{Select: "high"}); !errors.Is(err, failure) {
		t.Fatalf("uncertain mutation failure hidden: %v", err)
	}
	if svc.HasLiveChatController(testSession) {
		t.Fatal("uncertain native selection left a usable governed controller")
	}
}

type partialNativeConfigConversation struct {
	*fakeConversation
	options []ports.ChatConfigOption
}

func (c *partialNativeConfigConversation) ListConfigOptions(context.Context) ([]ports.ChatConfigOption, error) {
	return c.options, nil
}
func (c *partialNativeConfigConversation) SetConfigOption(context.Context, string, ports.ChatConfigOptionValue) ([]ports.ChatConfigOption, error) {
	return c.options, nil
}

func TestPartialNativeConfigReadbackNeverConfirmsRetainedDesiredChoices(t *testing.T) {
	model := ports.ChatConfigOption{ID: "model", Category: "model", CurrentReported: true, Current: ports.ChatConfigOptionValue{Select: "reported-model"}}
	effort := ports.ChatConfigOption{ID: "effort", Category: "thought_level", CurrentReported: true, Current: ports.ChatConfigOptionValue{Select: "high"}}
	acknowledgedEffort := effort
	acknowledgedEffort.CurrentReported = false
	for _, test := range []struct {
		name, stage string
		options     []ports.ChatConfigOption
	}{
		{"effort only", "native-config-partial", []ports.ChatConfigOption{effort}},
		{"model only", "native-config-partial", []ports.ChatConfigOption{model}},
		{"both reported", "native-config-reported", []ports.ChatConfigOption{model, effort}},
		{"locally acknowledged effort", "native-config-partial", []ports.ChatConfigOption{model, acknowledgedEffort}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t)
			conv := &partialNativeConfigConversation{fakeConversation: newFakeConversation(), options: test.options}
			svc := chatsvc.New(chatsvc.Options{Store: store, Sessions: store, Drivers: fakeRegistry{driver: fakeDriver{conv: conv}}, NewID: func() string { return "partial-native-config" }})
			t.Cleanup(func() { _ = svc.Stop(ctx, testSession) })
			if _, err := svc.Start(ctx, chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(), Model: "requested-model", Effort: "ultra"}); err != nil {
				t.Fatal(err)
			}
			stage, observedModel := "", ""
			svc.SetAgentConfigObserver(func(_ context.Context, _ domain.SessionID, model, _, reportedStage string) error {
				stage, observedModel = reportedStage, model
				return nil
			})
			if _, err := svc.SetConfigOption(ctx, testSession, "effort", ports.ChatConfigOptionValue{Select: "high"}); err != nil {
				t.Fatal(err)
			}
			if stage != test.stage {
				t.Fatalf("partial provider report became full confirmation: stage=%s", stage)
			}
			if test.name == "effort only" && observedModel != "requested-model" {
				t.Fatalf("retained desired model was lost: %s", observedModel)
			}
		})
	}
}

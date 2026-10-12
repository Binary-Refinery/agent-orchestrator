package review

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestDurableReviewerPickerChoiceIsRestoredOrStartsFreshBeforeReuse(t *testing.T) {
	for _, restore := range []bool{true, false} {
		t.Run(map[bool]string{true: "restore picker choice", false: "fresh selected config instead of mislabeled notify"}[restore], func(t *testing.T) {
			ctx := context.Background()
			store := newSQLiteReviewStore(t)
			worker := liveWorker()
			worker.ReviewerHarness = domain.ReviewerCodex
			seedReviewWorker(t, store, worker)
			launched := domain.AgentConfig{Model: "launched-model", Effort: "high"}
			picked := domain.AgentConfig{Model: "picked-model", Effort: "xhigh"}
			review := domain.Review{ID: "review-picker", SessionID: worker.ID, ProjectID: worker.ProjectID, Harness: domain.ReviewerCodex, InterfaceMode: domain.ReviewerInterfaceChat, ReviewerHandleID: "review-chat:review-picker", AgentSessionID: "native-reviewer", ProviderConversationID: "native-reviewer", ControllerGeneration: "original-generation", LaunchAgentConfig: &launched}
			if err := store.UpsertReview(ctx, review); err != nil {
				t.Fatal(err)
			}
			conversation, err := store.CreateReviewConversation(ctx, "reviewer-conversation", review.ID, worker.ProjectID, worker.ID, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SetConversationSettings(ctx, conversation.ID, domain.ConversationSettings{Model: picked.Model, ReasoningEffort: picked.Effort}, time.Now()); err != nil {
				t.Fatal(err)
			}
			launcher := &fakeLauncher{interfaceMode: domain.ReviewerInterfaceChat, handle: review.ReviewerHandleID, alive: !restore}
			projects := fakeProjects{cfg: domain.ProjectConfig{Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex, AgentConfig: launched}}}}
			engine := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha2"), projects, launcher)
			if restore {
				if _, err := engine.RestoreReviewer(ctx, worker.ID); err != nil {
					t.Fatal(err)
				}
				if !launcher.restored || launcher.gotSpec.AgentConfig != picked || launcher.gotSpec.ProviderConversationID != "native-reviewer" {
					t.Fatalf("restore lost the actual durable picker choice: %+v", launcher)
				}
			} else {
				if _, err := engine.Trigger(ctx, worker.ID, "", domain.AgentConfig{}); err != nil {
					t.Fatal(err)
				}
				if !launcher.spawned || launcher.notified || launcher.gotSpec.AgentConfig != launched || launcher.gotSpec.AgentSessionID != "" {
					t.Fatalf("selected launch config was mislabeled on the old differently configured controller: %+v", launcher)
				}
			}
		})
	}
}

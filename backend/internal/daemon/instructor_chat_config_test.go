package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type reviewerConfigConversation struct {
	events chan ports.ChatEvent
	once   sync.Once
}

type unboundControlStore struct{ instructorTestStore }

func (unboundControlStore) InstructorWorkForSession(context.Context, domain.SessionID) (domain.InstructorWork, bool, error) {
	return domain.InstructorWork{}, false, nil
}

type controlRoleValidator struct {
	calls int
	err   error
}

func (v *controlRoleValidator) ValidateInstructorAgentConfig(_ context.Context, _ domain.ProjectID, role domain.InstructorRole, harness, model, effort string) error {
	v.calls++
	if role.Harness != "codex" || harness != "opencode" || model != "old-model" || effort != "minimal" {
		return errors.New("unexpected control-role validation input")
	}
	return v.err
}

// Project control sessions deliberately live above individual WorkIDs. That
// separation must not exempt their replacement controllers from role checks.
func TestInstructorResumeValidatesUnboundProjectOrchestrator(t *testing.T) {
	dir := t.TempDir()
	store := unboundControlStore{instructorTestStore: instructorTestStore{
		projects: []domain.ProjectRecord{{ID: "project", Path: dir}},
		sessions: []domain.SessionRecord{{ID: "control", ProjectID: "project", Kind: domain.KindOrchestrator, Harness: domain.HarnessOpenCode, Metadata: domain.SessionMetadata{Model: "old-model", Effort: "minimal"}}},
	}}
	want := errors.New("project orchestrator no longer satisfies role policy")
	validator := &controlRoleValidator{err: want}
	guard := instructorGovernance{svc: instructorsvc.New(instructorsvc.Deps{Store: store}), store: store, validator: validator}
	if err := guard.ValidateResume(context.Background(), "control"); !errors.Is(err, want) {
		t.Fatalf("unbound project control bypassed resume role validation: %v", err)
	}
	if validator.calls != 1 {
		t.Fatalf("native role checks=%d, want exactly one", validator.calls)
	}
	store.sessions[0].Kind = domain.KindWorker
	validator.calls = 0
	guard.store = store
	if err := guard.ValidateResume(context.Background(), "control"); err != nil || validator.calls != 0 {
		t.Fatalf("unassigned legacy worker was silently adopted into governance: %v, calls=%d", err, validator.calls)
	}
}

func (*reviewerConfigConversation) ProviderConversationID() string { return "native-review" }
func (*reviewerConfigConversation) Capabilities() ports.ChatCapabilities {
	return ports.ChatCapabilities{ports.ChatCapabilityStreaming: true, ports.ChatCapabilityApprovals: true, ports.ChatCapabilityInterrupt: true, ports.ChatCapabilityResume: true}
}
func (*reviewerConfigConversation) SendTurn(context.Context, ports.ChatUserMessage) (ports.ChatTurnRef, error) {
	return ports.ChatTurnRef{}, nil
}
func (*reviewerConfigConversation) Interrupt(context.Context, string) error { return nil }
func (*reviewerConfigConversation) ResolveRequest(context.Context, string, ports.ChatDecision) error {
	return nil
}
func (c *reviewerConfigConversation) Events() <-chan ports.ChatEvent { return c.events }
func (c *reviewerConfigConversation) Close() error {
	c.once.Do(func() { close(c.events) })
	return nil
}

type reviewerConfigDriver struct{ conversation *reviewerConfigConversation }

func (reviewerConfigDriver) Harness() domain.AgentHarness { return domain.HarnessCodex }
func (d reviewerConfigDriver) Probe(context.Context) (ports.ChatCapabilities, error) {
	return d.conversation.Capabilities(), nil
}
func (d reviewerConfigDriver) Start(context.Context, ports.ChatStartConfig) (ports.ChatConversation, error) {
	return d.conversation, nil
}
func (d reviewerConfigDriver) Resume(context.Context, ports.ChatResumeConfig) (ports.ChatConversation, error) {
	return d.conversation, nil
}

type reviewerConfigRegistry struct{ driver ports.ChatDriver }

func (r reviewerConfigRegistry) Driver(domain.AgentHarness) (ports.ChatDriver, error) {
	return r.driver, nil
}
func (reviewerConfigRegistry) SupportsChat(domain.AgentHarness) bool { return true }

type reviewerConfigManager struct{ calls int }

func (m *reviewerConfigManager) PersistChatAgentConfig(context.Context, domain.SessionID, string, string) error {
	m.calls++
	return errors.New("review owner must not update a worker session")
}

// A review controller is addressed by review.ID, even though its conversation
// belongs to a worker session. Exercise the production callback with the real
// conversation/session tables: a valid review choice must remain live, durable,
// and independent of both the worker metadata and launch-history snapshot.
func TestReviewerChatConfigurationUsesDurableOwnerWithInstructorOffAndOn(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		t.Run(fmt.Sprintf("instructor-guard=%t", guarded), func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			store := sqlitetest.MustOpenAt(t, dir)
			now := time.Now().UTC()
			if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: dir, RegisteredAt: now}); err != nil {
				t.Fatal(err)
			}
			workerRecord, err := store.CreateSession(ctx, domain.SessionRecord{
				ID: "worker", ProjectID: "project", Kind: domain.KindWorker,
				Harness: domain.HarnessOpenCode, Mode: domain.SessionModeChat,
				Metadata:  domain.SessionMetadata{Model: "worker-model", Effort: "worker-effort"},
				CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.UpsertReview(ctx, domain.Review{
				ID: "review", SessionID: workerRecord.ID, ProjectID: "project", Harness: domain.ReviewerCodex,
				InterfaceMode:     domain.ReviewerInterfaceChat,
				LaunchAgentConfig: &domain.AgentConfig{Model: "launch-model", Effort: "high"},
				CreatedAt:         now, UpdatedAt: now,
			}); err != nil {
				t.Fatal(err)
			}
			manager := &reviewerConfigManager{}
			conversation := &reviewerConfigConversation{events: make(chan ports.ChatEvent)}
			sequence := 0
			service := chatsvc.New(chatsvc.Options{
				Store: store, Sessions: store,
				Drivers: reviewerConfigRegistry{driver: reviewerConfigDriver{conversation: conversation}},
				NewID:   func() string { sequence++; return fmt.Sprintf("review-config-%d", sequence) },
				OnAgentConfigChanged: func(id domain.SessionID, model, effort string) error {
					return persistChatAgentConfig(ctx, store, manager, id, model, effort)
				},
			})
			owner := domain.ReviewConversationOwner("review")
			t.Cleanup(func() { _ = service.StopForOwner(ctx, owner) })
			guardCalls := 0
			if guarded {
				service.SetAgentConfigValidator(func(_ context.Context, id domain.SessionID, model, effort string) error {
					guardCalls++
					if id != "review" || model != "selected-review-model" || effort != "ultra" {
						return errors.New("review choice violates configured role")
					}
					return nil
				})
			}
			controller, err := service.Start(ctx, chatsvc.StartConfig{
				Owner: owner, SessionID: workerRecord.ID, ProjectID: "project", Harness: domain.HarnessCodex,
				WorkspacePath: dir, Model: "launch-model", Effort: "high", ReadOnly: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			settings, err := service.SetTurnSettings(ctx, "review", domain.ConversationSettings{Model: "selected-review-model", ReasoningEffort: "ultra"})
			if err != nil {
				t.Fatalf("valid reviewer selection killed the controller: %v", err)
			}
			if settings.Model != "selected-review-model" || settings.ReasoningEffort != "ultra" {
				t.Fatalf("selected settings lost: %+v", settings)
			}
			live, err := service.ControllerForOwner(owner)
			if err != nil || live != controller || live.State() == ports.ChatControllerStopped {
				t.Fatalf("reviewer controller did not stay live: %v", err)
			}
			durable, err := store.ConversationForReview(ctx, "review")
			if err != nil || durable.Settings.Model != settings.Model || durable.Settings.ReasoningEffort != settings.ReasoningEffort {
				t.Fatalf("review choice was not durable: %+v, %v", durable.Settings, err)
			}
			worker, found, err := store.GetSession(ctx, workerRecord.ID)
			if err != nil || !found || worker.Metadata.Model != "worker-model" || worker.Metadata.Effort != "worker-effort" || manager.calls != 0 {
				t.Fatalf("review selection changed worker metadata: %+v, manager calls=%d, err=%v", worker.Metadata, manager.calls, err)
			}
			review, found, err := store.GetReviewByID(ctx, "review")
			if err != nil || !found || review.LaunchAgentConfig == nil || review.LaunchAgentConfig.Model != "launch-model" || review.LaunchAgentConfig.Effort != "high" {
				t.Fatalf("historical successful launch was rewritten: %+v, %v", review.LaunchAgentConfig, err)
			}
			if guarded && guardCalls != 1 {
				t.Fatalf("review selection skipped configured guard: calls=%d", guardCalls)
			}
			if err := persistChatAgentConfig(ctx, store, manager, "review", "unpersisted", "minimal"); err == nil {
				t.Fatal("review callback accepted settings absent from durable conversation")
			}
		})
	}
}

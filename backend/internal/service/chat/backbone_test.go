package chat_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/backbone"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
)

// A newly bound work must not adopt an old unbound controller simply because it
// has the same session ID. Exercise the actual queued dispatch boundary.
func TestBackboneRebindCancelsQueuedTurnBeforeNativeSend(t *testing.T) {
	h := newHarness(t)
	var changed atomic.Bool
	h.svc.SetBackboneValidator(func(_ context.Context, id domain.SessionID, binding *ports.ChatBackboneConfig) error {
		if id != testSession {
			t.Errorf("wrong session %s", id)
		}
		if changed.Load() {
			if binding != nil {
				t.Error("unbound controller acquired a binding")
			}
			return errors.New("binding changed; fresh controlled process required")
		}
		return nil
	})
	if _, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	queued, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{Text: "queued"})
	if err != nil || queued.State != domain.TurnStateQueued {
		t.Fatalf("queue: %+v %v", queued, err)
	}
	changed.Store(true)
	h.conv.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: "provider-turn-1", TurnState: domain.TurnStateCompleted}
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for {
		turn, err := h.st.TurnByID(context.Background(), queued.ID)
		if err == nil && turn.State == domain.TurnStateCancelled {
			break
		}
		select {
		case <-timeout.C:
			t.Fatalf("queue was not cancelled: %+v %v", turn, err)
		case <-poll.C:
		}
	}
	h.conv.mu.Lock()
	defer h.conv.mu.Unlock()
	if h.conv.sendCalls != 1 {
		t.Fatalf("stale native sends=%d", h.conv.sendCalls)
	}
}

func TestBackboneRebindRejectsHistoryProviderWork(t *testing.T) {
	for _, operation := range []string{"edit", "activate", "fork"} {
		t.Run(operation, func(t *testing.T) {
			h := newHarness(t)
			refused := errors.New("work now requires a different Backbone binding")
			h.svc.SetBackboneValidator(func(context.Context, domain.SessionID, *ports.ChatBackboneConfig) error { return refused })
			var err error
			switch operation {
			case "edit":
				_, err = h.svc.EditMessage(context.Background(), testSession, "turn", ports.ChatUserMessage{Text: "edited"})
			case "activate":
				_, err = h.svc.ActivateBranch(context.Background(), testSession, "branch")
			case "fork":
				_, err = h.svc.ForkConversation(context.Background(), testSession)
			}
			if !errors.Is(err, refused) {
				t.Fatalf("history operation did not check current work binding: %v", err)
			}
			h.conv.mu.Lock()
			defer h.conv.mu.Unlock()
			if h.conv.sendCalls != 0 || h.hostStops.Load() != 0 {
				t.Fatal("history rejection reached provider work")
			}
		})
	}
}

func TestBackboneRebindAfterPreparationBlocksDeferredSubmission(t *testing.T) {
	var changed atomic.Bool
	var nativeStarts atomic.Int32
	conv := &deferredConversation{fakeConversation: newFakeConversation(), start: func(string) error {
		nativeStarts.Add(1)
		return nil
	}}
	conv.onSend = func(string) { changed.Store(true) }
	h := newHarnessWithConversation(t, conv)
	h.svc.SetBackboneValidator(func(context.Context, domain.SessionID, *ports.ChatBackboneConfig) error {
		if changed.Load() {
			return errors.New("policy rebound after ACP preparation")
		}
		return nil
	})
	if _, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{Text: "prepared message", ClientMessageID: "deferred-binding"}); !errors.Is(err, ports.ErrSemanticDeliveryRejected) {
		t.Fatalf("stale deferred submission was not rejected: %v", err)
	}
	if nativeStarts.Load() != 0 {
		t.Fatal("changed binding reached native deferred submission")
	}
}

func TestBackboneWorkerBindingDoesNotReplaceIndependentReviewerAuthority(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	now := time.Now().UTC()
	const reviewID = "backbone-independent-review"
	if err := st.UpsertReview(ctx, domain.Review{ID: reviewID, SessionID: testSession, ProjectID: testProject, Harness: domain.ReviewerCodex, InterfaceMode: domain.ReviewerInterfaceChat, ReviewerActivityState: domain.ActivityActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	conv := newFakeConversation()
	var nextID atomic.Int32
	svc := chatsvc.New(chatsvc.Options{Store: st, Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: conv}}, Log: slog.New(slog.DiscardHandler), NewID: func() string { return fmt.Sprintf("review-backbone-%d", nextID.Add(1)) }})
	t.Cleanup(func() { svc.StopAll(context.Background()) })
	svc.SetBackboneValidator(func(context.Context, domain.SessionID, *ports.ChatBackboneConfig) error {
		return errors.New("worker requires its OpenCode binding")
	})
	owner := domain.ReviewConversationOwner(reviewID)
	if _, err := svc.Start(ctx, chatsvc.StartConfig{Owner: owner, SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, WorkspacePath: t.TempDir()}); err != nil {
		t.Fatalf("independent reviewer inherited worker selection: %v", err)
	}
	if _, err := svc.SendForOwner(ctx, owner, ports.ChatUserMessage{Text: "review the approved diff"}); err != nil {
		t.Fatalf("independent reviewer dispatch inherited worker selection: %v", err)
	}
}

type backboneConfigConversation struct {
	*fakeConversation
	lists atomic.Int32
	sets  atomic.Int32
}

func (c *backboneConfigConversation) ListConfigOptions(context.Context) ([]ports.ChatConfigOption, error) {
	c.lists.Add(1)
	return nil, nil
}
func (c *backboneConfigConversation) SetConfigOption(context.Context, string, ports.ChatConfigOptionValue) ([]ports.ChatConfigOption, error) {
	c.sets.Add(1)
	return nil, nil
}

// Real producer bytes cross the service port; the fake driver proves plumbing
// and mutation refusal only, never native model loading or qualification.
func TestBackboneProducerContextReachesActualChatPortAndCannotMutate(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "..", "policy", "backbone", "testdata", "producer"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	git := func(cwd string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", append([]string{"--no-replace-objects", "--no-lazy-fetch", "-c", "core.hooksPath=" + filepath.Join(root, "nohooks")}, args...)...)
		cmd.Dir = cwd
		for _, value := range os.Environ() {
			if !strings.HasPrefix(strings.ToUpper(value), "GIT_") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture Git: %v: %s", err, output)
		}
	}
	git(root, "clone", "--no-checkout", filepath.Join(fixture, "workspace.bundle"), workspace)
	git(workspace, "-c", "core.autocrlf=false", "checkout", "--detach", "c77601c5fcd96188ddeca71b3cc9429a2d692a51")
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var request struct {
		TaskPacket string `json:"task_packet"`
	}
	if err := json.Unmarshal(read(filepath.Join(fixture, "request.json")), &request); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(read(filepath.Join(workspace, ".engineering-backbone", "opencode.json")))
	raw, err := json.Marshal(backbone.Binding{SchemaVersion: "1", AdapterSHA256: hex.EncodeToString(sum[:]), Contexts: map[string]backbone.RoleBinding{"worker": {Role: "implementer", TaskPacket: request.TaskPacket, Context: read(filepath.Join(fixture, "context.json"))}}})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := backbone.ValidateBinding(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := backbone.Verify(context.Background(), workspace, parsed, "worker")
	if err != nil {
		t.Fatal(err)
	}
	st := openStore(t)
	conv := &backboneConfigConversation{fakeConversation: newFakeConversation()}
	var captured ports.ChatStartConfig
	var probes atomic.Int32
	var identifiers atomic.Int32
	svc := chatsvc.New(chatsvc.Options{NewID: func() string { return fmt.Sprintf("backbone-%d", identifiers.Add(1)) }, Store: st, Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: conv, startCfg: &captured, probe: func() error { probes.Add(1); return errors.New("uncontrolled native probe") }}}, Log: slog.New(slog.DiscardHandler)})
	t.Cleanup(func() { _ = svc.Stop(context.Background(), testSession) })
	cfg := chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessOpenCode, WorkspacePath: workspace, Model: "test/model", Effort: "high", Permissions: ports.PermissionModeDefault, SystemPrompt: "AO standing prompt\n\n" + verified.Prompt, Backbone: &ports.ChatBackboneConfig{Binding: raw, InstructorRole: "worker", PolicyVersion: strings.Repeat("a", 64), Model: "test/model", Effort: "high"}}
	if _, err := svc.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 0 || captured.Backbone == nil || !strings.HasSuffix(captured.SystemPrompt, verified.Prompt) {
		t.Fatal("controlled context bypassed or lost at native port")
	}
	before := conv.lists.Load()
	if _, err := svc.SetConfigOption(context.Background(), testSession, "mode", ports.ChatConfigOptionValue{Select: "ao-bypass"}); !errors.Is(err, ports.ErrChatConfigOptionInvalid) {
		t.Fatalf("mode mutation: %v", err)
	}
	if _, err := svc.SetTurnSettings(context.Background(), testSession, domain.ConversationSettings{Model: "other/model"}); !errors.Is(err, ports.ErrChatConfigOptionInvalid) {
		t.Fatalf("model mutation: %v", err)
	}
	if conv.sets.Load() != 0 || conv.lists.Load() != before {
		t.Fatal("governed mutation reached native configuration")
	}
	if _, err := svc.Start(context.Background(), cfg); err == nil {
		t.Fatal("live controller adoption accepted")
	}
	if err := os.WriteFile(filepath.Join(workspace, ".opencode", "skills", "task-intake", "SKILL.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Send(context.Background(), testSession, ports.ChatUserMessage{Text: "run with changed context"}); !errors.Is(err, ports.ErrSemanticDeliveryRejected) {
		t.Fatalf("stale context dispatched: %v", err)
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if conv.sendCalls != 0 {
		t.Fatal("stale context reached provider")
	}
}

package chat_test

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

// A reviewer has its own controller generation even while its worker is active.
// Exercise the real archive/projection transaction through both live controllers.
func TestReviewerChatProjectsItsOwnLifecycleWithoutChangingWorker(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	worker, found, err := st.GetSession(ctx, testSession)
	if err != nil || !found {
		t.Fatalf("GetSession: found=%v err=%v", found, err)
	}
	worker.Kind = domain.KindWorker
	if err := st.UpdateSession(ctx, worker); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	const reviewID = "review-lifecycle"
	now := time.Now().UTC()
	if err := st.UpsertReview(ctx, domain.Review{
		ID: reviewID, SessionID: testSession, ProjectID: testProject,
		Harness: domain.ReviewerCodex, InterfaceMode: domain.ReviewerInterfaceChat,
		ReviewerActivityState: domain.ActivityActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertReview: %v", err)
	}
	workerProvider, reviewerProvider := newFakeConversation(), newFakeConversation()
	workerProvider.providerConversationID = "worker-native-thread"
	reviewerProvider.providerConversationID = "reviewer-native-thread"
	workerActivity := &recordingActivity{}
	var nextID atomic.Int64
	svc := chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st, Reader: fullSnapshotReader(st),
		Activity: workerActivity,
		Drivers: fakeRegistry{driver: fakeDriver{start: func(cfg ports.ChatStartConfig) (ports.ChatConversation, error) {
			switch cfg.SessionID {
			case testSession:
				return workerProvider, nil
			case domain.SessionID("review-" + reviewID):
				if !cfg.ReadOnly {
					return nil, fmt.Errorf("reviewer host must be read-only")
				}
				return reviewerProvider, nil
			default:
				return nil, fmt.Errorf("unexpected provider host %q", cfg.SessionID)
			}
		}}},
		Log:   slog.New(slog.DiscardHandler),
		NewID: func() string { return fmt.Sprintf("review-lifecycle-%d", nextID.Add(1)) },
	})
	t.Cleanup(func() { svc.StopAll(context.Background()) })
	if _, err := svc.Start(ctx, chatsvc.StartConfig{
		SessionID: testSession, ProjectID: testProject, Kind: domain.KindWorker,
		Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(),
	}); err != nil {
		t.Fatalf("Start worker: %v", err)
	}
	workerController, err := svc.Controller(testSession)
	if err != nil {
		t.Fatalf("worker Controller: %v", err)
	}
	workerTurn, err := svc.Send(ctx, testSession, ports.ChatUserMessage{
		Text: "worker remains active", ClientMessageID: "worker-lifecycle-input", Origin: domain.MessageOriginHuman,
	})
	if err != nil {
		t.Fatalf("Send worker: %v", err)
	}
	workerProvider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: workerTurn.ProviderTurnID, ProviderEventID: "worker-start"})
	workerStartDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(workerStartDeadline) {
		if signals := workerActivity.snapshot(); len(signals) > 0 && signals[len(signals)-1].State == domain.ActivityActive {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if signals := workerActivity.snapshot(); len(signals) == 0 || signals[len(signals)-1].State != domain.ActivityActive {
		t.Fatal("fixture worker did not become natively active")
	}
	workerBefore := awaitStoreSnapshot(t, st, workerController.ConversationID(), func(s store.ConversationSnapshot) bool {
		return len(s.Turns) == 1 && s.Turns[0].State == domain.TurnStateRunning
	})
	workerRecordBefore, found, err := st.GetSession(ctx, testSession)
	if err != nil || !found {
		t.Fatalf("worker record before review: found=%v err=%v", found, err)
	}
	workerStateBefore := workerController.State()
	workerSignalsBefore := workerActivity.snapshot()
	owner := domain.ReviewConversationOwner(reviewID)
	if _, err := svc.Start(ctx, chatsvc.StartConfig{
		Owner: owner, SessionID: testSession, ProjectID: testProject,
		Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(),
	}); err != nil {
		t.Fatalf("Start reviewer: %v", err)
	}
	reviewerController, err := svc.ControllerForOwner(owner)
	if err != nil {
		t.Fatalf("reviewer ControllerForOwner: %v", err)
	}
	if reviewerController.Generation() == workerController.Generation() || reviewerController.ConversationID() == workerController.ConversationID() {
		t.Fatal("reviewer reused the worker controller generation or conversation")
	}
	review, found, err := st.GetReviewByID(ctx, reviewID)
	if err != nil || !found || review.ControllerGeneration != reviewerController.Generation() {
		t.Fatalf("review controller identity was not persisted: review=%+v found=%v err=%v", review, found, err)
	}
	reviewerTurn, err := svc.SendForOwner(ctx, owner, ports.ChatUserMessage{
		Text: "review the immutable worker snapshot", ClientMessageID: "reviewer-lifecycle-input", Origin: domain.MessageOriginHuman,
	})
	if err != nil {
		t.Fatalf("Send reviewer: %v", err)
	}
	// Deliberately reuse the provider-local turn ID: durable owner scope must
	// distinguish the reviewer events from the still-running worker turn.
	if reviewerTurn.ProviderTurnID != workerTurn.ProviderTurnID {
		t.Fatal("fixture did not exercise equal provider-local turn IDs across owners")
	}
	reviewerProvider.emit(
		ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: reviewerTurn.ProviderTurnID, ProviderEventID: "review-start"},
		ports.ChatEvent{Kind: ports.ChatEventMessageDelta, ProviderTurnID: reviewerTurn.ProviderTurnID, ProviderItemID: "review-answer", ProviderEventID: "review-delta", Delta: "Independent review "},
		ports.ChatEvent{Kind: ports.ChatEventActivityStarted, ProviderTurnID: reviewerTurn.ProviderTurnID, ProviderItemID: "review-command", ProviderEventID: "review-tool-start", ActivityKind: domain.ActivityKindCommand, ActivityStatus: domain.ActivityStatusRunning, Summary: "inspect frozen diff"},
		ports.ChatEvent{Kind: ports.ChatEventActivityCompleted, ProviderTurnID: reviewerTurn.ProviderTurnID, ProviderItemID: "review-command", ProviderEventID: "review-tool-end", ActivityKind: domain.ActivityKindCommand, ActivityStatus: domain.ActivityStatusCompleted, Summary: "inspect frozen diff", Detail: []byte(`{"exitCode":0}`)},
		ports.ChatEvent{Kind: ports.ChatEventMessageCompleted, ProviderTurnID: reviewerTurn.ProviderTurnID, ProviderItemID: "review-answer", ProviderEventID: "review-answer-end", Text: "Independent review complete."},
		ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: reviewerTurn.ProviderTurnID, ProviderEventID: "review-end", TurnState: domain.TurnStateCompleted},
	)
	awaitStoreSnapshot(t, st, reviewerController.ConversationID(), func(s store.ConversationSnapshot) bool {
		return len(s.Turns) == 1 && s.Turns[0].State == domain.TurnStateCompleted && len(s.Messages) == 2 && len(s.Activities) == 1
	})
	snapshot, err := svc.SnapshotForReview(ctx, reviewID)
	if err != nil {
		t.Fatalf("SnapshotForReview: %v", err)
	}
	if snapshot.Conversation.Scope != domain.ConversationScopeReview || snapshot.Conversation.ID != reviewerController.ConversationID() {
		t.Fatalf("review snapshot lost its owner scope: %+v", snapshot.Conversation)
	}
	if snapshot.Conversation.ReviewID != reviewID {
		t.Errorf("review snapshot omitted its typed reviewer identity: %+v", snapshot.Conversation)
	}
	if snapshot.Turns[0].ID != reviewerTurn.ID || snapshot.Turns[0].HandledByReviewID != reviewID || snapshot.Turns[0].State != domain.TurnStateCompleted {
		t.Errorf("review turn lifecycle = %+v", snapshot.Turns[0])
	}
	assistant := snapshot.Messages[1]
	if assistant.Role != domain.MessageRoleAssistant || assistant.Text != "Independent review complete." || assistant.Streaming || assistant.Revision == 0 {
		t.Errorf("durable reviewer message = %+v", assistant)
	}
	activity := snapshot.Activities[0]
	if activity.Kind != domain.ActivityKindCommand || activity.Status != domain.ActivityStatusCompleted || activity.Summary != "inspect frozen diff" || activity.Revision == 0 {
		t.Errorf("durable reviewer tool activity = %+v", activity)
	}
	archived, err := st.ProviderEventsSince(ctx, reviewerController.ConversationID(), 0, 20)
	if err != nil || len(archived) != 6 {
		t.Errorf("reviewer provider archive: events=%d err=%v", len(archived), err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		review, found, err = st.GetReviewByID(ctx, reviewID)
		if err != nil || !found {
			t.Fatalf("review activity readback: found=%v err=%v", found, err)
		}
		if review.ReviewerActivityState == domain.ActivityIdle {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if review.ReviewerActivityState != domain.ActivityIdle {
		t.Errorf("reviewer activity = %q, want idle after its own completed turn", review.ReviewerActivityState)
	}
	workerAfter, err := st.LoadConversationSnapshot(ctx, workerController.ConversationID())
	if err != nil || !reflect.DeepEqual(workerBefore, workerAfter) {
		t.Errorf("reviewer lifecycle changed the worker conversation: before=%+v after=%+v err=%v", workerBefore, workerAfter, err)
	}
	workerRecordAfter, found, err := st.GetSession(ctx, testSession)
	if err != nil || !found || !reflect.DeepEqual(workerRecordBefore, workerRecordAfter) || workerController.State() != workerStateBefore {
		t.Errorf("reviewer lifecycle changed worker state: before=%+v after=%+v controller=%q want=%q found=%v err=%v", workerRecordBefore, workerRecordAfter, workerController.State(), workerStateBefore, found, err)
	}
	if !reflect.DeepEqual(workerSignalsBefore, workerActivity.snapshot()) {
		t.Errorf("reviewer lifecycle emitted worker activity signals: before=%+v after=%+v", workerSignalsBefore, workerActivity.snapshot())
	}
}

// Cold attachment imports a settled root turn without sending a replacement
// prompt or leaving the durable reviewer activity at its prior active state.
func TestReviewerChatColdHistorySettlesItsOwnLifecycleWithoutSending(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	now := time.Now().UTC()
	const (
		reviewID        = "review-cold-history"
		conversationID  = "review-cold-conversation"
		providerID      = "review-cold-native-thread"
		priorGeneration = "review-cold-prior-generation"
		nativeTurnID    = "review-cold-native-turn"
		turnID          = "review-cold-turn"
	)
	if err := st.UpsertReview(ctx, domain.Review{
		ID: reviewID, SessionID: testSession, ProjectID: testProject,
		Harness: domain.ReviewerCodex, InterfaceMode: domain.ReviewerInterfaceChat,
		ReviewerActivityState: domain.ActivityActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertReview: %v", err)
	}
	if _, err := st.CreateReviewConversation(ctx, conversationID, reviewID, testProject, testSession, now); err != nil {
		t.Fatalf("CreateReviewConversation: %v", err)
	}
	if claimed, err := st.ClaimReviewChatController(ctx, reviewID, providerID, priorGeneration, now); err != nil || !claimed {
		t.Fatalf("seed prior reviewer controller: claimed=%v err=%v", claimed, err)
	}
	if err := st.AdoptReviewProviderTurn(ctx, conversationID, testSession, reviewID, priorGeneration, turnID, nativeTurnID, now); err != nil {
		t.Fatalf("seed running reviewer turn: %v", err)
	}
	if err := st.AppendImportedUserMessage(ctx, conversationID, nativeTurnID, domain.ConversationMessage{
		ID: "review-cold-user", Text: "review the retained snapshot", Origin: domain.MessageOriginProvider,
	}, now); err != nil {
		t.Fatalf("seed retained reviewer prompt: %v", err)
	}
	workerBefore, found, err := st.GetSession(ctx, testSession)
	if err != nil || !found {
		t.Fatalf("worker before cold attach: found=%v err=%v", found, err)
	}
	provider := &nativeHistoryConversation{
		fakeConversation: newFakeConversation(),
		events: []ports.ChatEvent{
			{Kind: ports.ChatEventTurnStarted, ProviderTurnID: nativeTurnID, ProviderEventID: "cold-root-start"},
			{Kind: ports.ChatEventUserMessageCompleted, ProviderTurnID: nativeTurnID, ProviderItemID: "cold-native-user", ProviderEventID: "cold-user", Text: "review the retained snapshot"},
			{Kind: ports.ChatEventMessageCompleted, ProviderTurnID: nativeTurnID, ProviderItemID: "cold-answer", ProviderEventID: "cold-message", Text: "Retained independent review complete."},
			{Kind: ports.ChatEventActivityCompleted, ProviderTurnID: nativeTurnID, ProviderItemID: "cold-command", ProviderEventID: "cold-tool", ActivityKind: domain.ActivityKindCommand, ActivityStatus: domain.ActivityStatusCompleted, Summary: "inspect retained diff", Detail: []byte(`{"exitCode":0}`)},
			{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: nativeTurnID, ProviderEventID: "cold-root-end", TurnState: domain.TurnStateCompleted},
		},
	}
	provider.providerConversationID = providerID
	var nextID atomic.Int64
	svc := chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st, Reader: fullSnapshotReader(st),
		Drivers:  fakeRegistry{driver: fakeDriver{conv: provider}},
		Activity: &recordingActivity{}, Log: slog.New(slog.DiscardHandler),
		NewID: func() string { return fmt.Sprintf("review-cold-%d", nextID.Add(1)) },
	})
	t.Cleanup(func() { svc.StopAll(context.Background()) })
	controller, err := svc.Start(ctx, chatsvc.StartConfig{
		Owner: domain.ReviewConversationOwner(reviewID), SessionID: testSession, ProjectID: testProject,
		Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(),
		ProviderConversationID: providerID, HistoryMode: ports.ChatHistoryRequired,
	})
	if err != nil {
		t.Fatalf("Start cold reviewer: %v", err)
	}
	if controller.Generation() == priorGeneration || controller.ConversationID() != conversationID {
		t.Fatalf("cold reviewer did not replace only its generation: generation=%q conversation=%q", controller.Generation(), controller.ConversationID())
	}
	snapshot, err := svc.SnapshotForReview(ctx, reviewID)
	if err != nil {
		t.Fatalf("SnapshotForReview: %v", err)
	}
	if snapshot.Conversation.Scope != domain.ConversationScopeReview || snapshot.Conversation.ReviewID != reviewID {
		t.Errorf("cold reviewer snapshot omitted its typed reviewer identity: %+v", snapshot.Conversation)
	}
	if len(snapshot.Turns) != 1 || snapshot.Turns[0].ID != turnID || snapshot.Turns[0].State != domain.TurnStateCompleted || snapshot.Turns[0].HandledByReviewID != reviewID {
		t.Errorf("cold reviewer turn was not durably settled: %+v", snapshot.Turns)
	}
	if len(snapshot.Messages) != 2 || snapshot.Messages[1].Text != "Retained independent review complete." || snapshot.Messages[1].Streaming {
		t.Errorf("cold reviewer messages = %+v", snapshot.Messages)
	}
	if len(snapshot.Activities) != 1 || snapshot.Activities[0].Status != domain.ActivityStatusCompleted || snapshot.Activities[0].Summary != "inspect retained diff" {
		t.Errorf("cold reviewer tool = %+v", snapshot.Activities)
	}
	review, found, err := st.GetReviewByID(ctx, reviewID)
	if err != nil || !found || review.ControllerGeneration != controller.Generation() || review.ReviewerActivityState != domain.ActivityIdle {
		t.Errorf("cold reviewer activity did not settle under its new owner: review=%+v found=%v err=%v", review, found, err)
	}
	if provider.historyReads() != 1 || provider.sendCallCount() != 0 {
		t.Errorf("cold history replay caused provider work: historyReads=%d sendCalls=%d", provider.historyReads(), provider.sendCallCount())
	}
	workerAfter, found, err := st.GetSession(ctx, testSession)
	if err != nil || !found || !reflect.DeepEqual(workerBefore, workerAfter) {
		t.Errorf("cold reviewer replay changed the worker: before=%+v after=%+v found=%v err=%v", workerBefore, workerAfter, found, err)
	}
}

// A retained failed turn has immutable error text, while credential recovery is
// a separate durable account fact. A terminal replay cannot clear that demand.
func TestReviewerChatColdHistoryKeepsReauthDemandUntilExplicitRecovery(t *testing.T) {
	for _, tc := range []struct {
		name         string
		reauth       bool
		recovered    bool
		typedFailure bool
		want         domain.ActivityState
	}{
		{"unresolved_auth", true, false, false, domain.ActivityWaitingInput},
		{"explicit_auth_recovery", true, true, false, domain.ActivityIdle},
		{"ordinary_provider_failure", false, false, false, domain.ActivityIdle},
		{"typed_auth_terminal", true, false, true, domain.ActivityWaitingInput},
		{"typed_auth_terminal_then_explicit_recovery", true, true, true, domain.ActivityIdle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st := openStore(t)
			now := time.Now().UTC()
			const (
				reviewID        = "review-auth-history"
				siblingReviewID = "review-auth-history-sibling"
				conversationID  = "review-auth-history-conversation"
				providerID      = "review-auth-history-native-thread"
				priorGeneration = "review-auth-history-prior-generation"
				turnID          = "review-auth-history-turn"
				nativeTurnID    = "review-auth-history-native-turn"
				prompt          = "review the retained immutable snapshot"
			)
			originalErr := fmt.Errorf("provider RPC failed; sign in again text is not an auth sentinel")
			if tc.reauth {
				originalErr = fmt.Errorf("%w: reviewer login expired", ports.ErrChatAuthRequired)
			}
			for _, seed := range []struct {
				id, provider, generation string
				harness                  domain.ReviewerHarness
			}{
				{reviewID, providerID, priorGeneration, domain.ReviewerCodex},
				{siblingReviewID, "sibling-auth-native-thread", "sibling-auth-current-generation", domain.ReviewerClaudeCode},
			} {
				if err := st.UpsertReview(ctx, domain.Review{
					ID: seed.id, SessionID: testSession, ProjectID: testProject,
					Harness: seed.harness, InterfaceMode: domain.ReviewerInterfaceChat,
					ReviewerActivityState: domain.ActivityActive, CreatedAt: now, UpdatedAt: now,
				}); err != nil {
					t.Fatal(err)
				}
				if claimed, err := st.ClaimReviewChatController(ctx, seed.id, seed.provider, seed.generation, now); err != nil || !claimed {
					t.Fatalf("claim reviewer %s: claimed=%v err=%v", seed.id, claimed, err)
				}
			}
			if _, err := st.CreateReviewConversation(ctx, conversationID, reviewID, testProject, testSession, now); err != nil {
				t.Fatal(err)
			}
			if err := st.AdoptReviewProviderTurn(ctx, conversationID, testSession, reviewID, priorGeneration, turnID, nativeTurnID, now); err != nil {
				t.Fatal(err)
			}
			if err := st.AppendImportedUserMessage(ctx, conversationID, nativeTurnID, domain.ConversationMessage{
				ID: "auth-history-user", Text: prompt, Origin: domain.MessageOriginProvider,
			}, now); err != nil {
				t.Fatal(err)
			}
			if err := st.SettleTurn(ctx, conversationID, nativeTurnID, domain.TurnStateFailed, originalErr.Error(), now); err != nil {
				t.Fatal(err)
			}
			if tc.reauth && !tc.typedFailure {
				if err := st.RecordAccount(ctx, conversationID, domain.ConversationAccount{
					ReauthRequiredAt: &now, ReauthReason: originalErr.Error(),
				}, now); err != nil {
					t.Fatal(err)
				}
				if updated, err := st.UpdateReviewChatActivity(ctx, reviewID, priorGeneration, domain.ActivityWaitingInput, now); err != nil || !updated {
					t.Fatalf("seed waiting_input: updated=%v err=%v", updated, err)
				}
			}
			workerBefore, found, err := st.GetSession(ctx, testSession)
			if err != nil || !found {
				t.Fatalf("worker before replay: found=%v err=%v", found, err)
			}
			siblingBefore, found, err := st.GetReviewByID(ctx, siblingReviewID)
			if err != nil || !found {
				t.Fatalf("sibling before replay: found=%v err=%v", found, err)
			}
			provider := &nativeHistoryConversation{
				fakeConversation: newFakeConversation(),
				events: []ports.ChatEvent{
					{Kind: ports.ChatEventTurnStarted, ProviderTurnID: nativeTurnID, ProviderEventID: "auth-history-root-start"},
					{Kind: ports.ChatEventUserMessageCompleted, ProviderTurnID: nativeTurnID, ProviderItemID: "auth-history-native-user", ProviderEventID: "auth-history-user", Text: prompt},
					{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: nativeTurnID, ProviderEventID: "auth-history-root-end", TurnState: domain.TurnStateRecovered},
				},
			}
			if tc.typedFailure {
				// The replay itself raises the auth demand; no persisted account was
				// seeded above. A later explicit recovery must be authoritative.
				provider.events[2].TurnState = domain.TurnStateFailed
				provider.events[2].Err = originalErr
			}
			if tc.recovered {
				provider.events = append(provider.events, ports.ChatEvent{
					Kind: ports.ChatEventAccountChanged, ProviderEventID: "auth-history-account-recovered",
					Account: &ports.ChatAccount{ReauthRecovered: true},
				})
			}
			provider.providerConversationID = providerID
			workerActivity := &recordingActivity{}
			var nextID atomic.Int64
			svc := chatsvc.New(chatsvc.Options{
				Store: st, Sessions: st, Reader: fullSnapshotReader(st),
				Drivers:  fakeRegistry{driver: fakeDriver{conv: provider}},
				Activity: workerActivity, Log: slog.New(slog.DiscardHandler),
				NewID: func() string { return fmt.Sprintf("auth-history-%d", nextID.Add(1)) },
			})
			t.Cleanup(func() { svc.StopAll(context.Background()) })
			controller, err := svc.Start(ctx, chatsvc.StartConfig{
				Owner: domain.ReviewConversationOwner(reviewID), SessionID: testSession, ProjectID: testProject,
				Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(),
				ProviderConversationID: providerID, HistoryMode: ports.ChatHistoryRequired,
			})
			if err != nil {
				t.Fatalf("cold reviewer attachment: %v", err)
			}
			snapshot, err := svc.SnapshotForReview(ctx, reviewID)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Conversation.Scope != domain.ConversationScopeReview || snapshot.Conversation.ReviewID != reviewID || controller.ConversationID() != conversationID {
				t.Fatalf("replay changed reviewer conversation identity: %+v", snapshot.Conversation)
			}
			if len(snapshot.Turns) != 1 || snapshot.Turns[0].ID != turnID || snapshot.Turns[0].ProviderTurnID != nativeTurnID || snapshot.Turns[0].HandledByReviewID != reviewID || snapshot.Turns[0].State != domain.TurnStateFailed || snapshot.Turns[0].ErrorMessage != originalErr.Error() {
				t.Fatalf("retained failed turn or original error changed: %+v", snapshot.Turns)
			}
			account := snapshot.Conversation.Account
			reauthRequired := account != nil && account.ReauthRequiredAt != nil
			if reauthRequired != (tc.reauth && !tc.recovered) {
				t.Fatalf("durable account demand: %+v", account)
			}
			if reauthRequired && account.ReauthReason != originalErr.Error() {
				t.Errorf("retained reauth reason changed: %+v", account)
			}
			if tc.recovered && account.ReauthReason != "" {
				t.Errorf("explicit recovery left a stale reason: %+v", account)
			}
			review, found, err := st.GetReviewByID(ctx, reviewID)
			if err != nil || !found || review.ControllerGeneration != controller.Generation() || controller.Generation() == priorGeneration {
				t.Fatalf("review owner generation mismatch: review=%+v found=%v err=%v", review, found, err)
			}
			t.Logf("durableReauthRequired=%v reviewerActivity=%s failedTurnError=%q nativeHistoryReads=%d providerSendCalls=%d", reauthRequired, review.ReviewerActivityState, snapshot.Turns[0].ErrorMessage, provider.historyReads(), provider.sendCallCount())
			if review.ReviewerActivityState != tc.want {
				t.Errorf("reviewer activity=%q, want %q", review.ReviewerActivityState, tc.want)
			}
			if updated, err := st.UpdateReviewChatActivity(ctx, reviewID, priorGeneration, domain.ActivityActive, now); err != nil || updated {
				t.Errorf("stale generation changed current activity: updated=%v err=%v", updated, err)
			}
			workerAfter, found, err := st.GetSession(ctx, testSession)
			if err != nil || !found || !reflect.DeepEqual(workerBefore, workerAfter) || len(workerActivity.snapshot()) != 0 {
				t.Errorf("review auth replay changed worker state: found=%v err=%v", found, err)
			}
			siblingAfter, found, err := st.GetReviewByID(ctx, siblingReviewID)
			if err != nil || !found || !reflect.DeepEqual(siblingBefore, siblingAfter) {
				t.Errorf("review auth replay changed sibling reviewer: found=%v err=%v", found, err)
			}
			if provider.historyReads() != 1 || provider.sendCallCount() != 0 {
				t.Errorf("replay issued provider work: historyReads=%d sendCalls=%d", provider.historyReads(), provider.sendCallCount())
			}
		})
	}
}

func TestReviewerChatLiveReconnectStopSettlesRetainedGenerationOnlyForItsOwner(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	now := time.Now().UTC()
	const (
		reviewID              = "review-reconnect-stop"
		siblingReviewID       = "review-reconnect-sibling"
		priorGeneration       = "review-reconnect-old-generation"
		providerID            = "review-reconnect-native-thread"
		workerConversationID  = "review-reconnect-worker-conversation"
		reviewConversationID  = "review-reconnect-conversation"
		siblingConversationID = "review-reconnect-sibling-conversation"
	)
	if err := st.ClaimChatControllerGeneration(ctx, testSession, "worker-current-generation"); err != nil {
		t.Fatalf("seed worker controller: %v", err)
	}
	if _, err := st.CreateConversation(ctx, workerConversationID, domain.ConversationScopeSession, testProject, testSession, now); err != nil {
		t.Fatalf("seed worker conversation: %v", err)
	}
	if err := st.AdoptProviderTurn(ctx, workerConversationID, testSession, "worker-current-generation", "worker-retained-turn", "worker-retained-native-turn", now); err != nil {
		t.Fatalf("seed running worker: %v", err)
	}
	for _, seed := range []struct {
		id, conversation, generation, provider string
		harness                                domain.ReviewerHarness
	}{
		{reviewID, reviewConversationID, priorGeneration, providerID, domain.ReviewerCodex},
		{siblingReviewID, siblingConversationID, "sibling-current-generation", "sibling-native-thread", domain.ReviewerClaudeCode},
	} {
		if err := st.UpsertReview(ctx, domain.Review{
			ID: seed.id, SessionID: testSession, ProjectID: testProject, Harness: seed.harness,
			InterfaceMode: domain.ReviewerInterfaceChat, ReviewerActivityState: domain.ActivityActive,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed reviewer %s: %v", seed.id, err)
		}
		if _, err := st.CreateReviewConversation(ctx, seed.conversation, seed.id, testProject, testSession, now); err != nil {
			t.Fatalf("seed review conversation %s: %v", seed.id, err)
		}
		if claimed, err := st.ClaimReviewChatController(ctx, seed.id, seed.provider, seed.generation, now); err != nil || !claimed {
			t.Fatalf("seed review generation %s: claimed=%v err=%v", seed.id, claimed, err)
		}
		if err := st.AdoptReviewProviderTurn(ctx, seed.conversation, testSession, seed.id, seed.generation, "turn-"+seed.id, "native-turn-"+seed.id, now); err != nil {
			t.Fatalf("seed retained review turn %s: %v", seed.id, err)
		}
	}
	for _, seed := range []struct{ conversation, nativeTurn string }{
		{workerConversationID, "worker-retained-native-turn"},
		{reviewConversationID, "native-turn-" + reviewID},
		{siblingConversationID, "native-turn-" + siblingReviewID},
	} {
		for _, activity := range []domain.ConversationActivity{
			{ID: "tool-" + seed.conversation, ProviderItemID: "tool-" + seed.conversation, Kind: domain.ActivityKindCommand, Status: domain.ActivityStatusRunning},
			{ID: "approval-" + seed.conversation, ProviderItemID: "approval-" + seed.conversation, Kind: domain.ActivityKindApproval, Status: domain.ActivityStatusPending},
		} {
			if err := st.UpsertActivity(ctx, seed.conversation, seed.nativeTurn, activity, now); err != nil {
				t.Fatalf("seed retained activity for %s: %v", seed.conversation, err)
			}
		}
	}
	workerBefore, err := st.LoadConversationSnapshot(ctx, workerConversationID)
	if err != nil {
		t.Fatal(err)
	}
	siblingBefore, err := st.LoadConversationSnapshot(ctx, siblingConversationID)
	if err != nil {
		t.Fatal(err)
	}
	workerRecordBefore, found, err := st.GetSession(ctx, testSession)
	if err != nil || !found {
		t.Fatalf("worker record before reconnect: found=%v err=%v", found, err)
	}
	provider := &liveReconnectedConversation{nativeHistoryConversation: &nativeHistoryConversation{fakeConversation: newFakeConversation()}}
	provider.providerConversationID = providerID
	var nextID atomic.Int64
	svc := chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st, Reader: fullSnapshotReader(st),
		Drivers:  fakeRegistry{driver: fakeDriver{conv: provider}},
		Activity: &recordingActivity{}, Log: slog.New(slog.DiscardHandler),
		NewID: func() string { return fmt.Sprintf("review-reconnect-%d", nextID.Add(1)) },
	})
	t.Cleanup(func() { svc.StopAll(context.Background()) })
	owner := domain.ReviewConversationOwner(reviewID)
	controller, err := svc.Start(ctx, chatsvc.StartConfig{
		Owner: owner, SessionID: testSession, ProjectID: testProject,
		Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(), ProviderConversationID: providerID,
	})
	if err != nil {
		t.Fatalf("live reconnect reviewer: %v", err)
	}
	if controller.Generation() == priorGeneration || controller.State() != ports.ChatControllerBusy {
		t.Fatalf("live reconnect did not restore the old running turn under a new owner generation: generation=%q state=%q", controller.Generation(), controller.State())
	}
	retained, err := st.LoadConversationSnapshot(ctx, reviewConversationID)
	if err != nil || len(retained.Turns) != 1 || retained.Turns[0].State != domain.TurnStateRunning || retained.Turns[0].ID != "turn-"+reviewID || retained.Turns[0].ProviderTurnID != "native-turn-"+reviewID {
		t.Fatalf("live reconnect did not retain the original turn generation: snapshot=%+v err=%v", retained, err)
	}
	if err := svc.StopForOwner(ctx, owner); err != nil {
		t.Fatalf("StopForOwner reconnected reviewer: %v", err)
	}
	stopped, err := st.LoadConversationSnapshot(ctx, reviewConversationID)
	if err != nil || len(stopped.Turns) != 1 || stopped.Turns[0].State != domain.TurnStateFailed {
		t.Errorf("current reviewer stop left its retained running turn unsettled: snapshot=%+v err=%v", stopped, err)
	}
	for _, activity := range stopped.Activities {
		if activity.Status != domain.ActivityStatusFailed {
			t.Errorf("current reviewer stop left retained activity %s at %q", activity.ID, activity.Status)
		}
	}
	workerAfter, err := st.LoadConversationSnapshot(ctx, workerConversationID)
	if err != nil || !reflect.DeepEqual(workerBefore, workerAfter) {
		t.Errorf("reviewer stop changed worker-owned work: before=%+v after=%+v err=%v", workerBefore, workerAfter, err)
	}
	siblingAfter, err := st.LoadConversationSnapshot(ctx, siblingConversationID)
	if err != nil || !reflect.DeepEqual(siblingBefore, siblingAfter) {
		t.Errorf("reviewer stop changed sibling reviewer work: before=%+v after=%+v err=%v", siblingBefore, siblingAfter, err)
	}
	workerRecordAfter, found, err := st.GetSession(ctx, testSession)
	if err != nil || !found || !reflect.DeepEqual(workerRecordBefore, workerRecordAfter) {
		t.Errorf("reviewer reconnect/stop changed worker state: before=%+v after=%+v found=%v err=%v", workerRecordBefore, workerRecordAfter, found, err)
	}
	if provider.sendCallCount() != 0 || provider.historyReads() != 0 {
		t.Errorf("live reconnect resubmitted or replayed provider work: sends=%d historyReads=%d", provider.sendCallCount(), provider.historyReads())
	}
}

package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestSettleRecoveredControllerWorkRequiresCurrentConversationOwner(t *testing.T) {
	for _, tc := range []struct {
		name       string
		scope      domain.ConversationScope
		generation string
		rebind     bool
		wrongOwner bool
		wantError  bool
	}{
		{name: "project current owner", scope: domain.ConversationScopeProject, generation: "current-generation"},
		{name: "session current owner", scope: domain.ConversationScopeSession, generation: "current-generation"},
		{name: "project stale generation", scope: domain.ConversationScopeProject, generation: "old-generation", wantError: true},
		{name: "project absent generation", scope: domain.ConversationScopeProject, wantError: true},
		{name: "project different session", scope: domain.ConversationScopeProject, generation: "current-generation", wrongOwner: true, wantError: true},
		{name: "project rebound while old session survives", scope: domain.ConversationScopeProject, generation: "current-generation", rebind: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			seedProject(t, s, "recovery-owner")
			record := sampleRecord("recovery-owner")
			record.Mode = domain.SessionModeChat
			record.Metadata.ProviderConversationID = "retained-native-thread"
			if tc.scope == domain.ConversationScopeProject {
				record.Kind = domain.KindOrchestrator
			}
			owner, err := s.CreateSession(ctx, record)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.ClaimChatControllerGeneration(ctx, owner.ID, "current-generation"); err != nil {
				t.Fatal(err)
			}
			conversation, err := s.CreateConversation(ctx, "recovery-conversation", tc.scope, owner.ProjectID, owner.ID, histClock)
			if err != nil {
				t.Fatal(err)
			}
			for _, turnID := range []string{"old-running", "never-dispatched"} {
				created, err := s.AppendUserMessage(ctx, conversation.ID, owner.ID, "current-generation", domain.ConversationMessage{
					ID: turnID + "-message", Text: turnID, Origin: domain.MessageOriginHuman,
				}, turnID, histClock)
				if err != nil || !created {
					t.Fatalf("append %s: created=%v error=%v", turnID, created, err)
				}
			}
			if err := s.BindTurnToProvider(ctx, "old-running", "native-turn", histClock); err != nil {
				t.Fatal(err)
			}
			for _, activity := range []domain.ConversationActivity{
				{ID: "running-command", Kind: domain.ActivityKindCommand, Status: domain.ActivityStatusRunning, Summary: "old command"},
				{ID: "pending-approval", Kind: domain.ActivityKindApproval, Status: domain.ActivityStatusPending, Summary: "old permission", RequestID: "old-approval"},
				{ID: "pending-input", Kind: domain.ActivityKindUserInput, Status: domain.ActivityStatusPending, Summary: "old question", RequestID: "old-input"},
			} {
				if err := s.UpsertActivity(ctx, conversation.ID, "native-turn", activity, histClock); err != nil {
					t.Fatal(err)
				}
			}
			caller := owner.ID
			if tc.rebind || tc.wrongOwner {
				replacement, err := s.CreateSession(ctx, record)
				if err != nil {
					t.Fatal(err)
				}
				// Matching generations alone must not authorize another session.
				if err := s.ClaimChatControllerGeneration(ctx, replacement.ID, "current-generation"); err != nil {
					t.Fatal(err)
				}
				if tc.rebind {
					rebound, err := s.CreateConversation(ctx, "unused-id", domain.ConversationScopeProject, owner.ProjectID, replacement.ID, histClock.Add(time.Minute))
					if err != nil || rebound.ID != conversation.ID || rebound.SessionID != replacement.ID {
						t.Fatalf("rebind retained conversation: %+v, %v", rebound, err)
					}
					retained, found, err := s.GetSession(ctx, owner.ID)
					if err != nil || !found || retained.Metadata.ControllerGeneration != "current-generation" {
						t.Fatalf("old owner must still exist with its generation: %+v found=%v error=%v", retained, found, err)
					}
				} else {
					caller = replacement.ID
				}
			}
			before, err := s.LoadConversationSnapshot(ctx, conversation.ID)
			if err != nil || len(before.Turns) != 2 || len(before.Activities) != 3 || len(before.Messages) != 2 {
				t.Fatalf("recovery fixture must expose all durable work: %+v error=%v", before, err)
			}
			err = s.SettleRecoveredControllerWork(ctx, caller, conversation.ID, tc.generation, histClock.Add(2*time.Minute))
			if tc.wantError {
				if !errors.Is(err, domain.ErrSessionRecoveryChanged) {
					t.Fatalf("stale owner/generation was not rejected: %v", err)
				}
			} else if err != nil {
				t.Fatalf("current owner could not settle retained conversation: %v", err)
			}
			after, err := s.LoadConversationSnapshot(ctx, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantError {
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("rejected recovery mutated running turn, queue or requests: before=%+v after=%+v", before, after)
				}
				return
			}
			if !reflect.DeepEqual(before.Messages, after.Messages) || len(after.Turns) != 2 || len(after.Activities) != 3 {
				t.Fatalf("successful settlement lost history: %+v", after)
			}
			for i, turn := range after.Turns {
				if turn.ID == "never-dispatched" {
					if !reflect.DeepEqual(before.Turns[i], turn) || turn.State != domain.TurnStateQueued {
						t.Fatalf("unsent queued turn changed: %+v", turn)
					}
				} else if turn.ID != "old-running" || turn.State != domain.TurnStateFailed || turn.ErrorMessage == "" || turn.CompletedAt == nil {
					t.Fatalf("old native turn was not settled: %+v", turn)
				}
			}
			for _, activity := range after.Activities {
				if activity.Status != domain.ActivityStatusFailed || activity.TurnID != "old-running" {
					t.Fatalf("old native operation/request remained active: %+v", activity)
				}
			}
		})
	}
}

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestConversationAdmissionRejectionVisibility(t *testing.T) {
	for _, tc := range []struct {
		name          string
		reason        string
		providerBound bool
		rolledBack    bool
		manualCancel  bool
		visible       bool
	}{
		{name: "governance rejection", reason: "governed directive no longer admitted: Work is parked", visible: true},
		{name: "budget rejection", reason: "budget delivery outcome is uncertain", visible: true},
		{name: "empty cancellation cause"},
		{name: "space only cancellation cause", reason: "   "},
		{name: "manual queued cancellation", manualCancel: true},
		{name: "provider bound cancellation", providerBound: true, reason: "provider interrupted the turn"},
		{name: "explicit rollback", rolledBack: true, reason: "governed directive no longer admitted: Work is parked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, session, conversation := conversationFixture(t)
			ctx := context.Background()
			const turnID = "admission-turn"
			const messageID = "admission-message"
			const activityID = "admission-activity"
			created, err := s.AppendUserMessage(ctx, conversation, session, "gen-1", domain.ConversationMessage{
				ID: messageID, Text: "Investigate the failing checks", Origin: domain.MessageOriginHuman,
			}, turnID, histClock)
			if err != nil || !created {
				t.Fatalf("append queued prompt: created=%v err=%v", created, err)
			}

			if tc.manualCancel {
				if err := s.CancelQueuedTurnByID(ctx, conversation, turnID, histClock.Add(time.Second)); err != nil {
					t.Fatalf("cancel queued turn: %v", err)
				}
			} else {
				// The activity writer correlates only by provider ID. Temporarily
				// bind one to seed a linked activity, then clear it for the
				// undispatched read-model cases. No provider is contacted here.
				const providerID = "fixture-provider-turn"
				if err := s.BindTurnToProvider(ctx, turnID, providerID, histClock); err != nil {
					t.Fatalf("bind fixture turn: %v", err)
				}
				if err := s.UpsertActivity(ctx, conversation, providerID, domain.ConversationActivity{
					ID: activityID, Kind: domain.ActivityKindError,
					Status: domain.ActivityStatusFailed, Summary: "Dispatch was rejected",
				}, histClock.Add(time.Second)); err != nil {
					t.Fatalf("append linked activity: %v", err)
				}
				if !tc.providerBound {
					if err := s.BindTurnToProvider(ctx, turnID, "", histClock); err != nil {
						t.Fatalf("clear fixture provider binding: %v", err)
					}
				}
				before, err := s.LoadConversationSnapshot(ctx, conversation)
				if err != nil || len(before.Activities) != 1 || before.Activities[0].TurnID != turnID {
					t.Fatalf("activity must be linked before cancellation: snapshot=%+v err=%v", before, err)
				}
				if err := s.SettleTurnByID(ctx, turnID, domain.TurnStateCancelled, tc.reason, histClock.Add(2*time.Second)); err != nil {
					t.Fatalf("record cancellation cause: %v", err)
				}
			}
			if tc.rolledBack {
				if count, err := s.RollbackTurns(ctx, conversation, turnID, histClock.Add(3*time.Second)); err != nil || count != 1 {
					t.Fatalf("roll back rejected turn: count=%d err=%v", count, err)
				}
			}

			snapshot, err := s.LoadConversationSnapshot(ctx, conversation)
			if err != nil {
				t.Fatalf("load full conversation: %v", err)
			}
			if len(snapshot.Turns) != 1 {
				t.Fatalf("durable turn was lost: %+v", snapshot.Turns)
			}
			turn := snapshot.Turns[0]
			if turn.ID != turnID || turn.State != domain.TurnStateCancelled || turn.ErrorMessage != tc.reason || (turn.ProviderTurnID != "") != tc.providerBound {
				t.Fatalf("durable cancellation changed: %+v", turn)
			}
			if (turn.RolledBackAt != nil) != tc.rolledBack {
				t.Fatalf("rollback marker changed: %+v", turn)
			}
			page, err := s.LoadConversationSnapshotPage(ctx, conversation, 0, 1)
			if err != nil {
				t.Fatalf("load latest conversation page: %v", err)
			}
			if !tc.visible {
				if len(snapshot.Messages) != 0 || len(snapshot.Activities) != 0 || len(page.Messages) != 0 || len(page.Activities) != 0 || page.HasMoreBefore {
					t.Fatalf("discarded content remains visible: full=%+v page=%+v", snapshot, page)
				}
				return
			}

			if len(snapshot.Messages) != 1 || snapshot.Messages[0].ID != messageID || snapshot.Messages[0].TurnID != turnID || snapshot.Messages[0].Text != "Investigate the failing checks" {
				t.Fatalf("rejected prompt was lost: %+v", snapshot.Messages)
			}
			if len(snapshot.Activities) != 1 || snapshot.Activities[0].ID != activityID || snapshot.Activities[0].TurnID != turnID {
				t.Fatalf("linked rejection activity was lost: %+v", snapshot.Activities)
			}
			if len(page.Messages) != 0 || len(page.Activities) != 1 || page.Activities[0].ID != activityID || !page.HasMoreBefore {
				t.Fatalf("latest page must retain rejection activity and older prompt cursor: %+v", page)
			}
			if len(page.Turns) != 1 || page.Turns[0].ID != turnID || page.Turns[0].ErrorMessage != tc.reason {
				t.Fatalf("latest page lost rejection cause: %+v", page.Turns)
			}
			older, err := s.LoadConversationSnapshotPage(ctx, conversation, page.OldestSequence, 1)
			if err != nil {
				t.Fatalf("load older conversation page: %v", err)
			}
			if len(older.Messages) != 1 || older.Messages[0].ID != messageID || len(older.Activities) != 0 || older.HasMoreBefore {
				t.Fatalf("older page must retain original rejected prompt exactly once: %+v", older)
			}
			if len(older.Turns) != 1 || older.Turns[0].ID != turnID || older.Turns[0].ErrorMessage != tc.reason {
				t.Fatalf("older page lost rejection cause: %+v", older.Turns)
			}
		})
	}
}

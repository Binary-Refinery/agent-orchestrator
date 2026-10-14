package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func reviewerProjectionFixture(t *testing.T, s *sqlite.Store, session domain.SessionID, id, generation string, harness domain.ReviewerHarness) string {
	t.Helper()
	ctx := context.Background()
	if err := s.UpsertReview(ctx, domain.Review{ID: id, SessionID: session, ProjectID: "hist", Harness: harness, CreatedAt: histClock, UpdatedAt: histClock}); err != nil {
		t.Fatal(err)
	}
	conv, err := s.CreateReviewConversation(ctx, "conv-"+id, id, "hist", session, histClock)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ClaimReviewChatController(ctx, id, "native-"+id, generation, histClock); err != nil || !ok {
		t.Fatalf("claim reviewer: ok=%v err=%v", ok, err)
	}
	return conv.ID
}

func TestReviewerProviderEventsUseTheirOwnGeneration(t *testing.T) {
	s, session, _ := conversationFixture(t)
	ctx := context.Background()
	conv := reviewerProjectionFixture(t, s, session, "review-1", "review-gen", domain.ReviewerCodex)
	if applied, err := s.ProjectProviderEvent(ctx, conv, "foreign-worker", "review-gen", "wrong-session", "message.delta", `{}`, histClock, func(context.Context) error {
		t.Error("foreign worker reached reviewer projection")
		return nil
	}); err != nil || applied {
		t.Fatalf("foreign reviewer worker: applied=%v err=%v", applied, err)
	}
	for _, generation := range []string{"gen-1", "old-review-gen"} {
		applied, err := s.ProjectProviderEvent(ctx, conv, session, generation, "wrong-"+generation, "message.delta", `{}`, histClock, func(context.Context) error {
			t.Error("foreign generation reached projection")
			return nil
		})
		if err != nil || applied {
			t.Fatalf("foreign generation: applied=%v err=%v", applied, err)
		}
	}
	projectionErr := errors.New("review projection failure")
	if _, err := s.ProjectProviderEvent(ctx, conv, session, "review-gen", "rollback", "message.delta", `{}`, histClock, func(context.Context) error { return projectionErr }); !errors.Is(err, projectionErr) {
		t.Fatalf("review projection rollback: %v", err)
	}
	count := 0
	for range 2 {
		_, err := s.ProjectProviderEvent(ctx, conv, session, "review-gen", "review-event", "message.delta", `{}`, histClock, func(context.Context) error { count++; return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.ProviderEventsSince(ctx, conv, 0, 10)
	if err != nil || len(events) != 1 || count != 1 {
		t.Fatalf("review archive/projection: count=%d events=%v err=%v", count, events, err)
	}
}

func TestReviewerCleanupDoesNotSettleWorkerOrOtherReviewer(t *testing.T) {
	s, session, workerConv := conversationFixture(t)
	ctx := context.Background()
	conv1 := reviewerProjectionFixture(t, s, session, "review-1", "review-gen-1", domain.ReviewerCodex)
	conv2 := reviewerProjectionFixture(t, s, session, "review-2", "review-gen-2", domain.ReviewerClaudeCode)
	owners := []struct{ conv, review, generation string }{{workerConv, "", "gen-1"}, {conv1, "review-1", "review-gen-1"}, {conv2, "review-2", "review-gen-2"}}
	for _, owner := range owners {
		msg := domain.ConversationMessage{ID: "message-" + owner.conv, Text: "bounded task", Origin: domain.MessageOriginHuman}
		var err error
		if owner.review == "" {
			_, err = s.AppendUserMessage(ctx, owner.conv, session, owner.generation, msg, "turn-"+owner.conv, histClock)
		} else {
			_, err = s.AppendReviewUserMessage(ctx, owner.conv, session, owner.review, owner.generation, msg, "turn-"+owner.conv, histClock)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := s.BindTurnToProvider(ctx, "turn-"+owner.conv, "native-turn-"+owner.conv, histClock); err != nil {
			t.Fatal(err)
		}
		for _, activity := range []domain.ConversationActivity{
			{ID: "tool-" + owner.conv, ProviderItemID: "tool-" + owner.conv, Kind: domain.ActivityKindCommand, Status: domain.ActivityStatusRunning},
			{ID: "approval-" + owner.conv, ProviderItemID: "approval-" + owner.conv, Kind: domain.ActivityKindApproval, Status: domain.ActivityStatusPending},
		} {
			if err := s.UpsertActivity(ctx, owner.conv, "native-turn-"+owner.conv, activity, histClock); err != nil {
				t.Fatal(err)
			}
		}
	}
	if owned, err := s.CleanupOwnedControllerWork(ctx, session, conv1, "old-review-gen", histClock.Add(time.Second)); err != nil || owned {
		t.Fatalf("stale reviewer cleanup: owned=%v err=%v", owned, err)
	}
	if owned, err := s.CleanupOwnedControllerWork(ctx, session, conv1, "review-gen-1", histClock.Add(2*time.Second)); err != nil || !owned {
		t.Fatalf("current reviewer cleanup: owned=%v err=%v", owned, err)
	}
	for _, owner := range owners {
		snapshot, err := s.LoadConversationSnapshot(ctx, owner.conv)
		if err != nil || len(snapshot.Turns) != 1 || len(snapshot.Activities) != 2 {
			t.Fatalf("snapshot for %s: %+v err=%v", owner.conv, snapshot, err)
		}
		if owner.review == "review-1" {
			if snapshot.Turns[0].State != domain.TurnStateFailed || snapshot.Activities[0].Status != domain.ActivityStatusFailed || snapshot.Activities[1].Status != domain.ActivityStatusFailed {
				t.Fatalf("reviewer cleanup incomplete: %+v", snapshot)
			}
		} else if snapshot.Turns[0].State != domain.TurnStateRunning || snapshot.Activities[0].Status != domain.ActivityStatusRunning || snapshot.Activities[1].Status != domain.ActivityStatusPending {
			t.Fatalf("cleanup changed sibling owner %s: %+v", owner.conv, snapshot)
		}
	}
	if owned, err := s.CleanupOwnedControllerWork(ctx, session, workerConv, "gen-1", histClock.Add(3*time.Second)); err != nil || !owned {
		t.Fatalf("worker cleanup: owned=%v err=%v", owned, err)
	}
	remaining, err := s.LoadConversationSnapshot(ctx, conv2)
	if err != nil || remaining.Turns[0].State != domain.TurnStateRunning || remaining.Activities[0].Status != domain.ActivityStatusRunning || remaining.Activities[1].Status != domain.ActivityStatusPending {
		t.Fatalf("worker cleanup changed its other reviewer: %+v err=%v", remaining, err)
	}
}

func TestReviewerChatActivityRejectsReplacedController(t *testing.T) {
	s, session, _ := conversationFixture(t)
	ctx := context.Background()
	reviewerProjectionFixture(t, s, session, "review-1", "review-gen", domain.ReviewerCodex)
	if updated, err := s.UpdateReviewChatActivity(ctx, "review-1", "review-gen", domain.ActivityActive, histClock); err != nil || !updated {
		t.Fatalf("current reviewer activity: updated=%v err=%v", updated, err)
	}
	if claimed, err := s.ClaimReviewChatController(ctx, "review-1", "replacement-native", "replacement-gen", histClock.Add(time.Second)); err != nil || !claimed {
		t.Fatalf("replace reviewer: claimed=%v err=%v", claimed, err)
	}
	if updated, err := s.UpdateReviewChatActivity(ctx, "review-1", "review-gen", domain.ActivityIdle, histClock.Add(2*time.Second)); err != nil || updated {
		t.Fatalf("stale reviewer activity: updated=%v err=%v", updated, err)
	}
	review, found, err := s.GetReviewByID(ctx, "review-1")
	if err != nil || !found || review.ReviewerActivityState != domain.ActivityActive || review.ProviderConversationID != "replacement-native" {
		t.Fatalf("stale activity changed replacement reviewer: %+v found=%v err=%v", review, found, err)
	}
}

package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func admitDiagnosticMember(t *testing.T, s *sqlite.Store, workID string, sid domain.SessionID, launches []domain.InstructorLaunch) domain.InstructorDiagnosticScope {
	t.Helper()
	ctx := context.Background()
	rec, found, err := s.GetSession(ctx, sid)
	if err != nil || !found {
		t.Fatalf("session: %v %v", found, err)
	}
	instance, found, err := s.InstructorSessionInstance(ctx, sid)
	if err != nil || !found {
		t.Fatalf("instance: %v %v", found, err)
	}
	work := domain.InstructorWork{ID: workID, Name: workID, ProjectID: rec.ProjectID, Source: "manual", SourceRef: workID, Revision: 1, Launches: launches}
	if _, err := s.CreateInstructorWork(ctx, work, domain.InstructorEvent{ID: workID + "-created", WorkID: workID}); err != nil {
		t.Fatal(err)
	}
	work.Revision++
	work.SessionIDs = []domain.SessionID{sid}
	if saved, err := s.AttachInstructorSession(ctx, work, 1, domain.InstructorEvent{ID: workID + "-admitted", WorkID: workID}, domain.InstructorSessionMembership{SessionID: sid, InstanceID: instance, ProjectID: rec.ProjectID, Provenance: "admitted"}); err != nil || !saved {
		t.Fatalf("admit: %v %v", saved, err)
	}
	generation := rec.Metadata.ControllerGeneration
	if domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeChat {
		generation = rec.Metadata.RuntimeLaunchID
	}
	return domain.InstructorDiagnosticScope{WorkID: workID, ProjectID: rec.ProjectID, SessionID: sid, InstanceID: instance, MemberID: "session:" + instance, RuntimeGeneration: generation}
}

func TestInstructorDiagnosticsRetainsStoppedHistoryAndBoundsPage(t *testing.T) {
	ctx := context.Background()
	s, sid, conversation := conversationFixture(t)
	scope := admitDiagnosticMember(t, s, "diagnostic-work", sid, nil)
	for i := 1; i <= 3; i++ {
		if added, err := s.AppendUserMessage(ctx, conversation, sid, "gen-1", domain.ConversationMessage{ID: fmt.Sprintf("diagnostic-message-%d", i), Text: fmt.Sprintf("message %d", i), Origin: domain.MessageOriginHuman}, fmt.Sprintf("diagnostic-turn-%d", i), histClock.Add(time.Duration(i)*time.Second)); err != nil || !added {
			t.Fatalf("message: %v %v", added, err)
		}
	}
	rec, _, _ := s.GetSession(ctx, sid)
	rec.IsTerminated, rec.Activity.State = true, domain.ActivityExited
	rec.Metadata.WorkspacePath = ""
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	stopped, found, err := s.GetSession(ctx, sid)
	if err != nil || !found || !stopped.IsTerminated || stopped.Activity.State != domain.ActivityExited {
		t.Fatalf("stopped fixture not persisted: %+v found=%v err=%v", stopped, found, err)
	}
	page, err := s.CaptureInstructorDiagnostics(ctx, scope, true, 0, 2)
	if err != nil || !page.Session.IsTerminated || len(page.Messages) != 2 || page.OldestSequence != 2 || !page.HasMoreBefore || page.Messages[0].Text != "message 2" || page.Conversation.ID != conversation {
		t.Fatalf("stopped page=%+v err=%v", page, err)
	}
	older, err := s.CaptureInstructorDiagnostics(ctx, scope, true, page.OldestSequence, 2)
	if err != nil || len(older.Messages) != 1 || older.Messages[0].Text != "message 1" || older.HasMoreBefore {
		t.Fatalf("older=%+v err=%v", older, err)
	}
	metadata, err := s.CaptureInstructorDiagnostics(ctx, scope, false, 0, 50)
	if err != nil || len(metadata.Messages) != 0 || len(metadata.Turns) != 0 || metadata.Conversation.ID != conversation {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	for _, alter := range []func(*domain.InstructorDiagnosticScope){
		func(s *domain.InstructorDiagnosticScope) { s.ProjectID = "foreign" },
		func(s *domain.InstructorDiagnosticScope) { s.WorkID = "other-work" },
		func(s *domain.InstructorDiagnosticScope) { s.RuntimeGeneration = "" },
		func(s *domain.InstructorDiagnosticScope) { s.InstanceID = "unknown" },
		func(s *domain.InstructorDiagnosticScope) { s.MemberID = "legacy:" + string(s.SessionID) },
	} {
		stale := scope
		alter(&stale)
		if _, err := s.CaptureInstructorDiagnostics(ctx, stale, true, 0, 50); !errors.Is(err, domain.ErrInstructorProducerChanged) {
			t.Fatalf("stale scope accepted: %+v err=%v", stale, err)
		}
	}
	if err := s.ClaimChatControllerGeneration(ctx, sid, "gen-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureInstructorDiagnostics(ctx, scope, true, 0, 50); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("old generation accepted: %v", err)
	}
}

func TestInstructorDiagnosticsRejectsReplacementBeforeReadingItsNarrative(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dataDir)
	seedProject(t, s, "hist")
	rec := sampleRecord("hist")
	rec.Mode = domain.SessionModeChat
	rec.Metadata.ControllerGeneration = "gen-1"
	original, err := s.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	sid := original.ID
	if _, err := s.CreateConversation(ctx, "original-conversation", domain.ConversationScopeSession, original.ProjectID, sid, histClock); err != nil {
		t.Fatal(err)
	}
	scope := admitDiagnosticMember(t, s, "original-work", sid, nil)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "ao.db")+failureStorePragmas)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// A started row is deliberately ineligible for DeleteSession. Mirror the
	// fixture deletion ordering with foreign keys enabled to replace it.
	if _, err := db.ExecContext(ctx, `DELETE FROM change_log WHERE session_id=?`, sid); err != nil {
		t.Fatal(err)
	}
	deleted, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := deleted.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("delete original row: rows=%d err=%v", n, err)
	}
	replacement, err := s.CreateSession(ctx, original)
	if err != nil || replacement.ID != original.ID || !replacement.CreatedAt.Equal(original.CreatedAt) {
		t.Fatalf("replacement must reuse display ID and timestamp: %+v %v", replacement, err)
	}
	newScope := admitDiagnosticMember(t, s, "replacement-work", sid, nil)
	if newScope.InstanceID == scope.InstanceID {
		t.Fatalf("replacement reused instance %q", newScope.InstanceID)
	}
	conversation, err := s.CreateConversation(ctx, "replacement-conversation", domain.ConversationScopeSession, replacement.ProjectID, sid, histClock)
	if err != nil {
		t.Fatal(err)
	}
	if added, err := s.AppendUserMessage(ctx, conversation.ID, sid, "gen-1", domain.ConversationMessage{ID: "replacement-secret", Text: "different work private narrative", Origin: domain.MessageOriginHuman}, "replacement-turn", histClock); err != nil || !added {
		t.Fatalf("append: %v %v", added, err)
	}
	for _, stale := range []domain.InstructorDiagnosticScope{scope, {WorkID: scope.WorkID, ProjectID: scope.ProjectID, MemberID: newScope.MemberID, SessionID: sid, InstanceID: newScope.InstanceID, RuntimeGeneration: "gen-1"}} {
		page, err := s.CaptureInstructorDiagnostics(ctx, stale, true, 0, 50)
		if !errors.Is(err, domain.ErrInstructorProducerChanged) || len(page.Messages) != 0 {
			t.Fatalf("replacement narrative escaped old authority: %+v err=%v", page, err)
		}
	}
	page, err := s.CaptureInstructorDiagnostics(ctx, newScope, true, 0, 50)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Text != "different work private narrative" {
		t.Fatalf("replacement own authority: %+v err=%v", page, err)
	}
}

func TestInstructorDiagnosticsReviewNamespaceAndRetainedIdentity(t *testing.T) {
	ctx := context.Background()
	s, sid, workerConversation := conversationFixture(t)
	instance, _, _ := s.InstructorSessionInstance(ctx, sid)
	created := histClock.Add(time.Minute)
	review := domain.Review{ID: string(sid), SessionID: sid, ProjectID: "hist", Harness: domain.ReviewerCodex, InterfaceMode: domain.ReviewerInterfaceChat, CreatedAt: created, UpdatedAt: created}
	if err := s.UpsertReview(ctx, review); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{ID: "diagnostic-review-run", ReviewID: review.ID, SessionID: sid, Harness: review.Harness, Status: domain.ReviewRunFailed, CreatedAt: created}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	reviewerConversation, err := s.CreateReviewConversation(ctx, "review-diagnostic-conversation", review.ID, "hist", sid, created)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.ClaimReviewChatController(ctx, review.ID, "review-provider", "review-generation", created); err != nil || !claimed {
		t.Fatalf("claim review: %v %v", claimed, err)
	}
	launch := domain.InstructorLaunch{ReviewID: review.ID, ReviewRunID: run.ID, ReviewCreatedAt: &created, OwnerSessionID: sid, SessionInstanceID: instance, RuntimeGeneration: "review-generation"}
	sessionScope := admitDiagnosticMember(t, s, "review-work", sid, []domain.InstructorLaunch{launch})
	reviewScope := sessionScope
	reviewScope.MemberID, reviewScope.RuntimeGeneration = "review:"+review.ID+":"+run.ID, "review-generation"
	worker, err := s.CaptureInstructorDiagnostics(ctx, sessionScope, true, 0, 50)
	if err != nil || worker.Kind != "session" || worker.Conversation.ID != workerConversation || worker.Review != nil {
		t.Fatalf("worker namespace mixed: %+v %v", worker, err)
	}
	page, err := s.CaptureInstructorDiagnostics(ctx, reviewScope, true, 0, 50)
	if err != nil || page.Kind != "review" || page.TimelineScope != "review" || page.Conversation.ID != reviewerConversation.ID || page.ReviewRun == nil || page.ReviewRun.ID != run.ID {
		t.Fatalf("review namespace mixed: %+v %v", page, err)
	}
	foreignWork := domain.InstructorWork{ID: "unadmitted-review-claim", Name: "unadmitted-review-claim", ProjectID: "hist", Source: "manual", SourceRef: "unadmitted-review-claim", Revision: 1, Launches: []domain.InstructorLaunch{launch}}
	if _, err := s.CreateInstructorWork(ctx, foreignWork, domain.InstructorEvent{ID: "unadmitted-review-created", WorkID: foreignWork.ID}); err != nil {
		t.Fatal(err)
	}
	foreign := reviewScope
	foreign.WorkID = foreignWork.ID
	if _, err := s.CaptureInstructorDiagnostics(ctx, foreign, true, 0, 50); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("copied review claim crossed work membership: %v", err)
	}
	stale := reviewScope
	stale.MemberID = "review:" + review.ID + ":another-run"
	if _, err := s.CaptureInstructorDiagnostics(ctx, stale, true, 0, 50); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("unadmitted run accepted: %v", err)
	}
	if claimed, err := s.ClaimReviewChatController(ctx, review.ID, "new-provider", "new-review-generation", created.Add(time.Second)); err != nil || !claimed {
		t.Fatalf("replace review generation: %v %v", claimed, err)
	}
	if _, err := s.CaptureInstructorDiagnostics(ctx, reviewScope, true, 0, 50); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("stale reviewer generation accepted: %v", err)
	}
	reviewScope.RuntimeGeneration = "new-review-generation"
	if _, err := s.CaptureInstructorDiagnostics(ctx, reviewScope, true, 0, 50); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("new controller inherited old launch authority: %v", err)
	}
}

func TestInstructorDiagnosticsTUIAndUnknownGenerationRemainExplicit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "no-controller")
	rec := sampleRecord("no-controller")
	rec.Mode = domain.SessionModeChat
	rec.ProvisionState, rec.ProvisionError = domain.SessionProvisionFailed, "provider startup failed"
	created, err := s.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	scope := admitDiagnosticMember(t, s, "no-controller-work", created.ID, nil)
	page, err := s.CaptureInstructorDiagnostics(ctx, scope, true, 0, 50)
	if err != nil || page.Session.ProvisionError != rec.ProvisionError || page.TimelineReason != "no_persisted_conversation" {
		t.Fatalf("missing-controller diagnosis=%+v %v", page, err)
	}
	conversation, err := s.CreateConversation(ctx, "retained-chat-conversation", domain.ConversationScopeSession, created.ProjectID, created.ID, histClock)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimChatControllerGeneration(ctx, created.ID, "chat-generation"); err != nil {
		t.Fatal(err)
	}
	if added, err := s.AppendUserMessage(ctx, conversation.ID, created.ID, "chat-generation", domain.ConversationMessage{ID: "retained-chat-message", Text: "previous Chat narrative", Origin: domain.MessageOriginHuman}, "retained-chat-turn", histClock); err != nil || !added {
		t.Fatalf("retain Chat narrative: %v %v", added, err)
	}
	if changed, err := s.CommitSessionControllerEpoch(ctx, created.ID, domain.SessionModeChat, domain.SessionModeTUI, "retained-native-conversation", histClock.Add(time.Second)); err != nil || !changed {
		t.Fatalf("switch to TUI: %v %v", changed, err)
	}
	tui, found, err := s.GetSession(ctx, created.ID)
	if err != nil || !found || tui.Mode != domain.SessionModeTUI {
		t.Fatalf("TUI fixture not persisted: %+v found=%v err=%v", tui, found, err)
	}
	scope.RuntimeGeneration = tui.Metadata.RuntimeLaunchID
	retained, err := s.LoadConversationSnapshotPage(ctx, conversation.ID, 0, 50)
	if err != nil || len(retained.Messages) != 1 {
		t.Fatalf("Chat narrative was not retained: %+v %v", retained, err)
	}
	page, err = s.CaptureInstructorDiagnostics(ctx, scope, true, 0, 50)
	if err != nil || page.Session.Mode != domain.SessionModeTUI || page.TimelineReason != "native_tui_logs_unavailable" || page.Conversation.ID != "" || len(page.Messages) != 0 {
		t.Fatalf("TUI diagnosis=%+v %v", page, err)
	}
}

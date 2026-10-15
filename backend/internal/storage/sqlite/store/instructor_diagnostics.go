package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// CaptureInstructorDiagnostics reads membership, owner incarnation, generation
// and persisted history in one read transaction. There is no subsequent SID-only
// read, provider call or filesystem operation that could cross a replacement.
// Stopped/terminated retained rows remain valid diagnostic authorities.
func (s *Store) CaptureInstructorDiagnostics(ctx context.Context, scope domain.InstructorDiagnosticScope, timeline bool, before, limit int64) (domain.InstructorDiagnosticCapture, error) {
	var captured domain.InstructorDiagnosticCapture
	if scope.WorkID == "" || scope.ProjectID == "" || scope.MemberID == "" || scope.SessionID == "" || scope.InstanceID == "" || before < 0 || limit < 1 || limit > 100 {
		return captured, domain.ErrInstructorProducerChanged
	}
	tx, err := s.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return captured, err
	}
	defer func() { _ = tx.Rollback() }()
	q := gen.New(tx)
	var body, instance string
	err = tx.QueryRowContext(ctx, `SELECT w.body,i.instance_id FROM instructor_works w JOIN sessions s ON s.id=? JOIN instructor_session_instances i ON i.session_id=s.id WHERE w.id=? AND w.project_id=? AND s.project_id=w.project_id`, scope.SessionID, scope.WorkID, scope.ProjectID).Scan(&body, &instance)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && instance != scope.InstanceID) {
		return captured, domain.ErrInstructorProducerChanged
	}
	if err != nil {
		return captured, err
	}
	var work domain.InstructorWork
	if err := json.Unmarshal([]byte(body), &work); err != nil {
		return captured, err
	}
	row, err := q.GetSession(ctx, scope.SessionID)
	if err != nil {
		return captured, err
	}
	captured.Session = rowToRecord(row)
	captured.WorkRevision = work.Revision
	if captured.Session.IsTaskPreparation || work.ProjectID != scope.ProjectID || captured.Session.ProjectID != scope.ProjectID {
		return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
	}
	if scope.MemberID != "coordinator:"+scope.InstanceID {
		// Review launches also need the admitted worker authority. A copied or
		// malformed launch claim cannot make another work own its conversation.
		var admitted int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM instructor_session_members m JOIN instructor_sessions b ON b.work_id=m.work_id AND b.session_id=m.session_id AND b.instance_id=m.instance_id WHERE m.work_id=? AND m.session_id=? AND m.instance_id=? AND json_extract(m.body,'$.provenance')='admitted'`, scope.WorkID, scope.SessionID, scope.InstanceID).Scan(&admitted)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
		}
		if err != nil {
			return captured, err
		}
	}
	var conversation gen.Conversation
	switch {
	case scope.MemberID == "session:"+scope.InstanceID:
		captured.Kind, captured.TimelineScope = "session", "session"
		if diagnosticSessionGeneration(captured.Session) != scope.RuntimeGeneration {
			return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
		}
		conversation, err = q.SelectConversationBySession(ctx, &scope.SessionID)
	case scope.MemberID == "coordinator:"+scope.InstanceID:
		if captured.Session.Kind != domain.KindOrchestrator || diagnosticSessionGeneration(captured.Session) != scope.RuntimeGeneration {
			return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
		}
		captured.Kind, captured.TimelineScope = "session", "project"
		conversation, err = q.SelectConversationBySession(ctx, &scope.SessionID)
	case strings.HasPrefix(scope.MemberID, "review:"):
		var launch *domain.InstructorLaunch
		for i := len(work.Launches) - 1; i >= 0; i-- {
			candidate := &work.Launches[i]
			if scope.MemberID == "review:"+candidate.ReviewID+":"+candidate.ReviewRunID && candidate.OwnerSessionID == scope.SessionID && candidate.SessionInstanceID == scope.InstanceID && candidate.ReviewCreatedAt != nil {
				launch = candidate
				break
			}
		}
		if launch == nil {
			return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
		}
		reviewRow, e := q.GetReviewByID(ctx, launch.ReviewID)
		if errors.Is(e, sql.ErrNoRows) {
			return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
		}
		if e != nil {
			return captured, e
		}
		review := reviewFromReview(reviewRow)
		runRow, e := q.GetReviewRun(ctx, launch.ReviewRunID)
		if errors.Is(e, sql.ErrNoRows) {
			return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
		}
		if e != nil {
			return captured, e
		}
		run := reviewRunFromRow(runRow)
		generation := review.ReviewerLaunchID
		if generation == "" {
			generation = review.ControllerGeneration
		}
		if review.ProjectID != scope.ProjectID || review.SessionID != scope.SessionID || !review.CreatedAt.Equal(*launch.ReviewCreatedAt) || run.SessionID != scope.SessionID || run.ReviewID != review.ID || generation != launch.RuntimeGeneration || generation != scope.RuntimeGeneration {
			return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
		}
		captured.Kind, captured.TimelineScope = "review", "review"
		captured.Review, captured.ReviewRun = &review, &run
		conversation, err = q.SelectConversationByReview(ctx, nullableString(review.ID))
	default:
		return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
	}
	if (captured.Kind == "session" && domain.NormalizeSessionMode(captured.Session.Mode) != domain.SessionModeChat) || (captured.Kind == "review" && captured.Review.InterfaceMode != domain.ReviewerInterfaceChat) {
		// A retained Chat conversation is not the current TUI provider's log.
		captured.TimelineReason = "native_tui_logs_unavailable"
		err = sql.ErrNoRows
	}
	if errors.Is(err, sql.ErrNoRows) {
		if captured.TimelineReason == "" {
			captured.TimelineReason = "no_persisted_conversation"
		}
	} else if err != nil {
		return domain.InstructorDiagnosticCapture{}, err
	} else {
		if conversation.ProjectID == nil || *conversation.ProjectID != scope.ProjectID || (captured.Kind == "review" && (!conversation.CurrentReviewID.Valid || conversation.CurrentReviewID.String != captured.Review.ID)) || (captured.Kind == "session" && (conversation.CurrentSessionID == nil || *conversation.CurrentSessionID != scope.SessionID || conversation.CurrentReviewID.Valid)) {
			return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
		}
		if conversation.Scope == domain.ConversationScopeProject {
			if captured.Session.Kind != domain.KindOrchestrator || captured.Kind != "session" {
				return domain.InstructorDiagnosticCapture{}, domain.ErrInstructorProducerChanged
			}
			captured.TimelineScope = "project"
		}
		captured.Conversation = conversationToDomain(conversation)
		if timeline {
			// A transaction-bound copy keeps every helper used by the existing
			// branch-aware page reader on this exact ownership snapshot.
			reader := *s
			reader.qr = q
			page, err := reader.LoadConversationSnapshotPage(ctx, conversation.ID, before, limit)
			if err != nil {
				return domain.InstructorDiagnosticCapture{}, err
			}
			captured.Conversation, captured.Turns, captured.Messages, captured.Activities = page.Conversation, page.Turns, page.Messages, page.Activities
			captured.OldestSequence, captured.HasMoreBefore = page.OldestSequence, page.HasMoreBefore
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.InstructorDiagnosticCapture{}, err
	}
	return captured, nil
}

func diagnosticSessionGeneration(rec domain.SessionRecord) string {
	if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
		return rec.Metadata.ControllerGeneration
	}
	return rec.Metadata.RuntimeLaunchID
}

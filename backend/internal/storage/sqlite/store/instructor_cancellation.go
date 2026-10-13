package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// CompleteInstructorCancellation fences every observed member and reviewer in
// the same transaction that records terminal cancellation and its audit.
func (s *Store) CompleteInstructorCancellation(ctx context.Context, w domain.InstructorWork, expected int64, event domain.InstructorEvent, proof domain.InstructorCancellationProof) (bool, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return false, err
	}
	defer s.writeMu.Unlock()
	saved := false
	err := s.inTxDB(ctx, "complete instructor cancellation", func(q *gen.Queries, db gen.DBTX) error {
		var body string
		if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_works WHERE id=? AND revision=?`, w.ID, expected).Scan(&body); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		var previous domain.InstructorWork
		if err := json.Unmarshal([]byte(body), &previous); err != nil {
			return err
		}
		if previous.Cancellation == nil || w.Cancellation == nil || previous.Cancellation.DispatchID != w.Cancellation.DispatchID || !previous.Cancellation.RequestedAt.Equal(w.Cancellation.RequestedAt) || w.Cancellation.CompletedAt == nil || previous.State != "parked" || w.State != "cancelled" || w.Revision != expected+1 {
			return errors.New("invalid cancellation completion intent")
		}
		for _, grant := range previous.PublicationGrants {
			if grant.Execution != nil && (grant.Execution.State == "claimed" || grant.Execution.State == "uncertain") {
				return domain.ErrInstructorProducerChanged
			}
		}
		for _, action := range previous.PhaseActions {
			if action.State == "running" {
				return domain.ErrInstructorProducerChanged
			}
		}
		var dispatchState string
		if err := db.QueryRowContext(ctx, `SELECT state FROM instructor_dispatches WHERE id=? AND work_id=?`, previous.Cancellation.DispatchID, w.ID).Scan(&dispatchState); err != nil || dispatchState != "delivered" && dispatchState != "failed" && dispatchState != "cancelled" {
			return domain.ErrInstructorProducerChanged
		}
		for _, a := range proof.Producers {
			if err := validateInstructorReportActor(ctx, db, a); err != nil {
				return err
			}
			row, err := q.GetSession(ctx, a.SessionID)
			if err != nil {
				return domain.ErrInstructorProducerChanged
			}
			rec := rowToRecord(row)
			runtime := rec.Metadata.RuntimeLaunchID
			if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
				runtime = "chat:" + rec.Metadata.ControllerGeneration
			}
			if rec.Kind != domain.KindWorker || rec.ProjectID != w.ProjectID || runtime != a.RuntimeID {
				return domain.ErrInstructorProducerChanged
			}
		}
		memberRows, err := db.QueryContext(ctx, `SELECT body FROM instructor_session_members WHERE work_id=?`, w.ID)
		if err != nil {
			return err
		}
		defer func() { _ = memberRows.Close() }()
		var members []domain.InstructorSessionMembership
		for memberRows.Next() {
			var data string
			if err := memberRows.Scan(&data); err != nil {
				_ = memberRows.Close()
				return err
			}
			var member domain.InstructorSessionMembership
			if err := json.Unmarshal([]byte(data), &member); err != nil {
				_ = memberRows.Close()
				return err
			}
			members = append(members, member)
		}
		err = memberRows.Err()
		_ = memberRows.Close()
		if err != nil {
			return err
		}
		for _, sid := range previous.SessionIDs {
			found := false
			for _, member := range members {
				if member.SessionID == sid {
					found = true
				}
			}
			if !found {
				return domain.ErrInstructorProducerChanged
			}
		}
		for _, member := range members {
			if member.Role == string(domain.KindOrchestrator) {
				continue
			}
			found := false
			for _, a := range proof.Producers {
				if a.SessionID == member.SessionID && a.InstanceID == member.InstanceID {
					found = true
				}
			}
			if !found {
				return domain.ErrInstructorProducerChanged
			}
		}
		seenReviews := map[string]bool{}
		for _, r := range proof.Reviews {
			review, err := q.GetReviewByID(ctx, r.ID)
			if err != nil {
				return domain.ErrInstructorProducerChanged
			}
			runtime := review.ReviewerLaunchID
			if runtime == "" {
				runtime = review.ControllerGeneration
			}
			if review.ProjectID != w.ProjectID || !review.CreatedAt.Equal(r.CreatedAt) || runtime != r.RuntimeID || review.ControllerGeneration != r.ControllerGeneration || review.ReviewerHandleID != r.HandleID || domain.ReviewerInterfaceMode(review.InterfaceMode) != r.InterfaceMode ||
				review.ReviewerActivityState != string(domain.ActivityExited) && (!r.NativeQuiet || r.InterfaceMode != domain.ReviewerInterfaceChat || r.ControllerGeneration == "") {
				return domain.ErrInstructorProducerChanged
			}
			seenReviews[r.ID] = true
		}
		for _, launch := range previous.Launches {
			if launch.ReviewID != "" && !seenReviews[launch.ReviewID] {
				return domain.ErrInstructorProducerChanged
			}
		}
		// Native project-orchestrator directives can continue independently of
		// a worker interrupt. Confirm their exact durable turn, never stop the
		// shared orchestrator merely because this work was cancelled.
		dispatchRows, err := db.QueryContext(ctx, `SELECT body FROM instructor_dispatches WHERE work_id=?`, w.ID)
		if err != nil {
			return err
		}
		defer func() { _ = dispatchRows.Close() }()
		var dispatches []domain.InstructorDispatch
		for dispatchRows.Next() {
			var data string
			if err := dispatchRows.Scan(&data); err != nil {
				_ = dispatchRows.Close()
				return err
			}
			var d domain.InstructorDispatch
			if err := json.Unmarshal([]byte(data), &d); err != nil {
				_ = dispatchRows.Close()
				return err
			}
			dispatches = append(dispatches, d)
		}
		err = dispatchRows.Err()
		_ = dispatchRows.Close()
		if err != nil {
			return err
		}
		for _, d := range dispatches {
			if d.Action == "stopp" || d.Action == "abbrechen" || d.TargetSessionID == "" {
				continue
			}
			if err := cancellationTargetSettled(ctx, db, d); err != nil {
				return err
			}
		}
		// Unknown reservations are not proof of no native launch. Every active
		// worker/reviewer reservation must belong to a positively quiet epoch.
		entries, err := readInstructorBudgetEntries(ctx, db, `SELECT body FROM instructor_budget_entries WHERE work_id=? AND kind='session' AND state IN ('reserved','started','uncertain')`, w.ID)
		if err != nil {
			return err
		}
		var finished []domain.InstructorBudgetEntry
		for _, entry := range entries {
			quiet := false
			for _, a := range proof.Producers {
				if entry.ReviewID == "" && entry.SessionID == a.SessionID && entry.ResourceKey == "session:"+a.InstanceID {
					quiet = true
				}
			}
			for _, r := range proof.Reviews {
				if entry.ReviewID != r.ID {
					continue
				}
				suffix := "/review:" + r.ID + ":" + r.CreatedAt.UTC().Format("20060102T150405.999999999Z")
				for _, a := range proof.Producers {
					if a.SessionID == entry.SessionID && entry.ResourceKey == "session:"+a.InstanceID+suffix {
						quiet = true
					}
				}
			}
			if !quiet && entry.ReviewID == "" {
				rec, err := q.GetSession(ctx, entry.SessionID)
				if err != nil {
					return domain.ErrInstructorProducerChanged
				}
				var instance string
				if err := db.QueryRowContext(ctx, `SELECT instance_id FROM instructor_session_instances WHERE session_id=?`, entry.SessionID).Scan(&instance); err != nil {
					return domain.ErrInstructorProducerChanged
				}
				if rec.Kind == domain.KindOrchestrator && rec.ProjectID != nil && *rec.ProjectID == w.ProjectID && entry.ResourceKey == "session:"+instance {
					// The shared coordinator's allocation belongs to this
					// work. Its directives were all proved terminal above;
					// allocations with no directive have no work-owned turn.
					quiet = true
				}
			}
			if !quiet {
				return domain.ErrInstructorProducerChanged
			}
			finished = append(finished, entry)
		}
		if err := finishInstructorBudgetEntries(ctx, db, finished, w.UpdatedAt); err != nil {
			return err
		}
		for _, launch := range previous.Launches {
			if launch.ReviewID == "" || launch.ReviewRunID == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, `UPDATE review_run SET status='cancelled',body='cancelled after positively observed Instructor work quiescence' WHERE id=? AND review_id=? AND status='running'`, launch.ReviewRunID, launch.ReviewID); err != nil {
				return err
			}
		}
		b, err := instructorJSON(w)
		if err != nil {
			return err
		}
		result, err := db.ExecContext(ctx, `UPDATE instructor_works SET revision=?,body=? WHERE id=? AND revision=?`, w.Revision, b, w.ID, expected)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return err
		}
		e, err := instructorJSON(event)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, w.ID, e)
		saved = err == nil
		return err
	})
	return saved, err
}

// cancellationTargetSettled requires provider-owned durable terminal state for
// the exact original directive. A failed lookup/probe cannot establish quiet.
func cancellationTargetSettled(ctx context.Context, db gen.DBTX, d domain.InstructorDispatch) error {
	// A bound outbox row is not native admission. Only a never-leased,
	// never-queued cancelled row may establish non-admission, under this
	// writer transaction and with a durable semantic fence against later send.
	if d.State == "cancelled" && d.Attempts == 0 && d.QueuedAt == nil && d.NativeReceipt == nil {
		var instance, runtime string
		err := db.QueryRowContext(ctx, `SELECT i.instance_id,s.runtime_launch_id FROM sessions s JOIN instructor_session_instances i ON i.session_id=s.id WHERE s.id=?`, d.TargetSessionID).Scan(&instance, &runtime)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrInstructorProducerChanged
			}
			return err
		}
		if strings.HasPrefix(d.TargetRuntimeID, "chat:") {
			var generation string
			if err := db.QueryRowContext(ctx, `SELECT controller_generation FROM sessions WHERE id=?`, d.TargetSessionID).Scan(&generation); err != nil {
				return err
			}
			runtime = "chat:" + generation
		}
		if instance != d.TargetSessionInstanceID || runtime != d.TargetRuntimeID || runtime == "" {
			return domain.ErrInstructorProducerChanged
		}
		var messages int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM conversation_messages WHERE client_message_id=? AND role='user'`, d.ID).Scan(&messages); err != nil {
			return err
		}
		if messages == 0 {
			var state, generation string
			err := db.QueryRowContext(ctx, `SELECT state,runtime_launch_id FROM semantic_deliveries WHERE session_id=? AND client_message_id=?`, d.TargetSessionID, d.ID).Scan(&state, &generation)
			if errors.Is(err, sql.ErrNoRows) {
				_, err = db.ExecContext(ctx, `INSERT INTO semantic_deliveries(session_id,client_message_id,runtime_launch_id,state,created_at) VALUES(?,?,?,'cancelled',?)`, d.TargetSessionID, d.ID, runtime, d.UpdatedAt.UTC().Format(time.RFC3339Nano))
				return err
			}
			if err != nil {
				return err
			}
			if state == "cancelled" && generation == runtime {
				return nil
			}
		}
	}
	if strings.HasPrefix(d.TargetRuntimeID, "chat:") {
		var count, unsettled int
		err := db.QueryRowContext(ctx, `SELECT count(*),COALESCE(SUM(t.state NOT IN ('completed','interrupted','failed','cancelled')),0) FROM conversation_messages m JOIN conversation_turns t ON t.id=m.turn_id WHERE m.client_message_id=? AND m.role='user' AND t.handled_by_session_id=? AND 'chat:'||t.controller_generation=?`, d.ID, d.TargetSessionID, d.TargetRuntimeID).Scan(&count, &unsettled)
		if err != nil {
			return err
		}
		if count == 0 || unsettled != 0 {
			return domain.ErrInstructorProducerChanged
		}
		return nil
	}
	var instance, runtime string
	var quiet bool
	err := db.QueryRowContext(ctx, `SELECT i.instance_id,s.runtime_launch_id,(s.is_terminated=1 OR s.activity_state='exited') FROM sessions s JOIN instructor_session_instances i ON i.session_id=s.id WHERE s.id=?`, d.TargetSessionID).Scan(&instance, &runtime, &quiet)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil || instance != d.TargetSessionInstanceID || runtime != d.TargetRuntimeID || !quiet {
		return domain.ErrInstructorProducerChanged
	}
	return nil
}

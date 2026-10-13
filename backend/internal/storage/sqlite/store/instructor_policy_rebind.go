package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// SaveInstructorPolicyRebind fences membership, runtime epochs, reservations and
// outbox admission in the writer transaction containing the replacement and its
// audit receipt. Native probes alone cannot fence a concurrent launch.
func (s *Store) SaveInstructorPolicyRebind(ctx context.Context, w domain.InstructorWork, expected int64, event domain.InstructorEvent, proof domain.InstructorCancellationProof) (bool, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return false, err
	}
	defer s.writeMu.Unlock()
	saved := false
	err := s.inTxDB(ctx, "rebind instructor policy", func(q *gen.Queries, db gen.DBTX) error {
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
		if previous.Cancellation != nil || previous.State == "done" || previous.State == "cancelled" || previous.State == "running" || len(previous.PublicationGrants) > 0 || len(previous.UncertainDispatchIDs) > 0 || w.Revision != expected+1 || len(w.PolicyRebindings) != len(previous.PolicyRebindings)+1 || w.PolicyVersion == previous.PolicyVersion || (w.State != "parked" && w.State != "waiting_human") {
			return errors.New("invalid policy replacement intent")
		}
		for _, action := range previous.PhaseActions {
			if action.State == "running" || action.State == "uncertain" {
				return domain.ErrInstructorProducerChanged
			}
		}
		if err := validatePolicyRebindProducers(ctx, q, db, previous, proof); err != nil {
			return err
		}
		if err := validatePolicyRebindDispatches(ctx, db, previous.ID); err != nil {
			return err
		}
		// No reservation is refunded or relabelled. A pre-effect/uncertain token
		// might still be in another native caller's hands and cannot be migrated.
		entries, err := readInstructorBudgetEntries(ctx, db, `SELECT body FROM instructor_budget_entries WHERE work_id=? AND state IN ('reserved','started','uncertain')`, w.ID)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.State != "started" {
				return fmt.Errorf("%w: budget reservation %s is %s", domain.ErrInstructorProducerChanged, entry.ID, entry.State)
			}
			resource := entry.ResourceKey
			if entry.Kind == "turn" {
				var found bool
				resource, _, found = strings.Cut(resource, "/turn:")
				if !found || entry.SessionID == "" {
					return domain.ErrInstructorProducerChanged
				}
			}
			quiet := false
			for _, producer := range proof.Producers {
				if entry.ReviewID == "" && entry.SessionID == producer.SessionID && resource == "session:"+producer.InstanceID {
					quiet = true
				}
				for _, review := range proof.Reviews {
					reviewResource := "session:" + producer.InstanceID + "/review:" + review.ID + ":" + review.CreatedAt.UTC().Format("20060102T150405.999999999Z")
					if entry.ReviewID == review.ID && entry.SessionID == producer.SessionID && resource == reviewResource {
						quiet = true
					}
				}
			}
			if !quiet {
				// A shared coordinator need not terminate. Every work-owned
				// directive was checked above, including the captured generation.
				var count int
				err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions s JOIN instructor_session_instances i ON i.session_id=s.id WHERE s.id=? AND s.kind='orchestrator' AND s.project_id=? AND ?='session:'||i.instance_id AND ?=''`, entry.SessionID, w.ProjectID, resource, entry.ReviewID).Scan(&count)
				if err != nil {
					return err
				}
				quiet = count == 1
				if quiet && entry.Kind == "turn" {
					// A manually submitted coordinator turn has no dispatch row.
					// Its exact native turn must also be terminal; a shared host's
					// idle display state cannot attest earlier turn completion.
					_, turnID, _ := strings.Cut(entry.ResourceKey, "/turn:")
					if err := db.QueryRowContext(ctx, `SELECT count(*) FROM conversation_turns WHERE id=? AND handled_by_session_id=? AND state IN ('completed','interrupted','failed','recovered')`, turnID, entry.SessionID).Scan(&count); err != nil {
						return err
					}
					if count != 1 {
						if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=? AND session_mode!='chat' AND (is_terminated=1 OR activity_state='exited')`, entry.SessionID).Scan(&count); err != nil {
							return err
						}
					}
					quiet = count == 1
				}
			}
			if !quiet {
				return domain.ErrInstructorProducerChanged
			}
		}
		body, err = instructorJSON(w)
		if err != nil {
			return err
		}
		result, err := db.ExecContext(ctx, `UPDATE instructor_works SET revision=?,body=? WHERE id=? AND revision=?`, w.Revision, body, w.ID, expected)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return err
		}
		body, err = instructorJSON(event)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, w.ID, body)
		saved = err == nil
		return err
	})
	return saved, err
}

func validatePolicyRebindProducers(ctx context.Context, q *gen.Queries, db gen.DBTX, w domain.InstructorWork, proof domain.InstructorCancellationProof) error {
	for _, producer := range proof.Producers {
		if producer.WorkID != w.ID || !producer.Member {
			return domain.ErrInstructorProducerChanged
		}
		if err := validateInstructorReportActor(ctx, db, producer); err != nil {
			return err
		}
		row, err := q.GetSession(ctx, producer.SessionID)
		if err != nil {
			return domain.ErrInstructorProducerChanged
		}
		record := rowToRecord(row)
		runtime := record.Metadata.RuntimeLaunchID
		if domain.NormalizeSessionMode(record.Mode) == domain.SessionModeChat {
			runtime = "chat:" + record.Metadata.ControllerGeneration
			var unsettled int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM conversation_turns WHERE handled_by_session_id=? AND state IN ('queued','running')`, record.ID).Scan(&unsettled); err != nil {
				return err
			}
			if unsettled != 0 {
				return domain.ErrInstructorProducerChanged
			}
		} else if !record.IsTerminated && record.Activity.State != domain.ActivityExited {
			return domain.ErrInstructorProducerChanged
		}
		if record.Kind != domain.KindWorker || record.ProjectID != w.ProjectID || runtime != producer.RuntimeID {
			return domain.ErrInstructorProducerChanged
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT body FROM instructor_session_members WHERE work_id=?`, w.ID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var members []domain.InstructorSessionMembership
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return err
		}
		var member domain.InstructorSessionMembership
		if err := json.Unmarshal([]byte(body), &member); err != nil {
			return err
		}
		members = append(members, member)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, id := range w.SessionIDs {
		found := false
		for _, member := range members {
			found = found || member.SessionID == id
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
		for _, producer := range proof.Producers {
			found = found || member.Provenance == "admitted" && producer.SessionID == member.SessionID && producer.InstanceID == member.InstanceID
		}
		if !found {
			return domain.ErrInstructorProducerChanged
		}
	}
	seen := map[string]bool{}
	for _, observed := range proof.Reviews {
		review, err := q.GetReviewByID(ctx, observed.ID)
		if err != nil {
			return domain.ErrInstructorProducerChanged
		}
		runtime := review.ReviewerLaunchID
		if runtime == "" {
			runtime = review.ControllerGeneration
		}
		if review.ProjectID != w.ProjectID || !review.CreatedAt.Equal(observed.CreatedAt) || runtime != observed.RuntimeID || review.ControllerGeneration != observed.ControllerGeneration || review.ReviewerHandleID != observed.HandleID || domain.ReviewerInterfaceMode(review.InterfaceMode) != observed.InterfaceMode || review.ReviewerActivityState != string(domain.ActivityExited) && (!observed.NativeQuiet || observed.InterfaceMode != domain.ReviewerInterfaceChat || observed.ControllerGeneration == "") {
			return domain.ErrInstructorProducerChanged
		}
		seen[observed.ID] = true
	}
	for _, launch := range w.Launches {
		if launch.ReviewID != "" && !seen[launch.ReviewID] {
			return domain.ErrInstructorProducerChanged
		}
	}
	return nil
}

func validatePolicyRebindDispatches(ctx context.Context, db gen.DBTX, workID string) error {
	rows, err := db.QueryContext(ctx, `SELECT body FROM instructor_dispatches WHERE work_id=?`, workID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var dispatches []domain.InstructorDispatch
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return err
		}
		var dispatch domain.InstructorDispatch
		if err := json.Unmarshal([]byte(body), &dispatch); err != nil {
			return err
		}
		dispatches = append(dispatches, dispatch)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, dispatch := range dispatches {
		admitted := false
		switch dispatch.State {
		case "delivered", "accepted", "progress", "started":
			// These transport receipts remain durable after a native turn ends.
			// The exact native turn below, not the receipt label, proves quiet.
			admitted = true
		case "failed", "cancelled":
		default:
			return fmt.Errorf("%w: dispatch %s is %s", domain.ErrInstructorProducerChanged, dispatch.ID, dispatch.State)
		}
		if dispatch.Action == "stopp" || dispatch.Action == "abbrechen" {
			continue // Native producers must independently pass positive quiescence.
		}
		if dispatch.TargetSessionID == "" {
			if admitted || dispatch.NativeReceipt != nil || dispatch.QueuedAt != nil {
				return domain.ErrInstructorProducerChanged
			}
			continue // Failed before target binding; no native admission exists.
		}
		// Failed pre-admission sends can be recovered only with an actual native
		// cancellation fence. Error text and a missing conversation are no proof.
		if dispatch.NativeReceipt == nil && dispatch.QueuedAt == nil && (dispatch.State == "failed" || dispatch.State == "cancelled") {
			var fenced int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM semantic_deliveries WHERE session_id=? AND client_message_id=? AND runtime_launch_id=? AND state='cancelled' AND NOT EXISTS(SELECT 1 FROM conversation_messages WHERE client_message_id=? AND role='user')`, dispatch.TargetSessionID, dispatch.ID, dispatch.TargetRuntimeID, dispatch.ID).Scan(&fenced); err != nil {
				return err
			}
			if fenced == 1 {
				continue
			}
		}
		if strings.HasPrefix(dispatch.TargetRuntimeID, "chat:") {
			var count, unsettled int
			if err := db.QueryRowContext(ctx, `SELECT count(*),COALESCE(SUM(t.state NOT IN ('completed','interrupted','failed','cancelled','recovered')),0) FROM conversation_messages m JOIN conversation_turns t ON t.id=m.turn_id WHERE m.client_message_id=? AND m.role='user' AND t.handled_by_session_id=? AND 'chat:'||t.controller_generation=?`, dispatch.ID, dispatch.TargetSessionID, dispatch.TargetRuntimeID).Scan(&count, &unsettled); err != nil {
				return err
			}
			if count == 0 || unsettled != 0 {
				return fmt.Errorf("%w: dispatch %s has no conclusive terminal native turn", domain.ErrInstructorProducerChanged, dispatch.ID)
			}
		} else if err := cancellationTargetSettled(ctx, db, dispatch); err != nil {
			return err
		}
	}
	return nil
}

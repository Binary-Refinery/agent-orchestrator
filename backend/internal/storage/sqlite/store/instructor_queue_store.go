package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// DeferInstructorDispatch holds a frozen directive while daemon inspection
// catches up. It does not pretend that a native provider received the message.
func (s *Store) DeferInstructorDispatch(ctx context.Context, id, reason string, retryAt time.Time) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "defer instructor directive", func(_ *gen.Queries, db gen.DBTX) error {
		var body string
		if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_dispatches WHERE id=?`, id).Scan(&body); err != nil {
			return err
		}
		var d domain.InstructorDispatch
		if err := json.Unmarshal([]byte(body), &d); err != nil {
			return err
		}
		if d.State != "leased" && d.State != "pending" {
			return nil
		}
		d.State, d.Error = "pending", reason
		body, err := instructorJSON(d)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `UPDATE instructor_dispatches SET state='pending',lease_until=?,body=? WHERE id=?`, retryAt.Unix(), body, id)
		return err
	})
}

// QueueInstructorDispatch atomically journals a native queue receipt and schedules a fenced retry.
func (s *Store) QueueInstructorDispatch(ctx context.Context, dispatch domain.InstructorDispatch, work domain.InstructorWork, expected int64, event domain.InstructorEvent, retryAt time.Time) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "queue instructor delivery", func(_ *gen.Queries, db gen.DBTX) error {
		var state, priorBody string
		if err := db.QueryRowContext(ctx, `SELECT state,body FROM instructor_dispatches WHERE id=?`, dispatch.ID).Scan(&state, &priorBody); err != nil {
			return err
		}
		if state == "accepted" || state == "progress" || state == "started" {
			return nil
		}
		var prior domain.InstructorDispatch
		if err := json.Unmarshal([]byte(priorBody), &prior); err != nil {
			return err
		}
		dispatch.TargetSessionInstanceID, dispatch.TargetRuntimeID, dispatch.TargetCreatedAt, dispatch.NativeReceipt = prior.TargetSessionInstanceID, prior.TargetRuntimeID, prior.TargetCreatedAt, prior.NativeReceipt
		if state == "cancelled" || state == "failed" {
			return errors.New("native queue dispatch was cancelled or fenced before retry")
		}
		if dispatch.WorkID != work.ID || prior.WorkID != work.ID ||
			dispatch.ProjectID != work.ProjectID || prior.ProjectID != work.ProjectID || event.WorkID != work.ID ||
			work.Revision != expected+1 || dispatch.WorkRevision != work.Revision {
			return errors.New("native queue work and dispatch identity or revision changed")
		}
		var previousBody string
		if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_works WHERE id=? AND revision=?`, work.ID, expected).Scan(&previousBody); errors.Is(err, sql.ErrNoRows) {
			return errors.New("instructor work changed before delivery queued")
		} else if err != nil {
			return err
		}
		var previous domain.InstructorWork
		if err := json.Unmarshal([]byte(previousBody), &previous); err != nil {
			return err
		}
		if prior.Action == "worker_result" {
			// Completion wake-ups intentionally survive ordinary checkpoints.
			// Their persisted phase/sequence/policy, not a stale work revision,
			// owns admission; sibling directive carry still uses exact expected.
			if dispatch.Action != prior.Action || prior.Phase != previous.Phase || prior.PhaseSequence != previous.PhaseSequence || prior.PolicyVersion != previous.PolicyVersion ||
				prior.Phase != work.Phase || prior.PhaseSequence != work.PhaseSequence || prior.PolicyVersion != work.PolicyVersion {
				return errors.New("native queue worker-result phase or policy changed")
			}
		} else if prior.WorkRevision != expected {
			return errors.New("native queue operator directive revision changed")
		}
		body, err := instructorJSON(work)
		if err != nil {
			return err
		}
		result, err := db.ExecContext(ctx, `UPDATE instructor_works SET revision=?,body=? WHERE id=? AND revision=?`, work.Revision, body, work.ID, expected)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("instructor work changed before delivery queued")
		}
		if err := preserveInstructorDirectivesAcrossQueue(ctx, db, previous, work, dispatch, event.At); err != nil {
			return err
		}
		body, err = instructorJSON(dispatch)
		if err != nil {
			return err
		}
		if _, err = db.ExecContext(ctx, `UPDATE instructor_dispatches SET state='pending',lease_until=?,body=? WHERE id=?`, retryAt.Unix(), body, dispatch.ID); err != nil {
			return err
		}
		body, err = instructorJSON(event)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, event.WorkID, body)
		return err
	})
}

// A confirmed native FIFO entry changes only transport display and bookkeeping.
// Compare every remaining field before using the shared audit carry, which by
// itself also permits PhaseActions changes and is therefore too broad here.
func preserveInstructorDirectivesAcrossQueue(ctx context.Context, db gen.DBTX, previous, current domain.InstructorWork, dispatch domain.InstructorDispatch, at time.Time) error {
	const queueWait = "Native Nachricht dauerhaft eingereiht; wartet auf bestätigten Empfang"
	if dispatch.QueuedAt == nil || current.State != "waiting_external" || current.WaitCondition != queueWait ||
		current.WaitUntil == nil || !current.WaitUntil.Equal(dispatch.QueuedAt.Add(2*time.Minute)) {
		return nil
	}
	switch previous.State {
	case "ready", "running":
		if previous.WaitCondition != "" || previous.WaitUntil != nil {
			return nil // Do not replace a business wait with executable authority.
		}
	case "waiting_external":
		if previous.WaitCondition != queueWait {
			return nil
		}
	default:
		return nil // Pauses, human gates and closed work never become executable.
	}
	previous.State, previous.WaitCondition, previous.WaitUntil = current.State, current.WaitCondition, current.WaitUntil
	before := previous
	before.Revision, before.UpdatedAt = current.Revision, current.UpdatedAt
	before.Actions, current.Actions = nil, nil
	priorJSON, err := instructorJSON(before)
	if err != nil {
		return err
	}
	currentJSON, err := instructorJSON(current)
	if err != nil {
		return err
	}
	if priorJSON != currentJSON {
		return nil
	}
	// PhaseActions and all business authority matched exactly above. The shared
	// helper carries only same-work/project pending or leased exact revisions.
	return preserveInstructorDirectivesAcrossAudit(ctx, db, previous, current, at, false)
}

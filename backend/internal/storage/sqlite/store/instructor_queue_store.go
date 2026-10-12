package store

import (
	"context"
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
		var state string
		if err := db.QueryRowContext(ctx, `SELECT state FROM instructor_dispatches WHERE id=?`, dispatch.ID).Scan(&state); err != nil {
			return err
		}
		if state == "accepted" || state == "progress" {
			return nil
		}
		if state == "cancelled" || state == "failed" {
			return errors.New("native queue dispatch was cancelled or fenced before retry")
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

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// InstructorSessionInstance returns the immutable inserted-row identity. It
// deliberately differs from controller generations, which change on restore.
func (s *Store) InstructorSessionInstance(ctx context.Context, id domain.SessionID) (string, bool, error) {
	var instance string
	err := s.readDB.QueryRowContext(ctx, `SELECT instance_id FROM instructor_session_instances WHERE session_id=?`, id).Scan(&instance)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return instance, err == nil, err
}

// InstructorSessionForMembership reads row facts and its inserted identity in
// one SQLite snapshot. Separate lookups could pair a deleted row's facts with
// the replacement row's instance key.
func (s *Store) InstructorSessionForMembership(ctx context.Context, id domain.SessionID) (domain.SessionRecord, string, bool, error) {
	tx, err := s.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.SessionRecord{}, "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := s.qr.WithTx(tx).GetSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SessionRecord{}, "", false, nil
	}
	if err != nil {
		return domain.SessionRecord{}, "", false, err
	}
	var instance string
	if err := tx.QueryRowContext(ctx, `SELECT instance_id FROM instructor_session_instances WHERE session_id=?`, id).Scan(&instance); err != nil {
		return domain.SessionRecord{}, "", false, err
	}
	if err := tx.Commit(); err != nil {
		return domain.SessionRecord{}, "", false, err
	}
	return rowToRecord(row), instance, true, nil
}

// ListInstructorSessionMembers retains deleted and superseded incarnations.
func (s *Store) ListInstructorSessionMembers(ctx context.Context, id string) ([]domain.InstructorSessionMembership, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT body FROM instructor_session_members WHERE work_id=? ORDER BY rowid`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []domain.InstructorSessionMembership{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var m domain.InstructorSessionMembership
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AttachInstructorSession atomically fences admission against both the work
// revision and the session row observed by the caller. An old display ID cannot
// attach a concurrently deleted/recreated row.
func (s *Store) AttachInstructorSession(ctx context.Context, w domain.InstructorWork, expected int64, event domain.InstructorEvent, m domain.InstructorSessionMembership) (bool, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return false, err
	}
	defer s.writeMu.Unlock()
	body, err := instructorJSON(w)
	if err != nil {
		return false, err
	}
	eb, err := instructorJSON(event)
	if err != nil {
		return false, err
	}
	updated := false
	err = s.inTxDB(ctx, "attach instructor session", func(_ *gen.Queries, db gen.DBTX) error {
		var current, project string
		if err := db.QueryRowContext(ctx, `SELECT i.instance_id,s.project_id FROM instructor_session_instances i JOIN sessions s ON s.id=i.session_id WHERE s.id=?`, m.SessionID).Scan(&current, &project); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrInstructorSessionInstanceChanged
			}
			return err
		}
		if current != m.InstanceID || project != string(w.ProjectID) {
			return domain.ErrInstructorSessionInstanceChanged
		}
		pinned := w
		pinned.Revision = expected
		resource := "session:" + m.InstanceID
		entry := domain.InstructorBudgetEntry{ID: domain.InstructorBudgetReservationID(w.ID, "session", resource), WorkID: w.ID, PolicyVersion: w.PolicyVersion, Kind: "session", ResourceKey: resource, SessionID: m.SessionID, Delegated: m.ParentSessionID != "", State: "reserved", CreatedAt: event.At, UpdatedAt: event.At}
		entry.AdoptedSession = !m.Prelaunch
		if _, err := reserveInstructorBudgetEntry(ctx, db, pinned, entry); err != nil {
			return err
		}
		result, err := db.ExecContext(ctx, `UPDATE instructor_works SET revision=?,body=? WHERE id=? AND revision=?`, w.Revision, body, w.ID, expected)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		if err := bindInstructorSession(ctx, db, w.ID, m); err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, w.ID, eb)
		if err == nil {
			updated = true
		}
		return err
	})
	return updated, err
}

func bindInstructorSession(ctx context.Context, db gen.DBTX, workID string, m domain.InstructorSessionMembership) error {
	var bound, instance string
	err := db.QueryRowContext(ctx, `SELECT work_id,instance_id FROM instructor_sessions WHERE session_id=?`, m.SessionID).Scan(&bound, &instance)
	if err == nil && (bound != workID || instance != m.InstanceID) {
		return errors.New("session instance already belongs to another work")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	body, err := instructorJSON(m)
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO instructor_sessions(session_id,work_id,instance_id) VALUES(?,?,?)`, m.SessionID, workID, m.InstanceID); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO instructor_session_members(work_id,session_id,instance_id,body) VALUES(?,?,?,?) ON CONFLICT(work_id,instance_id) DO UPDATE SET body=excluded.body WHERE json_extract(instructor_session_members.body,'$.provenance')='legacy_bound'`, workID, m.SessionID, m.InstanceID, body)
	return err
}

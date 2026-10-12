package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func instructorJSON(v any) (string, error) { b, e := json.Marshal(v); return string(b), e }

// GetInstructorPolicy reads the installed control-plane policy.
func (s *Store) GetInstructorPolicy(ctx context.Context) (domain.InstructorPolicy, bool, error) {
	var b string
	e := s.readDB.QueryRowContext(ctx, `SELECT body FROM instructor_policy WHERE id=1`).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return domain.InstructorPolicy{}, false, nil
	}
	var p domain.InstructorPolicy
	if e == nil {
		e = json.Unmarshal([]byte(b), &p)
	}
	return p, e == nil, e
}

// PutInstructorPolicy replaces the installed control-plane policy.
func (s *Store) PutInstructorPolicy(ctx context.Context, p domain.InstructorPolicy) error {
	b, e := instructorJSON(p)
	if e != nil {
		return e
	}
	if e := s.writeMu.LockContext(ctx); e != nil {
		return e
	}
	defer s.writeMu.Unlock()
	_, e = s.writeDB.ExecContext(ctx, `INSERT INTO instructor_policy(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`, b)
	return e
}

// CreateInstructorWork creates a durable work and intake event, reusing its existing source identity.
func (s *Store) CreateInstructorWork(ctx context.Context, w domain.InstructorWork, event domain.InstructorEvent) (domain.InstructorWork, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return w, err
	}
	defer s.writeMu.Unlock()
	body, err := instructorJSON(w)
	if err != nil {
		return w, err
	}
	eb, err := instructorJSON(event)
	if err != nil {
		return w, err
	}
	err = s.inTxDB(ctx, "create instructor work", func(_ *gen.Queries, db gen.DBTX) error {
		var old string
		e := db.QueryRowContext(ctx, `SELECT body FROM instructor_works WHERE project_id=? AND source=? AND source_ref=?`, w.ProjectID, w.Source, w.SourceRef).Scan(&old)
		if e == nil {
			return json.Unmarshal([]byte(old), &w)
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if _, e = db.ExecContext(ctx, `INSERT INTO instructor_works(id,project_id,source,source_ref,revision,body) VALUES(?,?,?,?,?,?)`, w.ID, w.ProjectID, w.Source, w.SourceRef, w.Revision, body); e != nil {
			return e
		}
		_, e = db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, w.ID, eb)
		return e
	})
	return w, err
}

// GetInstructorWork reads one durable work by its stable identity.
func (s *Store) GetInstructorWork(ctx context.Context, id string) (domain.InstructorWork, bool, error) {
	var body string
	err := s.readDB.QueryRowContext(ctx, `SELECT body FROM instructor_works WHERE id=?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InstructorWork{}, false, nil
	}
	if err != nil {
		return domain.InstructorWork{}, false, err
	}
	var w domain.InstructorWork
	err = json.Unmarshal([]byte(body), &w)
	return w, err == nil, err
}

// InstructorWorkForSession resolves the durable work explicitly bound to a session.
func (s *Store) InstructorWorkForSession(ctx context.Context, id domain.SessionID) (domain.InstructorWork, bool, error) {
	var body string
	err := s.readDB.QueryRowContext(ctx, `SELECT w.body FROM instructor_works w JOIN instructor_sessions s ON s.work_id=w.id WHERE s.session_id=?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InstructorWork{}, false, nil
	}
	if err != nil {
		return domain.InstructorWork{}, false, err
	}
	var w domain.InstructorWork
	err = json.Unmarshal([]byte(body), &w)
	return w, err == nil, err
}

// ListInstructorWorks reads all durable works in project and identity order.
func (s *Store) ListInstructorWorks(ctx context.Context) ([]domain.InstructorWork, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT body FROM instructor_works ORDER BY project_id,id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []domain.InstructorWork{}
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var w domain.InstructorWork
		if err := json.Unmarshal([]byte(b), &w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// SaveInstructorWork updates the expected work revision and its session bindings atomically with an event.
func (s *Store) SaveInstructorWork(ctx context.Context, w domain.InstructorWork, expected int64, event domain.InstructorEvent) (bool, error) {
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
	err = s.inTxDB(ctx, "save instructor work", func(_ *gen.Queries, db gen.DBTX) error {
		result, e := db.ExecContext(ctx, `UPDATE instructor_works SET revision=?,body=? WHERE id=? AND revision=?`, w.Revision, body, w.ID, expected)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil || n == 0 {
			return e
		}
		updated = true
		for _, id := range w.SessionIDs {
			var project string
			if e = db.QueryRowContext(ctx, `SELECT project_id FROM sessions WHERE id=?`, id).Scan(&project); errors.Is(e, sql.ErrNoRows) {
				continue
			} else if e != nil {
				return e
			}
			if project != string(w.ProjectID) {
				return errors.New("session belongs to another project")
			}
			var bound string
			e = db.QueryRowContext(ctx, `SELECT work_id FROM instructor_sessions WHERE session_id=?`, id).Scan(&bound)
			if e == nil && bound != w.ID {
				return errors.New("session already belongs to another work")
			}
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if _, e = db.ExecContext(ctx, `INSERT OR IGNORE INTO instructor_sessions(session_id,work_id) VALUES(?,?)`, id, w.ID); e != nil {
				return e
			}
		}
		_, e = db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, w.ID, eb)
		return e
	})
	return updated, err
}

// PutInstructorSnapshot records an immutable operator command snapshot.
func (s *Store) PutInstructorSnapshot(ctx context.Context, v domain.InstructorSnapshot) error {
	body, err := instructorJSON(v)
	if err != nil {
		return err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	_, err = s.writeDB.ExecContext(ctx, `INSERT INTO instructor_snapshots(id,body) VALUES(?,?)`, v.ID, body)
	return err
}

// GetInstructorSnapshot reads a previously captured operator command snapshot.
func (s *Store) GetInstructorSnapshot(ctx context.Context, id string) (domain.InstructorSnapshot, bool, error) {
	var b string
	e := s.readDB.QueryRowContext(ctx, `SELECT body FROM instructor_snapshots WHERE id=?`, id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return domain.InstructorSnapshot{}, false, nil
	}
	var v domain.InstructorSnapshot
	if e == nil {
		e = json.Unmarshal([]byte(b), &v)
	}
	return v, e == nil, e
}

// ListInstructorEvents reads a work’s immutable events in journal order.
func (s *Store) ListInstructorEvents(ctx context.Context, id string) ([]domain.InstructorEvent, error) {
	rows, e := s.readDB.QueryContext(ctx, `SELECT body FROM instructor_events WHERE work_id=? ORDER BY rowid`, id)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []domain.InstructorEvent{}
	for rows.Next() {
		var b string
		if e := rows.Scan(&b); e != nil {
			return nil, e
		}
		var v domain.InstructorEvent
		if e := json.Unmarshal([]byte(b), &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// EnqueueInstructorDispatches adds an ordered dispatch batch after checking its work revisions.
func (s *Store) EnqueueInstructorDispatches(ctx context.Context, ds []domain.InstructorDispatch) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "enqueue instructor batch", func(_ *gen.Queries, db gen.DBTX) error {
		for _, d := range ds {
			var revision int64
			if e := db.QueryRowContext(ctx, `SELECT revision FROM instructor_works WHERE id=?`, d.WorkID).Scan(&revision); e != nil {
				return e
			}
			if revision != d.WorkRevision {
				return errors.New("instructor work changed before enqueue")
			}
			b, e := instructorJSON(d)
			if e != nil {
				return e
			}
			if _, e = db.ExecContext(ctx, `INSERT INTO instructor_dispatches(id,batch_id,work_id,position,state,body) VALUES(?,?,?,?,?,?)`, d.ID, d.BatchID, d.WorkID, d.Position, d.State, b); e != nil {
				return e
			}
		}
		return nil
	})
}

// CommitInstructorBatch binds the frozen command, its grants/stop facts and
// outbox rows in one writer transaction. Any changed selected work aborts all.
func (s *Store) CommitInstructorBatch(ctx context.Context, ds []domain.InstructorDispatch, works []domain.InstructorWork, events []domain.InstructorEvent, cancelWorks []string) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "commit instructor batch", func(_ *gen.Queries, db gen.DBTX) error {
		updated := map[string]bool{}
		for _, w := range works {
			body, e := instructorJSON(w)
			if e != nil {
				return e
			}
			result, e := db.ExecContext(ctx, `UPDATE instructor_works SET revision=?,body=? WHERE id=? AND revision=?`, w.Revision, body, w.ID, w.Revision-1)
			if e != nil {
				return e
			}
			n, e := result.RowsAffected()
			if e != nil {
				return e
			}
			if n != 1 {
				return errors.New("instructor work changed before command commit")
			}
			updated[w.ID] = true
		}
		for _, event := range events {
			b, e := instructorJSON(event)
			if e != nil {
				return e
			}
			if _, e = db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, event.WorkID, b); e != nil {
				return e
			}
		}
		for _, id := range cancelWorks {
			cancelled, err := readInstructorDispatches(ctx, db, `SELECT body FROM instructor_dispatches WHERE work_id=? AND state IN ('pending','leased')`, id)
			if err != nil {
				return err
			}
			for _, d := range cancelled {
				d.State = "cancelled"
				d.Error = "operator stopped work"
				b, e := instructorJSON(d)
				if e != nil {
					return e
				}
				if _, e = db.ExecContext(ctx, `UPDATE instructor_dispatches SET state='cancelled',body=? WHERE id=?`, b, d.ID); e != nil {
					return e
				}
			}
		}
		for _, d := range ds {
			var revision int64
			if e := db.QueryRowContext(ctx, `SELECT revision FROM instructor_works WHERE id=?`, d.WorkID).Scan(&revision); e != nil {
				return e
			}
			if revision != d.WorkRevision {
				return errors.New("instructor work changed before dispatch commit")
			}
			b, e := instructorJSON(d)
			if e != nil {
				return e
			}
			if _, e = db.ExecContext(ctx, `INSERT INTO instructor_dispatches(id,batch_id,work_id,position,state,body) VALUES(?,?,?,?,?,?)`, d.ID, d.BatchID, d.WorkID, d.Position, d.State, b); e != nil {
				return e
			}
		}
		return nil
	})
}

// ListInstructorDispatches reads ordered dispatch records matching optional batch and work filters.
func (s *Store) ListInstructorDispatches(ctx context.Context, batchID, workID string) ([]domain.InstructorDispatch, error) {
	return readInstructorDispatches(ctx, s.readDB, `SELECT body FROM instructor_dispatches WHERE (?='' OR batch_id=?) AND (?='' OR work_id=?) ORDER BY rowid`, batchID, batchID, workID, workID)
}

// ClaimInstructorDispatch leases the next eligible dispatch whose earlier batch positions are resolved.
func (s *Store) ClaimInstructorDispatch(ctx context.Context, at time.Time) (domain.InstructorDispatch, bool, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.InstructorDispatch{}, false, err
	}
	defer s.writeMu.Unlock()
	var d domain.InstructorDispatch
	claimed := false
	err := s.inTxDB(ctx, "claim instructor dispatch", func(_ *gen.Queries, db gen.DBTX) error {
		var b string
		e := db.QueryRowContext(ctx, `SELECT d.body FROM instructor_dispatches d WHERE ((d.state='pending' AND d.lease_until<=?) OR (d.state='leased' AND d.lease_until<=?)) AND NOT EXISTS(SELECT 1 FROM instructor_dispatches p WHERE p.batch_id=d.batch_id AND p.position<d.position AND p.state IN ('pending','leased','failed','cancelled')) ORDER BY d.rowid LIMIT 1`, at.Unix(), at.Unix()).Scan(&b)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if e := json.Unmarshal([]byte(b), &d); e != nil {
			return e
		}
		d.State = "leased"
		d.Attempts++
		d.UpdatedAt = at
		b, e = instructorJSON(d)
		if e != nil {
			return e
		}
		_, e = db.ExecContext(ctx, `UPDATE instructor_dispatches SET state='leased',lease_until=?,body=? WHERE id=?`, at.Add(2*time.Minute).Unix(), b, d.ID)
		claimed = e == nil
		return e
	})
	return d, claimed, err
}

// SetInstructorDispatch records delivery state and cancels pending batch positions after failure.
func (s *Store) SetInstructorDispatch(ctx context.Context, id, state, detail string, at time.Time) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "update instructor dispatch", func(_ *gen.Queries, db gen.DBTX) error {
		var b string
		if e := db.QueryRowContext(ctx, `SELECT body FROM instructor_dispatches WHERE id=?`, id).Scan(&b); e != nil {
			return e
		}
		var d domain.InstructorDispatch
		if e := json.Unmarshal([]byte(b), &d); e != nil {
			return e
		}
		if (d.State == "progress" && (state == "delivered" || state == "accepted" || state == "failed")) || (d.State == "accepted" && (state == "delivered" || state == "failed")) {
			return nil
		}
		if d.State == "failed" || d.State == "cancelled" {
			return nil
		}
		d.State = state
		d.Error = detail
		d.UpdatedAt = at
		b, e := instructorJSON(d)
		if e != nil {
			return e
		}
		if _, e = db.ExecContext(ctx, `UPDATE instructor_dispatches SET state=?,lease_until=0,body=? WHERE id=?`, state, b, id); e != nil {
			return e
		}
		if state == "failed" {
			remaining, err := readInstructorDispatches(ctx, db, `SELECT body FROM instructor_dispatches WHERE batch_id=? AND state='pending'`, d.BatchID)
			if err != nil {
				return err
			}
			for _, v := range remaining {
				v.State = "cancelled"
				v.Error = fmt.Sprintf("batch stopped after %s", id)
				v.UpdatedAt = at
				body, e := instructorJSON(v)
				if e != nil {
					return e
				}
				if _, e = db.ExecContext(ctx, `UPDATE instructor_dispatches SET state='cancelled',body=? WHERE id=?`, body, v.ID); e != nil {
					return e
				}
			}
		}
		return nil
	})
}

// ReconcileInstructorDispatch is used only after a scoped operator observation
// or an actual receipt from the dispatch's immutable bound target.
func (s *Store) ReconcileInstructorDispatch(ctx context.Context, id, state, detail string, at time.Time) error {
	if state != "delivered" && state != "accepted" && state != "progress" && state != "cancelled" {
		return errors.New("invalid reconciled delivery state")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "reconcile instructor dispatch", func(_ *gen.Queries, db gen.DBTX) error {
		var body string
		if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_dispatches WHERE id=?`, id).Scan(&body); err != nil {
			return err
		}
		var d domain.InstructorDispatch
		if err := json.Unmarshal([]byte(body), &d); err != nil {
			return err
		}
		if d.State == "progress" || d.State == "accepted" && state == "delivered" {
			return nil
		}
		d.State, d.Error, d.UpdatedAt = state, detail, at
		body, err := instructorJSON(d)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `UPDATE instructor_dispatches SET state=?,lease_until=0,body=? WHERE id=?`, state, body, id)
		return err
	})
}

// BindInstructorDispatchTarget pins one immutable delivery target before the external send.
func (s *Store) BindInstructorDispatchTarget(ctx context.Context, id string, target domain.SessionID) error {
	if target == "" {
		return errors.New("dispatch target required")
	}
	if e := s.writeMu.LockContext(ctx); e != nil {
		return e
	}
	defer s.writeMu.Unlock()
	var b string
	if e := s.writeDB.QueryRowContext(ctx, `SELECT body FROM instructor_dispatches WHERE id=?`, id).Scan(&b); e != nil {
		return e
	}
	var d domain.InstructorDispatch
	if e := json.Unmarshal([]byte(b), &d); e != nil {
		return e
	}
	if d.TargetSessionID != "" && d.TargetSessionID != target {
		return errors.New("dispatch target already bound")
	}
	d.TargetSessionID = target
	b, e := instructorJSON(d)
	if e != nil {
		return e
	}
	_, e = s.writeDB.ExecContext(ctx, `UPDATE instructor_dispatches SET body=? WHERE id=?`, b, id)
	return e
}

// readInstructorDispatches closes each result set before its caller performs
// another statement on the transaction's connection.
func readInstructorDispatches(ctx context.Context, db gen.DBTX, query string, args ...any) ([]domain.InstructorDispatch, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	dispatches := []domain.InstructorDispatch{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var dispatch domain.InstructorDispatch
		if err := json.Unmarshal([]byte(body), &dispatch); err != nil {
			return nil, err
		}
		dispatches = append(dispatches, dispatch)
	}
	return dispatches, rows.Err()
}

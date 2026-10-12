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
	return s.instructorWorkForSession(ctx, id, "")
}

// InstructorWorkForSessionInstance rejects a binding that changed after its
// caller captured the inserted session row. Progress in the same work does not
// invalidate this identity observation.
func (s *Store) InstructorWorkForSessionInstance(ctx context.Context, id domain.SessionID, instance string) (domain.InstructorWork, bool, error) {
	if instance == "" {
		return domain.InstructorWork{}, false, nil
	}
	return s.instructorWorkForSession(ctx, id, instance)
}

func (s *Store) instructorWorkForSession(ctx context.Context, id domain.SessionID, instance string) (domain.InstructorWork, bool, error) {
	query := `SELECT w.body FROM instructor_works w JOIN instructor_sessions s ON s.work_id=w.id JOIN instructor_session_instances i ON i.session_id=s.session_id AND i.instance_id=s.instance_id WHERE s.session_id=?`
	args := []any{id}
	if instance != "" {
		query += ` AND s.instance_id=?`
		args = append(args, instance)
	}
	var body string
	err := s.readDB.QueryRowContext(ctx, query, args...).Scan(&body)
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

// SaveInstructorWork updates the expected work revision atomically with an event.
// Session admission is separate and incarnation-fenced; historical IDs are inert.
func (s *Store) SaveInstructorWork(ctx context.Context, w domain.InstructorWork, expected int64, event domain.InstructorEvent) (bool, error) {
	return s.saveInstructorWork(ctx, w, expected, event, false)
}

// SaveInstructorPhaseActionAudit preserves current directives across terminal
// action metadata, without authorizing an older operator intent.
func (s *Store) SaveInstructorPhaseActionAudit(ctx context.Context, w domain.InstructorWork, expected int64, event domain.InstructorEvent) (bool, error) {
	if event.Kind != "phase_action_completed" && event.Kind != "phase_action_recovered" {
		return false, errors.New("only terminal phase action audits may preserve a current directive")
	}
	return s.saveInstructorWork(ctx, w, expected, event, true)
}

func (s *Store) saveInstructorWork(ctx context.Context, w domain.InstructorWork, expected int64, event domain.InstructorEvent, preserveDirectives bool) (bool, error) {
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
		var previous domain.InstructorWork
		if preserveDirectives {
			var priorBody string
			if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_works WHERE id=? AND revision=?`, w.ID, expected).Scan(&priorBody); errors.Is(err, sql.ErrNoRows) {
				return nil
			} else if err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(priorBody), &previous); err != nil {
				return err
			}
		}
		result, e := db.ExecContext(ctx, `UPDATE instructor_works SET revision=?,body=? WHERE id=? AND revision=?`, w.Revision, body, w.ID, expected)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil || n == 0 {
			return e
		}
		updated = true
		// Ordinary work saves must never reconstruct associations from the
		// historical SessionIDs list. Admission is explicit and incarnation-fenced.
		if preserveDirectives {
			if err := preserveInstructorDirectivesAcrossAudit(ctx, db, previous, w, event.At, false); err != nil {
				return err
			}
		}

		_, e = db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, w.ID, eb)
		return e
	})
	return updated, err
}

func preserveInstructorDirectivesAcrossAudit(ctx context.Context, db gen.DBTX, previous, current domain.InstructorWork, at time.Time, onlyStop bool) error {
	if current.Revision != previous.Revision+1 {
		return nil
	}
	expected := previous.Revision
	if onlyStop {
		if previous.State != "parked" || current.State != "parked" {
			return nil
		}
		// A running authorized profile may report failure/revoke old green proof
		// after Stop. This preserves only the restrictive pause, never Go/Weiter.
		previous.Summary, previous.Evidence = current.Summary, current.Evidence
	}
	// Only the action audit and ordinary save bookkeeping may differ. Changes
	// to business state, gates, policy, producer bindings or command intent fence delivery.
	previous.PhaseActions = current.PhaseActions
	previous.Revision, previous.UpdatedAt = current.Revision, current.UpdatedAt
	previous.Actions, current.Actions = nil, nil // Derived menu entries carry no command authority.
	before, err := instructorJSON(previous)
	if err != nil {
		return err
	}
	after, err := instructorJSON(current)
	if err != nil {
		return err
	}
	if before != after {
		return nil
	}
	dispatches, err := readInstructorDispatches(ctx, db, `SELECT body FROM instructor_dispatches WHERE work_id=? AND state IN ('pending','leased')`, current.ID)
	if err != nil {
		return err
	}
	for _, dispatch := range dispatches {
		if dispatch.ProjectID != current.ProjectID || dispatch.WorkRevision != expected || onlyStop && dispatch.Action != "stopp" {
			continue
		}
		dispatch.WorkRevision, dispatch.UpdatedAt = current.Revision, at
		body, err := instructorJSON(dispatch)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `UPDATE instructor_dispatches SET body=? WHERE id=?`, body, dispatch.ID); err != nil {
			return err
		}
	}
	return nil
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
		nativeDelivered := state == "delivered"
		if d.State == "started" && !nativeDelivered {
			return nil
		}
		startFailure := state == "failed" && d.ReportID != "" && (d.NativeReceipt == nil || d.NativeReceipt.StartedAt == nil)
		if !nativeDelivered && !startFailure && ((d.State == "progress" && (state == "accepted" || state == "failed")) || (d.State == "accepted" && state == "failed")) {
			return nil
		}
		if d.State == "failed" || d.State == "cancelled" {
			return nil
		}
		if nativeDelivered && (d.State == "accepted" || d.State == "progress" || d.State == "started") {
			state = d.State
		}
		d.State = state
		d.Error = detail
		d.UpdatedAt = at
		if nativeDelivered {
			if d.NativeReceipt == nil {
				d.NativeReceipt = &domain.InstructorNativeReceipt{RuntimeID: d.TargetRuntimeID}
			}
			if d.NativeReceipt.DeliveredAt == nil {
				d.NativeReceipt.DeliveredAt = &at
			}
			if d.NativeReceipt.AcceptedAt == nil {
				d.NativeReceipt.AcceptedAt = &at
			}
		}
		b, e := instructorJSON(d)
		if e != nil {
			return e
		}
		if _, e = db.ExecContext(ctx, `UPDATE instructor_dispatches SET state=?,lease_until=0,body=? WHERE id=?`, instructorDispatchIndexState(state), b, id); e != nil {
			return e
		}
		if state == "failed" {
			remaining, err := readInstructorDispatches(ctx, db, `SELECT body FROM instructor_dispatches WHERE batch_id=? AND position>? AND state IN ('pending','leased')`, d.BatchID, d.Position)
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
	if state != "delivered" && state != "accepted" && state != "progress" && state != "started" && state != "cancelled" {
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
		if err := validateInstructorReportActor(ctx, db, domain.InstructorReportAdmission{WorkID: d.WorkID, SessionID: d.TargetSessionID, InstanceID: d.TargetSessionInstanceID, DispatchID: d.ID}); err != nil {
			return err
		}
		if d.State == "started" || d.State == "progress" || d.State == "accepted" && state == "delivered" {
			return nil
		}
		d.State, d.Error, d.UpdatedAt = state, detail, at
		body, err := instructorJSON(d)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `UPDATE instructor_dispatches SET state=?,lease_until=0,body=? WHERE id=?`, instructorDispatchIndexState(state), body, id)
		return err
	})
}

// The historical index vocabulary groups all settled receipt states under
// accepted. The full JSON record preserves the more precise native started
// fact, without modifying an already shipped migration's CHECK constraint.
func instructorDispatchIndexState(state string) string {
	if state == "started" {
		return "accepted"
	}
	return state
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
	var instance string
	if e := s.writeDB.QueryRowContext(ctx, `SELECT i.instance_id FROM instructor_session_instances i JOIN sessions r ON r.id=i.session_id WHERE r.id=? AND r.project_id=?`, target, d.ProjectID).Scan(&instance); e != nil {
		return e
	}
	if d.TargetSessionID != "" && d.TargetSessionInstanceID != instance {
		return errors.New("bound dispatch session instance changed or is legacy unknown")
	}
	d.TargetSessionID = target
	d.TargetSessionInstanceID = instance
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

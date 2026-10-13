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

// AdmitInstructorReport captures the association and incarnation in one SQL
// snapshot. Separate WorkForSession and Instance reads permit a reused-id ABA.
func (s *Store) AdmitInstructorReport(ctx context.Context, workID string, id domain.SessionID, dispatchID string) (domain.InstructorReportAdmission, bool, error) {
	a := domain.InstructorReportAdmission{WorkID: workID, SessionID: id, DispatchID: dispatchID}
	var member, target bool
	err := s.readDB.QueryRowContext(ctx, `SELECT i.instance_id,
 EXISTS(SELECT 1 FROM instructor_sessions b WHERE b.session_id=i.session_id AND b.instance_id=i.instance_id AND b.work_id=?),
 EXISTS(SELECT 1 FROM instructor_dispatches d WHERE d.id=? AND d.work_id=? AND json_extract(d.body,'$.targetSessionId')=i.session_id AND json_extract(d.body,'$.targetSessionInstanceId')=i.instance_id)
 FROM instructor_session_instances i WHERE i.session_id=?`, workID, dispatchID, workID, id).Scan(&a.InstanceID, &member, &target)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, err
	}
	a.Member = member
	if dispatchID != "" {
		return a, target, nil
	}
	return a, member, nil
}

func validateInstructorReportActor(ctx context.Context, db gen.DBTX, a domain.InstructorReportAdmission) error {
	var count int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM instructor_session_instances i WHERE i.session_id=? AND i.instance_id=? AND
 ((?='' AND EXISTS(SELECT 1 FROM instructor_sessions b WHERE b.session_id=i.session_id AND b.instance_id=i.instance_id AND b.work_id=?)) OR
 (?!='' AND EXISTS(SELECT 1 FROM instructor_dispatches d WHERE d.id=? AND d.work_id=? AND json_extract(d.body,'$.targetSessionId')=i.session_id AND json_extract(d.body,'$.targetSessionInstanceId')=i.instance_id)))`, a.SessionID, a.InstanceID, a.DispatchID, a.WorkID, a.DispatchID, a.DispatchID, a.WorkID).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.ErrInstructorProducerChanged
	}
	if a.Harness != "" {
		row, err := gen.New(db).GetSession(ctx, a.SessionID)
		if err != nil {
			return err
		}
		rec := rowToRecord(row)
		runtimeID := rec.Metadata.RuntimeLaunchID
		if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
			runtimeID = "chat:" + rec.Metadata.ControllerGeneration
		}
		if rec.Kind != domain.KindWorker || runtimeID != a.RuntimeID || rec.Metadata.WorkspacePath != a.WorkspacePath || rec.Metadata.WorkspaceRepoPath != a.WorkspaceRepoPath || rec.Harness != a.Harness || rec.Metadata.Model != a.Model || rec.Metadata.Effort != a.Effort {
			return domain.ErrInstructorProducerChanged
		}
	}
	return nil
}

// SaveInstructorReport fences the captured producer under the writer transaction
// that changes the work, its dispatch receipt and event. Work revision alone
// cannot detect deletion/recreation of a display session ID.
func (s *Store) SaveInstructorReport(ctx context.Context, w domain.InstructorWork, expected int64, event domain.InstructorEvent, a domain.InstructorReportAdmission, receiptState string) (bool, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return false, err
	}
	defer s.writeMu.Unlock()
	updated := false
	err := s.inTxDB(ctx, "save admitted instructor report", func(_ *gen.Queries, db gen.DBTX) error {
		if w.ID != a.WorkID {
			return errors.New("report work admission mismatch")
		}
		if err := validateInstructorReportActor(ctx, db, a); err != nil {
			return err
		}
		var previous domain.InstructorWork
		preserveStop := w.State == "parked" && (event.Kind == "check_executed" || event.Kind == "check_failed")
		preserveReceipt := a.DispatchID != "" && (receiptState == "accepted" || receiptState == "progress")
		if preserveStop || preserveReceipt {
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
		body, err := instructorJSON(w)
		if err != nil {
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
		if preserveStop {
			if err := preserveInstructorDirectivesAcrossAudit(ctx, db, previous, w, event.At, true); err != nil {
				return err
			}
		}
		if preserveReceipt {
			if err := preserveInstructorDirectivesAcrossReceipt(ctx, db, previous, w, event.At); err != nil {
				return err
			}
		}
		if a.DispatchID != "" && (receiptState == "accepted" || receiptState == "progress") {
			var b string
			if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_dispatches WHERE id=?`, a.DispatchID).Scan(&b); err != nil {
				return err
			}
			var d domain.InstructorDispatch
			if err := json.Unmarshal([]byte(b), &d); err != nil {
				return err
			}
			if d.State != "started" && (d.State != "progress" || receiptState == "progress") {
				d.State, d.Error, d.UpdatedAt = receiptState, "", event.At
				b, err = instructorJSON(d)
				if err != nil {
					return err
				}
				if _, err := db.ExecContext(ctx, `UPDATE instructor_dispatches SET state=?,lease_until=0,body=? WHERE id=?`, receiptState, b, d.ID); err != nil {
					return err
				}
			}
		}
		body, err = instructorJSON(event)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, w.ID, body); err != nil {
			return err
		}
		updated = true
		return nil
	})
	return updated, err
}

// A bound receipt can arrive after the engine has queued the next phase. Its
// informational update must not stale that current directive. Carry only the
// exact prior revision forward, in the same transaction as the fenced report;
// immutable human snapshots and already-stale directives retain their revision.
func preserveInstructorDirectivesAcrossReceipt(ctx context.Context, db gen.DBTX, previous, current domain.InstructorWork, at time.Time) error {
	previous.Summary, previous.Next = current.Summary, current.Next
	previous.ProgressAt, previous.NudgedAt = current.ProgressAt, current.NudgedAt
	// Ready/running and a cleared native-queue wait describe receipt/progress,
	// not a new execution grant. Paused states and external business waits are
	// never normalized. Every other field (including gates, phase, commit,
	// evidence, policy, grants and bindings) must still compare exactly.
	wasExecutable := previous.State == "ready" || previous.State == "running"
	wasNativeQueue := previous.State == "waiting_external" && strings.HasPrefix(previous.WaitCondition, "Native Nachricht")
	isExecutable := current.State == "ready" || current.State == "running"
	if (wasExecutable || wasNativeQueue) && isExecutable && current.WaitCondition == "" && current.WaitUntil == nil {
		previous.State, previous.WaitCondition, previous.WaitUntil = current.State, current.WaitCondition, current.WaitUntil
	}
	// Unlike phase-action audits, receipts may not change phase action authority.
	// Compare that field before using the shared exact-revision carry-forward.
	beforeActions, err := instructorJSON(previous.PhaseActions)
	if err != nil {
		return err
	}
	afterActions, err := instructorJSON(current.PhaseActions)
	if err != nil || beforeActions != afterActions {
		return err
	}
	return preserveInstructorDirectivesAcrossAudit(ctx, db, previous, current, at, false)
}

// PutInstructorHeartbeatForReport atomically fences the admitted producer before recording availability.
func (s *Store) PutInstructorHeartbeatForReport(ctx context.Context, h domain.InstructorHeartbeat, a domain.InstructorReportAdmission) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "record admitted producer availability", func(_ *gen.Queries, db gen.DBTX) error {
		if h.WorkID != a.WorkID || h.SessionID != a.SessionID {
			return errors.New("heartbeat admission mismatch")
		}
		if err := validateInstructorReportActor(ctx, db, a); err != nil {
			return err
		}
		body, err := instructorJSON(h)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO instructor_heartbeats(work_id,session_id,body) VALUES(?,?,?) ON CONFLICT(work_id,session_id) DO UPDATE SET body=excluded.body`, h.WorkID, h.SessionID, body)
		return err
	})
}

// ResolveInstructorSemanticDelivery atomically rejects a reused target before
// changing the native ledger; it cannot acknowledge a replacement's delivery.
func (s *Store) ResolveInstructorSemanticDelivery(ctx context.Context, d domain.InstructorDispatch, accepted bool) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "resolve admitted native receipt", func(q *gen.Queries, db gen.DBTX) error {
		a := domain.InstructorReportAdmission{WorkID: d.WorkID, SessionID: d.TargetSessionID, InstanceID: d.TargetSessionInstanceID, DispatchID: d.ID}
		if err := validateInstructorReportActor(ctx, db, a); err != nil {
			return err
		}
		record, err := q.GetSemanticDelivery(ctx, gen.GetSemanticDeliveryParams{SessionID: string(d.TargetSessionID), ClientMessageID: d.ID})
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if record.RuntimeLaunchID != d.TargetRuntimeID && d.TargetRuntimeID != "" {
			return errors.New("native receipt runtime changed")
		}
		if !accepted && record.State == "accepted" {
			return errors.New("accepted native delivery cannot become unsent")
		}
		state := "cancelled"
		at := sql.NullString{}
		if accepted {
			state = "accepted"
			at = sql.NullString{String: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}
		}
		_, err = q.ResolveSemanticDelivery(ctx, gen.ResolveSemanticDeliveryParams{SessionID: string(d.TargetSessionID), ClientMessageID: d.ID, State: state, AcceptedAt: at})
		return err
	})
}

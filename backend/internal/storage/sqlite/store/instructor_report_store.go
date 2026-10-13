package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// CreateInstructorWorkerReport transfers one completion claim to the canonical
// Instructor outbox in the same transaction that persists the report. Ordinary
// batching never owns this claim, including after daemon restart.
func (s *Store) CreateInstructorWorkerReport(ctx context.Context, rec domain.ReportRecord, captured domain.InstructorWork) (domain.ReportRecord, error) {
	if err := rec.Validate(); err != nil {
		return rec, err
	}
	if rec.State != domain.ReportDone || captured.ID == "" {
		return rec, errors.New("managed worker completion report required")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return rec, err
	}
	defer s.writeMu.Unlock()
	var created gen.Report
	err := s.inTxDB(ctx, "record worker completion and required orchestrator turn", func(q *gen.Queries, db gen.DBTX) error {
		var body string
		if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_works WHERE id=? AND revision=?`, captured.ID, captured.Revision).Scan(&body); err != nil {
			return err
		}
		var w domain.InstructorWork
		if err := json.Unmarshal([]byte(body), &w); err != nil {
			return err
		}
		if w.Cancellation != nil || w.ProjectID != rec.ProjectID || w.State == "done" || w.State == "cancelled" {
			return errors.New("completion claim does not belong to active work")
		}
		var instance string
		if err := db.QueryRowContext(ctx, `SELECT i.instance_id FROM instructor_session_instances i JOIN instructor_sessions b ON b.session_id=i.session_id AND b.instance_id=i.instance_id WHERE b.work_id=? AND i.session_id=?`, w.ID, rec.SessionID).Scan(&instance); err != nil {
			return errors.New("completion producer no longer belongs to this work incarnation")
		}
		bound := false
		for _, launch := range captured.Launches {
			if launch.SessionID == rec.SessionID && launch.SessionInstanceID == instance {
				bound = true
			}
		}
		if !bound {
			return errors.New("completion producer identity changed since admission")
		}
		session, err := q.GetSession(ctx, rec.SessionID)
		if err != nil {
			return err
		}
		runtimeID := session.RuntimeLaunchID
		if session.SessionMode == domain.SessionModeChat {
			runtimeID = "chat:" + session.ControllerGeneration
		}
		if runtimeID == "" || runtimeID == "chat:" {
			return errors.New("completion producer has no current runtime incarnation")
		}
		created, err = q.CreateReport(ctx, gen.CreateReportParams{ID: rec.ID, SessionID: string(rec.SessionID), ProjectID: string(rec.ProjectID), State: string(rec.State), Note: rec.Note, Message: rec.Message, CreatedAt: rec.CreatedAt, AvailableAt: rec.AvailableAt, SettlementDeadline: nullTime(rec.SettlementDeadline), RepeatCount: rec.RepeatCount})
		if err != nil {
			return err
		}
		for i, output := range rec.Outputs {
			if err := q.CreateReportOutput(ctx, gen.CreateReportOutputParams{ReportID: rec.ID, Position: int64(i), Kind: string(output.Kind), Reference: output.Reference, Label: output.Label}); err != nil {
				return err
			}
		}
		id := "instructor-report:" + rec.ID
		d := domain.InstructorDispatch{ID: id, BatchID: id, WorkID: w.ID, ProjectID: w.ProjectID, ReportID: rec.ID, ProducerSessionID: rec.SessionID, ProducerRuntimeID: runtimeID, Phase: w.Phase, PhaseSequence: w.PhaseSequence, PolicyVersion: w.PolicyVersion, WorkRevision: w.Revision, Action: "worker_result", State: "pending", CreatedAt: rec.CreatedAt, UpdatedAt: rec.CreatedAt,
			Message: fmt.Sprintf("Worker completion claim report=%s work=%s phase=%s sequence=%d producer=%s runtime=%s. Read the durable report and drive required checks, independent review, triage, approved bounded fixes and delta through the pinned Instructor workflow. This report is not verified evidence and never completes work. Worker note: %s", rec.ID, w.ID, w.Phase, w.PhaseSequence, rec.SessionID, runtimeID, rec.Note)}
		for _, output := range rec.Outputs {
			d.Message += fmt.Sprintf("\nUnverified worker output kind=%s ref=%s label=%s", output.Kind, output.Reference, output.Label)
		}
		body, err = instructorJSON(d)
		if err != nil {
			return err
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO instructor_dispatches(id,batch_id,work_id,position,state,body) VALUES(?,?,?,?,?,?)`, d.ID, d.BatchID, d.WorkID, 0, d.State, body); err != nil {
			return err
		}
		if _, err = db.ExecContext(ctx, `UPDATE reports SET delivery_state='claimed',claim_token=?,claimed_at=?,delivery_batch_id=?,available_at=? WHERE id=?`, id, rec.CreatedAt, id, rec.CreatedAt, rec.ID); err != nil {
			return err
		}
		event := domain.InstructorEvent{ID: id, WorkID: w.ID, Kind: "worker_report_queued", Detail: fmt.Sprintf("report=%s dispatch=%s phase=%s sequence=%d producer=%s runtime=%s", rec.ID, id, w.Phase, w.PhaseSequence, rec.SessionID, runtimeID), At: rec.CreatedAt}
		body, err = instructorJSON(event)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, event.WorkID, body)
		return err
	})
	if err != nil {
		return rec, err
	}
	result := reportFromGen(created)
	result.Outputs = append([]domain.ReportOutput(nil), rec.Outputs...)
	result.DeliveryState, result.ClaimToken, result.DeliveryBatchID, result.ClaimedAt = domain.ReportClaimed, "instructor-report:"+rec.ID, "instructor-report:"+rec.ID, rec.CreatedAt
	result.AvailableAt = rec.CreatedAt
	return result, nil
}

// BindInstructorDispatchRuntime freezes the target's immutable creation and
// controller epoch. Restored delivery cannot silently move to another runtime.
func (s *Store) BindInstructorDispatchRuntime(ctx context.Context, id string, target domain.SessionRecord) error {
	runtimeID := target.Metadata.RuntimeLaunchID
	if domain.NormalizeSessionMode(target.Mode) == domain.SessionModeChat {
		runtimeID = "chat:" + target.Metadata.ControllerGeneration
	}
	if target.ID == "" || target.CreatedAt.IsZero() || runtimeID == "" || runtimeID == "chat:" {
		return errors.New("native target session and runtime incarnation required")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "bind instructor runtime", func(_ *gen.Queries, db gen.DBTX) error {
		var body string
		if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_dispatches WHERE id=?`, id).Scan(&body); err != nil {
			return err
		}
		var d domain.InstructorDispatch
		if err := json.Unmarshal([]byte(body), &d); err != nil {
			return err
		}
		if d.TargetSessionID != "" && d.TargetSessionID != target.ID || d.TargetCreatedAt != nil && !d.TargetCreatedAt.Equal(target.CreatedAt) {
			return errors.New("instructor target incarnation changed; reconcile old native delivery")
		}
		if d.TargetRuntimeID != "" && d.TargetRuntimeID != runtimeID {
			// A proved AO-owned FIFO entry has not crossed provider admission. A
			// replacement Chat controller may promote that same entry, preserving
			// its original key/generation; unknown in-flight/TUI effects stay fenced.
			var count int
			if d.QueuedAt != nil && domain.NormalizeSessionMode(target.Mode) == domain.SessionModeChat {
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM conversation_messages m JOIN conversation_turns t ON t.id=m.turn_id WHERE m.client_message_id=? AND m.role='user' AND t.handled_by_session_id=? AND t.state='queued' AND t.provider_turn_id='' AND 'chat:'||t.controller_generation=?`, d.ID, target.ID, d.TargetRuntimeID).Scan(&count); err != nil {
					return err
				}
			}
			if count == 0 {
				return errors.New("instructor target runtime changed after possible native submission")
			}
			runtimeID = d.TargetRuntimeID // Immutable enqueue origin, not a fabricated new reservation.
		}
		d.TargetSessionID, d.TargetCreatedAt, d.TargetRuntimeID = target.ID, &target.CreatedAt, runtimeID
		body, err := instructorJSON(d)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `UPDATE instructor_dispatches SET body=? WHERE id=?`, body, id)
		return err
	})
}

// RecordNativeSemanticStart is called only for a genuine provider event. It
// joins the exact durable client key, provider turn and original controller
// generation; unrelated turns and a replacement runtime cannot certify a wake-up.
func (s *Store) RecordNativeSemanticStart(ctx context.Context, conversationID, providerTurnID, observerRuntimeID, source string, at time.Time) error {
	if conversationID == "" || providerTurnID == "" || source == "" {
		return nil
	}
	_, unlock := s.conversationWriter(ctx)
	defer unlock()
	return s.inTx(ctx, "observe native orchestrator turn start", func(q *gen.Queries) error {
		matches, err := q.InstructorDispatchesForNativeTurn(ctx, gen.InstructorDispatchesForNativeTurnParams{ConversationID: conversationID, ProviderTurnID: providerTurnID})
		if err != nil {
			return err
		}
		for _, v := range matches {
			var d domain.InstructorDispatch
			if err := json.Unmarshal([]byte(v.Body), &d); err != nil {
				return err
			}
			if d.TargetRuntimeID != "chat:"+v.ControllerGeneration || d.TargetSessionID != v.HandledBySessionID || d.State == "cancelled" {
				continue
			}
			if d.NativeReceipt != nil && d.NativeReceipt.StartedAt != nil {
				continue
			}
			if d.NativeReceipt == nil {
				d.NativeReceipt = &domain.InstructorNativeReceipt{RuntimeID: d.TargetRuntimeID}
			}
			d.NativeReceipt.ProviderTurnID, d.NativeReceipt.StartedAt, d.NativeReceipt.Source = providerTurnID, &at, source
			d.NativeReceipt.ObserverRuntimeID = observerRuntimeID
			if d.NativeReceipt.AcceptedAt == nil {
				d.NativeReceipt.AcceptedAt = &at
			}
			body, err := instructorJSON(d)
			if err != nil {
				return err
			}
			if err := q.SetInstructorDispatchBody(ctx, gen.SetInstructorDispatchBodyParams{Body: body, ID: d.ID}); err != nil {
				return err
			}
			event := domain.InstructorEvent{ID: "native-start:" + d.ID, WorkID: d.WorkID, Kind: "native_turn_started", Detail: fmt.Sprintf("dispatch=%s report=%s phase=%s sequence=%d session=%s runtime=%s observerRuntime=%s providerTurn=%s source=%s", d.ID, d.ReportID, d.Phase, d.PhaseSequence, d.TargetSessionID, d.TargetRuntimeID, observerRuntimeID, providerTurnID, source), At: at}
			body, err = instructorJSON(event)
			if err != nil {
				return err
			}
			if err := q.InsertInstructorNativeStartEvent(ctx, gen.InsertInstructorNativeStartEventParams{ID: event.ID, WorkID: event.WorkID, Body: body}); err != nil {
				return err
			}
			if d.ReportID != "" {
				if err := q.AcknowledgeInstructorWorkerReport(ctx, gen.AcknowledgeInstructorWorkerReportParams{AcknowledgedAt: nullTime(at), ID: d.ReportID, DeliveryBatchID: d.ID}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

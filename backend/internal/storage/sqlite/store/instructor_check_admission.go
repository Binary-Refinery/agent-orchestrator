package store

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// CaptureInstructorCheckProducer observes the actual producer row, immutable
// identity and work association in one read transaction, including its workspace.
func (s *Store) CaptureInstructorCheckProducer(ctx context.Context, workID string, id domain.SessionID) (domain.SessionRecord, domain.InstructorReportAdmission, bool, error) {
	a := domain.InstructorReportAdmission{WorkID: workID, SessionID: id, Member: true}
	tx, err := s.readDB.BeginTx(ctx, nil)
	if err != nil {
		return domain.SessionRecord{}, a, false, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := gen.New(tx).GetSession(ctx, id)
	if err != nil {
		return domain.SessionRecord{}, a, false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT i.instance_id FROM instructor_session_instances i JOIN instructor_sessions b ON b.session_id=i.session_id AND b.instance_id=i.instance_id WHERE i.session_id=? AND b.work_id=?`, id, workID).Scan(&a.InstanceID); err != nil {
		return domain.SessionRecord{}, a, false, domain.ErrInstructorProducerChanged
	}
	if err := tx.Commit(); err != nil {
		return domain.SessionRecord{}, a, false, err
	}
	rec := rowToRecord(row)
	a.RuntimeID = rec.Metadata.RuntimeLaunchID
	if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
		a.RuntimeID = "chat:" + rec.Metadata.ControllerGeneration
	}
	a.WorkspacePath, a.WorkspaceRepoPath, a.Harness, a.Model, a.Effort = rec.Metadata.WorkspacePath, rec.Metadata.WorkspaceRepoPath, rec.Harness, rec.Metadata.Model, rec.Metadata.Effort
	return rec, a, true, nil
}

// ValidateInstructorCheckProducer rechecks the captured resource and incarnation
// under the daemon writer lock before native execution and after its effect.
// The caller continues to use the captured workspace, never a fresh SID lookup.
func (s *Store) ValidateInstructorCheckProducer(ctx context.Context, a domain.InstructorReportAdmission, expected domain.SessionRecord) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "fence native check producer", func(q *gen.Queries, db gen.DBTX) error {
		if err := validateInstructorReportActor(ctx, db, a); err != nil {
			return err
		}
		row, err := q.GetSession(ctx, a.SessionID)
		if err != nil {
			return err
		}
		actual := rowToRecord(row)
		if actual.Kind != domain.KindWorker || actual.ProjectID != expected.ProjectID || actual.Metadata.WorkspacePath != expected.Metadata.WorkspacePath || actual.Metadata.WorkspaceRepoPath != expected.Metadata.WorkspaceRepoPath || actual.Metadata.RuntimeLaunchID != expected.Metadata.RuntimeLaunchID || actual.Metadata.ControllerGeneration != expected.Metadata.ControllerGeneration || actual.Harness != expected.Harness || actual.Metadata.Model != expected.Metadata.Model || actual.Metadata.Effort != expected.Metadata.Effort {
			return domain.ErrInstructorProducerChanged
		}
		if actual.IsTerminated && !expected.IsTerminated {
			return errors.New("producer terminated during native check boundary")
		}
		return nil
	})
}

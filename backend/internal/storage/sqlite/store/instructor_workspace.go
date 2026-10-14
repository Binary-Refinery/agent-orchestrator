package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// CaptureInstructorWorkspace atomically checks the immutable session instance,
// current work binding and both project identities, then pins every workspace
// root from that same transaction. The caller holds the lifecycle input lease
// through this capture and the complete filesystem operation.
func (s *Store) CaptureInstructorWorkspace(ctx context.Context, workID string, id domain.SessionID, instance string, project domain.ProjectID) (domain.InstructorWorkspaceSnapshot, error) {
	var captured domain.InstructorWorkspaceSnapshot
	if workID == "" || id == "" || instance == "" || project == "" {
		return captured, domain.ErrInstructorProducerChanged
	}
	tx, err := s.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return captured, err
	}
	defer func() { _ = tx.Rollback() }()
	q := gen.New(tx)
	row, err := q.GetSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return captured, domain.ErrInstructorProducerChanged
	}
	if err != nil {
		return captured, err
	}
	var actualInstance, actualProject string
	err = tx.QueryRowContext(ctx, `SELECT i.instance_id,w.project_id FROM instructor_session_instances i JOIN instructor_sessions b ON b.session_id=i.session_id AND b.instance_id=i.instance_id JOIN instructor_works w ON w.id=b.work_id JOIN instructor_session_members m ON m.work_id=b.work_id AND m.session_id=i.session_id AND m.instance_id=i.instance_id AND json_extract(m.body,'$.provenance')='admitted' WHERE i.session_id=? AND b.work_id=?`, id, workID).Scan(&actualInstance, &actualProject)
	if errors.Is(err, sql.ErrNoRows) {
		return captured, domain.ErrInstructorProducerChanged
	}
	if err != nil {
		return captured, err
	}
	captured.Session = rowToRecord(row)
	if actualInstance != instance || actualProject != string(project) || captured.Session.ProjectID != project || captured.Session.IsTaskPreparation || captured.Session.IsTerminated || captured.Session.Activity.State == domain.ActivityExited || strings.TrimSpace(captured.Session.Metadata.WorkspacePath) == "" {
		return domain.InstructorWorkspaceSnapshot{}, domain.ErrInstructorProducerChanged
	}
	rows, err := q.ListSessionWorktrees(ctx, id)
	if err != nil {
		return domain.InstructorWorkspaceSnapshot{}, err
	}
	captured.Worktrees = make([]domain.SessionWorktreeRecord, 0, len(rows))
	for _, worktree := range rows {
		captured.Worktrees = append(captured.Worktrees, sessionWorktreeFromGen(worktree))
	}
	if err := tx.Commit(); err != nil {
		return domain.InstructorWorkspaceSnapshot{}, err
	}
	return captured, nil
}

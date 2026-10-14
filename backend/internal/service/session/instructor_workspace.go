package session

import (
	"context"
	"errors"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

// InstructorWorkspaceScope is explicit caller evidence, not a session display
// ID lookup. A missing or stale immutable instance never falls back to a legacy
// workspace route.
type InstructorWorkspaceScope struct {
	WorkID     string
	SessionID  domain.SessionID
	InstanceID string
	ProjectID  domain.ProjectID
}

type scopedWorkspaceKey struct{}

type instructorWorkspaceCapture interface {
	CaptureInstructorWorkspace(context.Context, string, domain.SessionID, string, domain.ProjectID) (domain.InstructorWorkspaceSnapshot, error)
}

// WithScopedWorkspace holds the session lifecycle lease through actual I/O,
// pins the row and child roots atomically, and makes those resources available
// only to synchronous workspace operations within the callback.
func (s *Service) WithScopedWorkspace(ctx context.Context, scope InstructorWorkspaceScope, operation func(context.Context) error) error {
	if strings.TrimSpace(scope.WorkID) == "" || scope.SessionID == "" || strings.TrimSpace(scope.InstanceID) == "" || scope.ProjectID == "" {
		return apierr.Invalid("INSTRUCTOR_WORKSPACE_SCOPE_REQUIRED", "workId, sessionId, projectId and instanceId are required", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	leaser, ok := s.manager.(interface {
		AcquireSessionInput(domain.SessionID) (func(), bool)
	})
	if !ok {
		return apierr.NotImplemented("INSTRUCTOR_WORKSPACE_UNAVAILABLE", "Atomic scoped workspace access is unavailable")
	}
	capturer, ok := s.store.(instructorWorkspaceCapture)
	if !ok {
		return apierr.NotImplemented("INSTRUCTOR_WORKSPACE_UNAVAILABLE", "Atomic scoped workspace capture is unavailable")
	}
	release, admitted := leaser.AcquireSessionInput(scope.SessionID)
	if !admitted {
		return apierr.Conflict("INSTRUCTOR_WORKSPACE_BUSY", "The session has an exclusive lifecycle operation", nil)
	}
	defer release()
	captured, err := capturer.CaptureInstructorWorkspace(ctx, scope.WorkID, scope.SessionID, scope.InstanceID, scope.ProjectID)
	if errors.Is(err, domain.ErrInstructorProducerChanged) {
		return apierr.Conflict("INSTRUCTOR_WORKSPACE_SCOPE_CHANGED", "The captured work/session/project incarnation is no longer current", nil)
	}
	if err != nil {
		return err
	}
	if captured.Session.ID != scope.SessionID || captured.Session.ProjectID != scope.ProjectID || strings.TrimSpace(captured.Session.Metadata.WorkspacePath) == "" {
		return apierr.Conflict("INSTRUCTOR_WORKSPACE_SCOPE_CHANGED", "The captured workspace does not match the requested scope", nil)
	}
	return operation(context.WithValue(ctx, scopedWorkspaceKey{}, captured))
}

func (s *Service) sessionWorkspaceWorktrees(ctx context.Context, id domain.SessionID) ([]domain.SessionWorktreeRecord, error) {
	if captured, ok := ctx.Value(scopedWorkspaceKey{}).(domain.InstructorWorkspaceSnapshot); ok {
		if captured.Session.ID != id {
			return nil, apierr.Conflict("INSTRUCTOR_WORKSPACE_SCOPE_CHANGED", "Workspace operation changed session scope", nil)
		}
		return captured.Worktrees, nil
	}
	return s.store.ListSessionWorktrees(ctx, id)
}

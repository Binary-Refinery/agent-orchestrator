package controllers

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

type scopedWorkspaceService interface {
	WithScopedWorkspace(context.Context, sessionsvc.InstructorWorkspaceScope, func(context.Context) error) error
}

func (c *SessionsController) registerScopedWorkspaces(r chi.Router) {
	const root = "/instructor/works/{workId}/sessions/{sessionId}/workspace"
	r.Get(root+"/tree", c.scopedWorkspace(c.listWorkspaceTree))
	r.Get(root+"/search", c.scopedWorkspace(c.searchWorkspaceFiles))
	r.Get(root+"/file", c.scopedWorkspace(c.getWorkspaceFile))
	r.Put(root+"/file", c.scopedWorkspace(c.updateWorkspaceFile))
	r.Post(root+"/diffs", c.scopedWorkspace(c.getWorkspaceDiffs))
}

// Distinct paths make old daemons fail closed. An optional parameter on legacy
// session-only paths would silently lose the caller's incarnation fence.
func (c *SessionsController) scopedWorkspace(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !c.InstructorEnabled {
			envelope.WriteError(w, r, apierr.Unavailable("INSTRUCTOR_DISABLED", "Instructor is disabled"))
			return
		}
		query := r.URL.Query()
		if len(query["projectId"]) != 1 || len(query["instanceId"]) != 1 || strings.TrimSpace(query.Get("projectId")) == "" || strings.TrimSpace(query.Get("instanceId")) == "" {
			envelope.WriteError(w, r, apierr.Invalid("INSTRUCTOR_WORKSPACE_SCOPE_REQUIRED", "One explicit projectId and instanceId are required", nil))
			return
		}
		svc, ok := c.Svc.(scopedWorkspaceService)
		if !ok {
			envelope.WriteError(w, r, apierr.NotImplemented("INSTRUCTOR_WORKSPACE_UNAVAILABLE", "Atomic scoped workspace access is unavailable"))
			return
		}
		err := svc.WithScopedWorkspace(r.Context(), sessionsvc.InstructorWorkspaceScope{
			WorkID: chi.URLParam(r, "workId"), SessionID: sessionID(r),
			ProjectID: domain.ProjectID(query.Get("projectId")), InstanceID: query.Get("instanceId"),
		}, func(ctx context.Context) error {
			next(w, r.WithContext(ctx))
			return nil
		})
		if err != nil {
			envelope.WriteError(w, r, err)
		}
	}
}

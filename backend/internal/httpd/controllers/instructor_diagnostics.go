package controllers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
)

type instructorDiagnosticService interface {
	InstructorDiagnostics(context.Context, domain.InstructorDiagnosticScope, bool, int64, int64) (domain.InstructorMemberDiagnostics, error)
}

func (c *SessionsController) registerInstructorDiagnostics(r chi.Router) {
	const root = "/instructor/works/{workId}/members/{memberId}"
	r.Get(root+"/diagnostics", c.instructorDiagnostics(false))
	r.Get(root+"/timeline", c.instructorDiagnostics(true))
}

func (c *SessionsController) instructorDiagnostics(timeline bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !c.InstructorEnabled {
			envelope.WriteError(w, r, apierr.Unavailable("INSTRUCTOR_DISABLED", "Instructor is disabled"))
			return
		}
		query := r.URL.Query()
		for _, key := range []string{"projectId", "sessionId", "instanceId", "runtimeGeneration"} {
			if len(query[key]) != 1 || len(query.Get(key)) > 512 || (key != "runtimeGeneration" && strings.TrimSpace(query.Get(key)) == "") {
				envelope.WriteError(w, r, apierr.Invalid("INSTRUCTOR_DIAGNOSTIC_SCOPE_REQUIRED", "One explicit projectId, sessionId, instanceId and runtimeGeneration are required; generation may be empty", nil))
				return
			}
		}
		before, limit := int64(0), int64(50)
		if len(query["beforeSequence"]) > 0 {
			parsed, err := strconv.ParseInt(query.Get("beforeSequence"), 10, 64)
			if len(query["beforeSequence"]) != 1 || err != nil || parsed < 0 {
				envelope.WriteError(w, r, apierr.Invalid("INSTRUCTOR_DIAGNOSTIC_PAGE_INVALID", "beforeSequence must be one nonnegative integer", nil))
				return
			}
			before = parsed
		}
		if len(query["limit"]) > 0 {
			parsed, err := strconv.ParseInt(query.Get("limit"), 10, 64)
			if len(query["limit"]) != 1 || err != nil || parsed < 1 || parsed > 100 {
				envelope.WriteError(w, r, apierr.Invalid("INSTRUCTOR_DIAGNOSTIC_PAGE_INVALID", "limit must be one integer between 1 and 100", nil))
				return
			}
			limit = parsed
		}
		svc, ok := c.Svc.(instructorDiagnosticService)
		if !ok {
			envelope.WriteError(w, r, apierr.NotImplemented("INSTRUCTOR_DIAGNOSTICS_UNAVAILABLE", "Atomic persisted member diagnostics are unavailable"))
			return
		}
		result, err := svc.InstructorDiagnostics(r.Context(), domain.InstructorDiagnosticScope{
			WorkID: chi.URLParam(r, "workId"), MemberID: chi.URLParam(r, "memberId"), ProjectID: domain.ProjectID(query.Get("projectId")),
			SessionID: domain.SessionID(query.Get("sessionId")), InstanceID: query.Get("instanceId"), RuntimeGeneration: query.Get("runtimeGeneration"),
		}, timeline, before, limit)
		if err != nil {
			envelope.WriteError(w, r, err)
			return
		}
		envelope.WriteJSON(w, http.StatusOK, result)
	}
}

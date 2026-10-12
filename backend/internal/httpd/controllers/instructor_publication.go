package controllers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func (c *InstructorController) publish(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	publisher, ok := c.Svc.(interface {
		ExecutePublication(context.Context, instructorsvc.PublicationInput) (domain.InstructorWork, error)
	})
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	var req InstructorPublicationRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	work, err := publisher.ExecutePublication(r.Context(), instructorsvc.PublicationInput{
		WorkID: chi.URLParam(r, "workId"), GrantID: req.GrantID, SnapshotID: req.SnapshotID,
		WorkRevision: req.WorkRevision, SHA: req.SHA, Branch: req.Branch, Operation: req.Operation,
	})
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, work)
}

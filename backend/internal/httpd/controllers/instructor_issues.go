package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type instructorIssueIntakeService interface {
	IssueCandidates(context.Context, domain.ProjectID) (domain.InstructorIssueCandidates, error)
	IssuePreview(context.Context, domain.ProjectID, int) (domain.InstructorIssueCandidates, error)
	IntakeIssue(context.Context, instructorsvc.IssueIntakeInput) (domain.InstructorWork, error)
}

func (c *InstructorController) issues(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	service, ok := c.Svc.(instructorIssueIntakeService)
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	id := domain.ProjectID(chi.URLParam(r, "projectId"))
	var result domain.InstructorIssueCandidates
	var err error
	if r.URL.Query().Has("number") {
		values := r.URL.Query()["number"]
		if len(values) != 1 {
			writeInstructorError(w, r, instructorsvc.ErrValidation)
			return
		}
		number, parseErr := strconv.Atoi(values[0])
		if parseErr != nil || number <= 0 {
			writeInstructorError(w, r, instructorsvc.ErrValidation)
			return
		}
		result, err = service.IssuePreview(r.Context(), id, number)
	} else {
		result, err = service.IssueCandidates(r.Context(), id)
	}
	if err != nil {
		writeInstructorIssueError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) intakeIssue(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	service, ok := c.Svc.(instructorIssueIntakeService)
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	var request InstructorIssueIntakeRequest
	if !decodeInstructorRequest(w, r, &request) {
		return
	}
	result, err := service.IntakeIssue(r.Context(), instructorsvc.IssueIntakeInput{ProjectID: domain.ProjectID(chi.URLParam(r, "projectId")), Repository: request.Repository, Number: request.Number, Name: request.Name})
	if err != nil {
		writeInstructorIssueError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, result)
}

func writeInstructorIssueError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, instructorsvc.ErrIssuesUnavailable) {
		envelope.WriteAPIError(w, r, http.StatusServiceUnavailable, "unavailable", "INSTRUCTOR_ISSUES_UNAVAILABLE", instructorsvc.ErrIssuesUnavailable.Error(), nil)
		return
	}
	writeInstructorError(w, r, err)
}

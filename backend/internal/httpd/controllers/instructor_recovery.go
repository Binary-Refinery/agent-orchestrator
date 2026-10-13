package controllers

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

func (c *InstructorController) effectiveProjectPolicy(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	svc, ok := c.Svc.(interface {
		ResolveProjectPolicy(context.Context, domain.ProjectID) (domain.InstructorPolicy, string, error)
	})
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	policy, source, err := svc.ResolveProjectPolicy(r.Context(), domain.ProjectID(chi.URLParam(r, "projectId")))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, InstructorEffectivePolicyResponse{Policy: policy, Source: source})
}

type instructorPolicyRebinder interface {
	PreviewPolicyRebind(context.Context, instructorsvc.PolicyRebindInput) (instructorsvc.PolicyRebindPreview, error)
	ApplyPolicyRebind(context.Context, instructorsvc.PolicyRebindInput) (domain.InstructorWork, error)
}

func policyRebindInput(in InstructorPolicyRebindRequest) instructorsvc.PolicyRebindInput {
	return instructorsvc.PolicyRebindInput{SnapshotID: in.SnapshotID, WorkID: in.WorkID, Reason: in.Reason, TargetPolicyVersion: in.TargetPolicyVersion, Approval: in.Approval}
}

func (c *InstructorController) previewPolicyRebind(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	svc, ok := c.Svc.(instructorPolicyRebinder)
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	var in InstructorPolicyRebindRequest
	if !decodeInstructorRequest(w, r, &in) {
		return
	}
	preview, err := svc.PreviewPolicyRebind(r.Context(), policyRebindInput(in))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, preview)
}

func (c *InstructorController) applyPolicyRebind(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	svc, ok := c.Svc.(instructorPolicyRebinder)
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	var in InstructorPolicyRebindRequest
	if !decodeInstructorRequest(w, r, &in) {
		return
	}
	result, err := svc.ApplyPolicyRebind(r.Context(), policyRebindInput(in))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

type instructorControllerRecovery interface {
	Recovery(context.Context, string) (domain.InstructorRecoveryStatus, error)
	Recover(context.Context, domain.InstructorRecoveryInput) (domain.InstructorRecoveryStatus, error)
}

func (c *InstructorController) recovery(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	svc, ok := c.Svc.(instructorControllerRecovery)
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	result, err := svc.Recovery(r.Context(), chi.URLParam(r, "workId"))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) recoverController(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	svc, ok := c.Svc.(instructorControllerRecovery)
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	var in InstructorRecoveryRequest
	if !decodeInstructorRequest(w, r, &in) {
		return
	}
	if in.WorkID != chi.URLParam(r, "workId") {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INSTRUCTOR_INVALID", "Recovery work must match the route", nil)
		return
	}
	result, err := svc.Recover(r.Context(), domain.InstructorRecoveryInput(in))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func writeInstructorRecoveryError(w http.ResponseWriter, r *http.Request, err error) bool {
	var code string
	switch {
	case errors.Is(err, instructorsvc.ErrRecoveryInProgress), errors.Is(err, sessionmanager.ErrResumeInProgress), errors.Is(err, sessionmanager.ErrSwitchInProgress), errors.Is(err, sessionmanager.ErrInterfaceTransitionInProgress):
		code = "SESSION_RECOVERY_IN_PROGRESS"
	case errors.Is(err, instructorsvc.ErrRecoveryRequired):
		code = "INSTRUCTOR_RECOVERY_REQUIRED"
	case errors.Is(err, instructorsvc.ErrDeliveryUncertain):
		code = "INSTRUCTOR_DELIVERY_UNCERTAIN"
	default:
		// Recovery goes directly through the manager. Reuse the same precedence,
		// details and status codes as the ordinary session restore/resume routes.
		mapped := sessionsvc.MapRecoveryError(err)
		var typed *apierr.Error
		if !errors.As(mapped, &typed) {
			return false
		}
		envelope.WriteError(w, r, mapped)
		return true
	}
	envelope.WriteAPIError(w, r, http.StatusConflict, "conflict", code, err.Error(), nil)
	return true
}

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

// InstructorService is the daemon-owned portfolio control plane. Controllers
// never spawn sessions or infer gates from display status.
type InstructorService interface {
	Register(context.Context, instructorsvc.RegisterInput) (domain.InstructorWork, error)
	AttachSession(context.Context, string, domain.SessionID) error
	Report(context.Context, instructorsvc.ReportInput) (domain.InstructorWork, error)
	Snapshot(context.Context, string) (domain.InstructorSnapshot, error)
	Details(context.Context, string) (domain.InstructorDetails, error)
	Commands() []domain.InstructorCommandSpec
	Command(context.Context, instructorsvc.CommandInput) (domain.InstructorCommandResult, error)
	RunChecks(context.Context, string, domain.SessionID, []string) (domain.InstructorWork, error)
	VerifyEvidence(context.Context, string, domain.InstructorEvidence) (domain.InstructorWork, error)
	GetPolicy(context.Context) (domain.InstructorPolicy, error)
	SetPolicy(context.Context, domain.InstructorPolicy) (domain.InstructorPolicy, error)
	ReconcileDelivery(context.Context, instructorsvc.ReconcileInput) (domain.InstructorWork, error)
	PreviewException(context.Context, instructorsvc.ExceptionInput) (instructorsvc.ExceptionPreview, error)
	ApplyException(context.Context, instructorsvc.ExceptionInput) (domain.InstructorWork, error)
	Plan(context.Context, domain.InstructorPlanInput) (domain.InstructorPlanJob, error)
	GetPlan(context.Context, string) (domain.InstructorPlanJob, error)
	ListPlans(context.Context) ([]domain.InstructorPlanJob, error)
}

// InstructorInventoryReader lists real runtime facts without adopting legacy
// sessions into governed work or inspecting project secrets.
type InstructorInventoryReader interface {
	Inventory(context.Context) (InstructorInventoryResponse, error)
}

// InstructorController exposes the global daemon-owned work control plane.
type InstructorController struct {
	Svc             InstructorService
	InventorySource InstructorInventoryReader
	Enabled         bool
}

// Register mounts Instructor operations on the existing loopback API router.
func (c *InstructorController) Register(r chi.Router) {
	r.Get("/instructor/capabilities", c.capabilities)
	r.Get("/instructor/status", c.status)
	r.Get("/instructor/inventory", c.inventory)
	r.Post("/instructor/inventory/reconcile", c.reconcileInventory)
	r.Get("/instructor/commands", c.commands)
	r.Get("/instructor/policy", c.getPolicy)
	r.Put("/instructor/policy", c.setPolicy)
	r.Post("/instructor/commands", c.command)
	r.Post("/instructor/works", c.registerWork)
	r.Get("/instructor/works/{workId}", c.details)
	r.Get("/instructor/works/{workId}/members", c.members)
	r.Post("/instructor/works/{workId}/sessions", c.attachSession)
	r.Post("/instructor/works/{workId}/checks", c.runChecks)
	r.Post("/instructor/works/{workId}/publication", c.publish)
	r.Post("/instructor/works/{workId}/evidence/verify", c.verifyEvidence)
	r.Post("/instructor/reports", c.report)
	r.Post("/instructor/reconcile", c.reconcile)
	r.Post("/instructor/exceptions/preview", c.previewException)
	r.Post("/instructor/exceptions", c.applyException)
	r.Post("/instructor/plans", c.plan)
	r.Get("/instructor/plans", c.plans)
	r.Get("/instructor/plans/{planId}", c.getPlan)
}

func (c *InstructorController) getPolicy(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	policy, err := c.Svc.GetPolicy(r.Context())
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, policy)
}

func (c *InstructorController) setPolicy(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req domain.InstructorPolicy
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	policy, err := c.Svc.SetPolicy(r.Context(), req)
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, policy)
}

func writeInstructorError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, instructorsvc.ErrPublicationUnavailable):
		envelope.WriteAPIError(w, r, http.StatusServiceUnavailable, "unavailable", "INSTRUCTOR_PUBLICATION_UNAVAILABLE", err.Error(), nil)
	case errors.Is(err, instructorsvc.ErrPublicationConsumed):
		envelope.WriteAPIError(w, r, http.StatusConflict, "conflict", "INSTRUCTOR_PUBLICATION_CONSUMED", err.Error(), nil)
	case errors.Is(err, instructorsvc.ErrEvidencePending):
		envelope.WriteAPIError(w, r, http.StatusConflict, "conflict", "INSTRUCTOR_EVIDENCE_PENDING", "Current persisted evidence is being inspected; retry after the inspection completes.", nil)
	case errors.Is(err, instructorsvc.ErrConflict):
		envelope.WriteAPIError(w, r, http.StatusConflict, "conflict", "INSTRUCTOR_CONFLICT", err.Error(), nil)
	case errors.Is(err, instructorsvc.ErrNotFound):
		envelope.WriteAPIError(w, r, http.StatusNotFound, "not_found", "INSTRUCTOR_NOT_FOUND", err.Error(), nil)
	case errors.Is(err, instructorsvc.ErrValidation):
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INSTRUCTOR_INVALID", err.Error(), nil)
	default:
		envelope.WriteError(w, r, err)
	}
}

func (c *InstructorController) capabilities(w http.ResponseWriter, r *http.Request) {
	reason := ""
	if !c.Enabled {
		reason = "AO_INSTRUCTOR is disabled"
	}
	if c.Svc == nil {
		reason = "Instructor service is unavailable"
	}
	commands := []domain.InstructorCommandSpec{}
	if c.Svc != nil {
		commands = c.Svc.Commands()
	}
	publication := domain.InstructorPublicationCapabilities{Reason: "Protected publication service is unavailable."}
	if publisher, ok := c.Svc.(interface {
		PublicationCapabilities(context.Context) domain.InstructorPublicationCapabilities
	}); ok {
		publication = publisher.PublicationCapabilities(r.Context())
	}
	envelope.WriteJSON(w, http.StatusOK, InstructorCapabilitiesResponse{Schema: "ao.instructor.v1", Enabled: c.Enabled && c.Svc != nil, Reason: reason, Commands: commands, Publication: publication})
}

func (c *InstructorController) available(w http.ResponseWriter, r *http.Request) bool {
	if !c.Enabled {
		envelope.WriteAPIError(w, r, http.StatusServiceUnavailable, "unavailable", "INSTRUCTOR_DISABLED", "Instructor is disabled; enable AO_INSTRUCTOR in the daemon launch configuration", nil)
		return false
	}
	if c.Svc == nil {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return false
	}
	return true
}

func (c *InstructorController) status(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	result, err := c.Svc.Snapshot(r.Context(), r.URL.Query().Get("baseId"))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) inventory(w http.ResponseWriter, r *http.Request) {
	if c.InventorySource == nil {
		apispec.NotImplemented(w, r, r.Method, "/api/v1/instructor/inventory")
		return
	}
	result, err := c.InventorySource.Inventory(r.Context())
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) commands(w http.ResponseWriter, _ *http.Request) {
	commands := []domain.InstructorCommandSpec{}
	if c.Svc != nil {
		commands = c.Svc.Commands()
	}
	envelope.WriteJSON(w, http.StatusOK, InstructorCommandsResponse{Commands: commands})
}

func (c *InstructorController) command(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req InstructorCommandRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	input := instructorsvc.CommandInput{Command: req.Command, SnapshotID: req.SnapshotID, WorkIDs: req.WorkIDs, Approval: req.Approval, IdempotencyKey: req.IdempotencyKey, PublicationOperation: req.PublicationOperation, RequesterSessionID: domain.SessionID(r.Header.Get("X-AO-Session-ID"))}
	input.Approvals = req.Approvals
	if req.New != nil {
		value := instructorRegisterInput(*req.New)
		input.New = &value
	}
	result, err := c.Svc.Command(r.Context(), input)
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func instructorRegisterInput(req InstructorRegisterRequest) instructorsvc.RegisterInput {
	return instructorsvc.RegisterInput{ProjectID: domain.ProjectID(req.ProjectID), Name: req.Name, Goal: req.Goal, Source: req.Source, SourceRef: req.SourceRef, IssueID: domain.IssueID(req.IssueID), Branch: req.Branch, Policy: req.Policy}
}

func (c *InstructorController) registerWork(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req InstructorRegisterRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	result, err := c.Svc.Register(r.Context(), instructorRegisterInput(req))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, result)
}

func (c *InstructorController) details(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	result, err := c.Svc.Details(r.Context(), chi.URLParam(r, "workId"))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) attachSession(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req InstructorAttachSessionRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	if err := c.Svc.AttachSession(r.Context(), chi.URLParam(r, "workId"), domain.SessionID(req.SessionID)); err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, InstructorOKResponse{OK: true})
}

func (c *InstructorController) runChecks(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req InstructorChecksRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	result, err := c.Svc.RunChecks(r.Context(), chi.URLParam(r, "workId"), domain.SessionID(req.SessionID), req.Profiles)
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) report(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req InstructorReportRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	result, err := c.Svc.Report(r.Context(), instructorsvc.ReportInput{WorkID: req.WorkID, SessionID: domain.SessionID(req.SessionID), Kind: req.Kind, Summary: req.Summary, Next: req.Next, SHA: req.SHA, Branch: req.Branch, Phase: req.Phase, WaitCondition: req.WaitCondition, WaitUntil: req.WaitUntil, Gate: req.Gate, Evidence: req.Evidence, OpenCriticalFindings: req.OpenCriticalFindings, DispatchID: req.DispatchID})
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) verifyEvidence(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var evidence domain.InstructorEvidence
	if !decodeInstructorRequest(w, r, &evidence) {
		return
	}
	result, err := c.Svc.VerifyEvidence(r.Context(), chi.URLParam(r, "workId"), evidence)
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) reconcile(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req InstructorReconcileRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	if req.Accepted == nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INSTRUCTOR_INVALID", "accepted must explicitly state true or false", nil)
		return
	}
	result, err := c.Svc.ReconcileDelivery(r.Context(), instructorsvc.ReconcileInput{SnapshotID: req.SnapshotID, WorkID: req.WorkID, DispatchID: req.DispatchID, Accepted: *req.Accepted, Note: req.Note})
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func decodeInstructorRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid Instructor JSON body", nil)
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Instructor body must contain exactly one JSON value", nil)
		return false
	}
	return true
}

func instructorExceptionInput(req InstructorExceptionRequest) instructorsvc.ExceptionInput {
	return instructorsvc.ExceptionInput{SnapshotID: req.SnapshotID, WorkID: req.WorkID, Role: req.Role, Selection: req.Selection, MaxFixRounds: req.MaxFixRounds, ExecutionBudgets: req.ExecutionBudgets, Reason: req.Reason, ExpiresAt: req.ExpiresAt, Approval: req.Approval}
}

func (c *InstructorController) reconcileInventory(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	reconciler, ok := c.InventorySource.(interface {
		ReconcileInventory(context.Context, string, time.Time) error
	})
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	var req InstructorInventoryReconcileRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	if err := reconciler.ReconcileInventory(r.Context(), req.ResourceID, req.LastSeenAt); err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, InstructorOKResponse{OK: true})
}

func (c *InstructorController) previewException(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req InstructorExceptionRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	result, err := c.Svc.PreviewException(r.Context(), instructorExceptionInput(req))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) applyException(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req InstructorExceptionRequest
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	result, err := c.Svc.ApplyException(r.Context(), instructorExceptionInput(req))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *InstructorController) plan(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	var req domain.InstructorPlanInput
	if !decodeInstructorRequest(w, r, &req) {
		return
	}
	job, err := c.Svc.Plan(r.Context(), req)
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusAccepted, job)
}

func (c *InstructorController) plans(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	jobs, err := c.Svc.ListPlans(r.Context())
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	if jobs == nil {
		jobs = []domain.InstructorPlanJob{}
	}
	envelope.WriteJSON(w, http.StatusOK, InstructorPlansResponse{Jobs: jobs})
}

func (c *InstructorController) getPlan(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	job, err := c.Svc.GetPlan(r.Context(), chi.URLParam(r, "planId"))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, job)
}

func (c *InstructorController) members(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r) {
		return
	}
	reader, ok := c.Svc.(interface {
		Members(context.Context, string) (domain.InstructorMembers, error)
	})
	if !ok {
		apispec.NotImplemented(w, r, r.Method, r.URL.Path)
		return
	}
	result, err := reader.Members(r.Context(), chi.URLParam(r, "workId"))
	if err != nil {
		writeInstructorError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

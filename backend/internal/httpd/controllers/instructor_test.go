package controllers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type fakeInstructorService struct {
	controllers.InstructorService
	base      string
	command   instructorsvc.CommandInput
	reconcile instructorsvc.ReconcileInput
	exception instructorsvc.ExceptionInput
	plan      domain.InstructorPlanInput
	policy    domain.InstructorPolicy
	err       error
}

func (s *fakeInstructorService) Commands() []domain.InstructorCommandSpec {
	return []domain.InstructorCommandSpec{{Name: "Weiter", Action: "weiter", Mutates: true, SnapshotRequired: true}}
}
func (s *fakeInstructorService) Snapshot(_ context.Context, base string) (domain.InstructorSnapshot, error) {
	s.base = base
	return domain.InstructorSnapshot{ID: "snapshot", Works: []domain.InstructorWork{}, Commands: s.Commands()}, s.err
}
func (s *fakeInstructorService) Command(_ context.Context, in instructorsvc.CommandInput) (domain.InstructorCommandResult, error) {
	s.command = in
	return domain.InstructorCommandResult{BatchID: "batch", Dispatches: []domain.InstructorDispatch{}, Skipped: []string{}}, s.err
}
func (s *fakeInstructorService) GetPolicy(context.Context) (domain.InstructorPolicy, error) {
	return s.policy, s.err
}
func (s *fakeInstructorService) SetPolicy(_ context.Context, p domain.InstructorPolicy) (domain.InstructorPolicy, error) {
	s.policy = p
	return p, s.err
}
func (s *fakeInstructorService) ReconcileDelivery(_ context.Context, in instructorsvc.ReconcileInput) (domain.InstructorWork, error) {
	s.reconcile = in
	return domain.InstructorWork{ID: in.WorkID}, s.err
}
func (s *fakeInstructorService) PreviewException(_ context.Context, in instructorsvc.ExceptionInput) (instructorsvc.ExceptionPreview, error) {
	s.exception = in
	return instructorsvc.ExceptionPreview{WorkID: in.WorkID, ProposalHash: "hash", ExactApproval: "EXCEPTION-GO work hash"}, s.err
}
func (s *fakeInstructorService) ApplyException(_ context.Context, in instructorsvc.ExceptionInput) (domain.InstructorWork, error) {
	s.exception = in
	return domain.InstructorWork{ID: in.WorkID}, s.err
}
func (s *fakeInstructorService) Plan(_ context.Context, in domain.InstructorPlanInput) (domain.InstructorPlanJob, error) {
	s.plan = in
	return domain.InstructorPlanJob{ID: "plan", Input: in, State: "queued"}, s.err
}
func (s *fakeInstructorService) GetPlan(_ context.Context, id string) (domain.InstructorPlanJob, error) {
	return domain.InstructorPlanJob{ID: id, State: "completed"}, s.err
}
func (s *fakeInstructorService) ListPlans(context.Context) ([]domain.InstructorPlanJob, error) {
	return nil, s.err
}

func instructorTestServer(t *testing.T, svc *fakeInstructorService, enabled bool) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Instructor: svc, InstructorEnabled: enabled}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInstructorAPIExposesDisabledCapabilityAndRejectsMutation(t *testing.T) {
	svc := &fakeInstructorService{}
	srv := instructorTestServer(t, svc, false)
	body, status, _ := doRequest(t, srv, "GET", "/api/v1/instructor/capabilities", "")
	if status != http.StatusOK || !strings.Contains(string(body), `"enabled":false`) || !strings.Contains(string(body), `"name":"Weiter"`) {
		t.Fatalf("status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, "POST", "/api/v1/instructor/commands", `{"command":"Weiter","snapshotId":"snapshot","idempotencyKey":"key"}`)
	if status != http.StatusServiceUnavailable || !strings.Contains(string(body), "INSTRUCTOR_DISABLED") || svc.command.Command != "" {
		t.Fatalf("status=%d body=%s input=%+v", status, body, svc.command)
	}
}

func TestInstructorAPIStatusAndSnapshotConflict(t *testing.T) {
	svc := &fakeInstructorService{}
	srv := instructorTestServer(t, svc, true)
	body, status, _ := doRequest(t, srv, "GET", "/api/v1/instructor/status?baseId=previous", "")
	if status != http.StatusOK || svc.base != "previous" || !strings.Contains(string(body), `"id":"snapshot"`) {
		t.Fatalf("status=%d body=%s base=%s", status, body, svc.base)
	}
	svc.err = instructorsvc.ErrConflict
	body, status, _ = doRequest(t, srv, "POST", "/api/v1/instructor/commands", `{"command":"Weiter","snapshotId":"snapshot","workIds":["work"],"idempotencyKey":"key"}`)
	if status != http.StatusConflict || !strings.Contains(string(body), "INSTRUCTOR_CONFLICT") || svc.command.SnapshotID != "snapshot" || len(svc.command.WorkIDs) != 1 {
		t.Fatalf("status=%d body=%s input=%+v", status, body, svc.command)
	}
}

func TestInstructorAPIPendingEvidenceHasDistinctRetryableConflict(t *testing.T) {
	svc := &fakeInstructorService{err: instructorsvc.ErrEvidencePending}
	server := instructorTestServer(t, svc, true)
	body, status, _ := doRequest(t, server, http.MethodPost, "/api/v1/instructor/commands", `{"command":"Weiter","snapshotId":"snapshot","workIds":["work"]}`)
	if status != http.StatusConflict || !strings.Contains(string(body), "INSTRUCTOR_EVIDENCE_PENDING") {
		t.Fatalf("pending evidence should request inspection completion: status=%d body=%s", status, body)
	}
}

func TestInstructorAPIPolicyUsesStrictDaemonContract(t *testing.T) {
	svc := &fakeInstructorService{policy: domain.InstructorPolicy{Schema: 1, Version: "pinned"}}
	srv := instructorTestServer(t, svc, true)
	body, status, _ := doRequest(t, srv, "GET", "/api/v1/instructor/policy", "")
	if status != http.StatusOK || !strings.Contains(string(body), `"version":"pinned"`) {
		t.Fatalf("status=%d body=%s", status, body)
	}
	_, status, _ = doRequest(t, srv, "PUT", "/api/v1/instructor/policy", `{"schema":1,"unknown":true}`)
	if status != http.StatusBadRequest {
		t.Fatalf("unknown policy fields status=%d", status)
	}
	_, status, _ = doRequest(t, srv, "POST", "/api/v1/instructor/commands", `{"command":"Weiter"}{"command":"Go"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("trailing JSON status=%d", status)
	}
}

func TestInstructorAPIReconciliationRequiresExplicitObservation(t *testing.T) {
	svc := &fakeInstructorService{}
	srv := instructorTestServer(t, svc, true)
	_, status, _ := doRequest(t, srv, "POST", "/api/v1/instructor/reconcile", `{"snapshotId":"snapshot","workId":"work","dispatchId":"dispatch","note":"checked target"}`)
	if status != http.StatusBadRequest || svc.reconcile.WorkID != "" {
		t.Fatalf("implicit negative observation status=%d input=%+v", status, svc.reconcile)
	}
	body, status, _ := doRequest(t, srv, "POST", "/api/v1/instructor/reconcile", `{"snapshotId":"snapshot","workId":"work","dispatchId":"dispatch","accepted":false,"note":"checked target"}`)
	if status != http.StatusOK || !strings.Contains(string(body), `"id":"work"`) || svc.reconcile.Accepted || svc.reconcile.SnapshotID != "snapshot" || svc.reconcile.DispatchID != "dispatch" {
		t.Fatalf("explicit observation status=%d body=%s input=%+v", status, body, svc.reconcile)
	}
}

func TestInstructorAPIExceptionPreviewAndApprovalPreserveExactScope(t *testing.T) {
	svc := &fakeInstructorService{}
	srv := instructorTestServer(t, svc, true)
	body, status, _ := doRequest(t, srv, "POST", "/api/v1/instructor/exceptions/preview", `{"snapshotId":"snapshot","workId":"work","maxFixRounds":3,"reason":"bounded additional fix","expiresAt":"2026-10-11T15:00:00Z"}`)
	if status != http.StatusOK || !strings.Contains(string(body), `"exactApproval":"EXCEPTION-GO work hash"`) || svc.exception.MaxFixRounds == nil || *svc.exception.MaxFixRounds != 3 || svc.exception.Approval != "" || svc.exception.SnapshotID != "snapshot" {
		t.Fatalf("preview status=%d body=%s input=%+v", status, body, svc.exception)
	}
	svc.err = instructorsvc.ErrConflict
	body, status, _ = doRequest(t, srv, "POST", "/api/v1/instructor/exceptions", `{"snapshotId":"snapshot","workId":"work","maxFixRounds":3,"reason":"bounded additional fix","expiresAt":"2026-10-11T15:00:00Z","approval":"EXCEPTION-GO work hash"}`)
	if status != http.StatusConflict || !strings.Contains(string(body), "INSTRUCTOR_CONFLICT") || svc.exception.Approval != "EXCEPTION-GO work hash" {
		t.Fatalf("approval status=%d body=%s input=%+v", status, body, svc.exception)
	}
}

func TestInstructorAPIPlanningIsAsynchronousAndDoesNotAdmitCommands(t *testing.T) {
	svc := &fakeInstructorService{}
	srv := instructorTestServer(t, svc, true)
	body, status, _ := doRequest(t, srv, "POST", "/api/v1/instructor/plans", `{"snapshotId":"snapshot","idempotencyKey":"intent","request":"Clarify priorities"}`)
	if status != http.StatusAccepted || svc.plan.SnapshotID != "snapshot" || svc.plan.IdempotencyKey != "intent" || svc.command.Command != "" || !strings.Contains(string(body), `"state":"queued"`) {
		t.Fatalf("status=%d body=%s plan=%+v command=%+v", status, body, svc.plan, svc.command)
	}
	body, status, _ = doRequest(t, srv, "GET", "/api/v1/instructor/plans", "")
	if status != http.StatusOK || string(body) != "{\"jobs\":[]}\n" {
		t.Fatalf("list status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, "GET", "/api/v1/instructor/plans/plan", "")
	if status != http.StatusOK || !strings.Contains(string(body), `"id":"plan"`) {
		t.Fatalf("get status=%d body=%s", status, body)
	}
}

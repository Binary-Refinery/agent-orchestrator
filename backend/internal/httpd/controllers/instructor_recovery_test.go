package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

type fakeInstructorRecoveryService struct {
	fakeInstructorService
	rebind                                 instructorsvc.PolicyRebindInput
	recovery                               domain.InstructorRecoveryInput
	observedWork                           string
	previewCalls, applyCalls, recoverCalls int
}

func (s *fakeInstructorRecoveryService) PreviewPolicyRebind(_ context.Context, in instructorsvc.PolicyRebindInput) (instructorsvc.PolicyRebindPreview, error) {
	s.rebind = in
	s.previewCalls++
	return instructorsvc.PolicyRebindPreview{WorkID: in.WorkID, SnapshotID: in.SnapshotID, WorkRevision: 42, TargetPolicyVersion: "target-policy", ProposalHash: "proposal", ExactApproval: "POLICY-GO work-1 proposal"}, s.err
}
func (s *fakeInstructorRecoveryService) ApplyPolicyRebind(_ context.Context, in instructorsvc.PolicyRebindInput) (domain.InstructorWork, error) {
	s.rebind = in
	s.applyCalls++
	return domain.InstructorWork{ID: in.WorkID}, s.err
}
func (s *fakeInstructorRecoveryService) Recovery(_ context.Context, id string) (domain.InstructorRecoveryStatus, error) {
	s.observedWork = id
	return domain.InstructorRecoveryStatus{WorkID: id, WorkRevision: 42, PolicyVersion: "pinned-policy", SessionID: "project-1", Action: "resume", ReasonCode: "CONTROLLER_UNAVAILABLE", Reason: "Resume the saved conversation", ProviderConversationID: "thread-1", ControllerGeneration: "generation-1"}, s.err
}
func (s *fakeInstructorRecoveryService) Recover(_ context.Context, in domain.InstructorRecoveryInput) (domain.InstructorRecoveryStatus, error) {
	s.recovery = in
	s.recoverCalls++
	return domain.InstructorRecoveryStatus{WorkID: in.WorkID, WorkRevision: in.WorkRevision, SessionID: in.SessionID, Ready: true, Action: "none"}, s.err
}
func instructorRecoveryTestServer(t *testing.T, svc *fakeInstructorRecoveryService) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Instructor: svc, InstructorEnabled: true}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

const capturedRecoveryJSON = `{"workId":"work-1","workRevision":42,"policyVersion":"pinned-policy","sessionId":"project-1","sessionCreatedAt":"2026-10-13T09:00:00Z","providerConversationId":"thread-1","controllerGeneration":"generation-1"}`

func TestInstructorAPIRecoveryPreservesObservedOwnerAndWorkFences(t *testing.T) {
	svc := &fakeInstructorRecoveryService{}
	srv := instructorRecoveryTestServer(t, svc)
	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/instructor/works/work-1/recovery", "")
	var observed domain.InstructorRecoveryStatus
	if err := json.Unmarshal(body, &observed); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || svc.observedWork != "work-1" || observed.Ready || observed.Action != "resume" || observed.ControllerGeneration != "generation-1" || observed.PolicyVersion != "pinned-policy" || svc.recoverCalls != 0 {
		t.Fatalf("observation changed state or scope: status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/instructor/works/work-1/recovery", capturedRecoveryJSON)
	created, err := time.Parse(time.RFC3339, "2026-10-13T09:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.InstructorRecoveryInput{WorkID: "work-1", WorkRevision: 42, PolicyVersion: "pinned-policy", SessionID: "project-1", SessionCreatedAt: created, ProviderConversationID: "thread-1", ControllerGeneration: "generation-1"}
	if status != http.StatusOK || svc.recovery != want || svc.recoverCalls != 1 || !strings.Contains(string(body), `"ready":true`) {
		t.Fatalf("recovery input changed: status=%d body=%s got=%+v", status, body, svc.recovery)
	}
}

func TestInstructorAPIRecoveryRejectsWrongWorkAndUnknownAuthorityFields(t *testing.T) {
	for _, input := range []string{
		strings.Replace(capturedRecoveryJSON, `"work-1"`, `"other-work"`, 1),
		strings.TrimSuffix(capturedRecoveryJSON, "}") + `,"force":true}`,
		strings.TrimSuffix(capturedRecoveryJSON, "}") + `,"approval":"GO"}`,
		capturedRecoveryJSON + `{}`,
	} {
		t.Run(input, func(t *testing.T) {
			svc := &fakeInstructorRecoveryService{}
			srv := instructorRecoveryTestServer(t, svc)
			body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/instructor/works/work-1/recovery", input)
			if status != http.StatusBadRequest || svc.recoverCalls != 0 {
				t.Fatalf("invalid recovery reached service: status=%d body=%s", status, body)
			}
		})
	}
}

func TestInstructorAPIRecoveryReturnsStableActionableConflicts(t *testing.T) {
	for _, tc := range []struct {
		cause error
		code  string
	}{
		{instructorsvc.ErrRecoveryRequired, "INSTRUCTOR_RECOVERY_REQUIRED"},
		{domain.ErrSessionRecoveryChanged, "SESSION_RECOVERY_CHANGED"},
		{sessionmanager.ErrInstructorResumeBlocked, "INSTRUCTOR_RESUME_BLOCKED"},
		{ports.ErrChatRecoveryInconclusive, "CHAT_RECOVERY_INCONCLUSIVE"},
		{instructorsvc.ErrDeliveryUncertain, "INSTRUCTOR_DELIVERY_UNCERTAIN"},
		{instructorsvc.ErrConflict, "INSTRUCTOR_CONFLICT"},
		{instructorsvc.ErrRecoveryInProgress, "SESSION_RECOVERY_IN_PROGRESS"},
		{sessionmanager.ErrResumeInProgress, "SESSION_RECOVERY_IN_PROGRESS"},
		{sessionmanager.ErrSwitchInProgress, "SESSION_RECOVERY_IN_PROGRESS"},
		{sessionmanager.ErrInterfaceTransitionInProgress, "SESSION_RECOVERY_IN_PROGRESS"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			svc := &fakeInstructorRecoveryService{fakeInstructorService: fakeInstructorService{err: fmt.Errorf("recovery observation: %w", tc.cause)}}
			srv := instructorRecoveryTestServer(t, svc)
			body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/instructor/works/work-1/recovery", capturedRecoveryJSON)
			if status != http.StatusConflict || !strings.Contains(string(body), tc.code) || !strings.Contains(string(body), `"requestId":`) {
				t.Fatalf("lost conflict code/request ID: status=%d body=%s", status, body)
			}
		})
	}
}

func TestInstructorAPIRecoveryUsesSessionErrorsAndNativeValidationPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cause   error
		status  int
		code    string
		message string
	}{
		{"provider resume", fmt.Errorf("%w: provider conversation unavailable", ports.ErrChatResumeFailed), http.StatusConflict, "CHAT_RESUME_FAILED", "provider conversation unavailable"},
		{"history unsettled", fmt.Errorf("%w: pending transcript turn", ports.ErrChatHistoryUnsettled), http.StatusConflict, "CHAT_HISTORY_UNSETTLED", "pending transcript turn"},
		{"history unavailable", fmt.Errorf("%w: provider cannot replay transcript", ports.ErrChatHistoryUnavailable), http.StatusConflict, "CHAT_HISTORY_UNAVAILABLE", "provider cannot replay transcript"},
		{"history load failure", fmt.Errorf("%w: provider rejected transcript load", ports.ErrChatHistoryLoadFailed), http.StatusConflict, "CHAT_HISTORY_LOAD_FAILED", "provider rejected transcript load"},
		{"missing handles", sessionmanager.ErrIncompleteHandle, http.StatusConflict, "SESSION_INCOMPLETE_HANDLE", "missing runtime or workspace handles"},
		{"native model over governance", fmt.Errorf("%w: %w: model not advertised", sessionmanager.ErrInstructorResumeBlocked, sessionmanager.ErrUnsupportedModel), http.StatusBadRequest, "UNSUPPORTED_MODEL", "model not advertised"},
		{"native effort over governance", fmt.Errorf("%w: %w: max is unavailable for model", sessionmanager.ErrInstructorResumeBlocked, ports.ErrUnsupportedEffort), http.StatusBadRequest, "UNSUPPORTED_EFFORT", "max is unavailable for model"},
		{"capabilities over governance", fmt.Errorf("%w: %w: catalog is stale", sessionmanager.ErrInstructorResumeBlocked, ports.ErrModelCapabilitiesUnavailable), http.StatusBadRequest, "MODEL_CAPABILITIES_UNAVAILABLE", "catalog is stale"},
		{"driver unavailable", fmt.Errorf("%w: executable is absent", ports.ErrChatDriverUnavailable), http.StatusConflict, "CHAT_DRIVER_UNAVAILABLE", "executable is absent"},
		{"driver incompatible", fmt.Errorf("%w: protocol version mismatch", ports.ErrChatDriverIncompatible), http.StatusConflict, "CHAT_DRIVER_INCOMPATIBLE", "protocol version mismatch"},
		{"authentication", ports.ErrChatAuthRequired, http.StatusConflict, "CHAT_AUTH_REQUIRED", "not authenticated"},
		{"workspace locked", fmt.Errorf("%w: registered worktree is unavailable", ports.ErrWorkspaceLocked), http.StatusConflict, "WORKSPACE_LOCKED", "registered worktree is unavailable"},
		{"unknown remains internal", errors.New("unexpected storage read failure"), http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeInstructorRecoveryService{fakeInstructorService: fakeInstructorService{err: fmt.Errorf("resume project-1: %w", tc.cause)}}
			srv := instructorRecoveryTestServer(t, svc)
			body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/instructor/works/work-1/recovery", capturedRecoveryJSON)
			var response struct{ Code, Message, RequestID string }
			if err := json.Unmarshal(body, &response); err != nil {
				t.Fatal(err)
			}
			if status != tc.status || response.Code != tc.code || !strings.Contains(response.Message, tc.message) || response.RequestID == "" {
				t.Fatalf("wrong recovery envelope: status=%d body=%s", status, body)
			}
		})
	}
}

func TestInstructorAPIPolicyRebindPreservesApprovalAndSnapshotScope(t *testing.T) {
	svc := &fakeInstructorRecoveryService{}
	srv := instructorRecoveryTestServer(t, svc)
	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/instructor/policy-rebind/preview", `{"snapshotId":"snapshot-1","workId":"work-1","reason":"approved mapping"}`)
	if status != http.StatusOK || svc.rebind.Approval != "" || svc.rebind.SnapshotID != "snapshot-1" || svc.previewCalls != 1 || svc.applyCalls != 0 || !strings.Contains(string(body), `"targetPolicyVersion":"target-policy"`) {
		t.Fatalf("preview changed authority: status=%d body=%s input=%+v", status, body, svc.rebind)
	}
	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/instructor/policy-rebind", `{"snapshotId":"snapshot-1","workId":"work-1","reason":"approved mapping","targetPolicyVersion":"target-policy","approval":"POLICY-GO work-1 proposal"}`)
	want := instructorsvc.PolicyRebindInput{SnapshotID: "snapshot-1", WorkID: "work-1", Reason: "approved mapping", TargetPolicyVersion: "target-policy", Approval: "POLICY-GO work-1 proposal"}
	if status != http.StatusOK || svc.rebind != want || svc.applyCalls != 1 {
		t.Fatalf("apply changed captured authority: status=%d body=%s input=%+v", status, body, svc.rebind)
	}
	for _, route := range []string{"/api/v1/instructor/policy-rebind/preview", "/api/v1/instructor/policy-rebind"} {
		body, status, _ = doRequest(t, srv, http.MethodPost, route, `{"snapshotId":"snapshot-1","workId":"work-1","operatorId":"forged"}`)
		if status != http.StatusBadRequest || svc.previewCalls != 1 || svc.applyCalls != 1 {
			t.Fatalf("unknown authority field accepted: status=%d body=%s", status, body)
		}
	}
	svc.err = instructorsvc.ErrConflict
	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/instructor/policy-rebind", `{"snapshotId":"old","workId":"work-1","reason":"approved mapping","targetPolicyVersion":"old-target","approval":"POLICY-GO old"}`)
	if status != http.StatusConflict || !strings.Contains(string(body), "INSTRUCTOR_CONFLICT") {
		t.Fatalf("stale approval lost conflict: status=%d body=%s", status, body)
	}
}

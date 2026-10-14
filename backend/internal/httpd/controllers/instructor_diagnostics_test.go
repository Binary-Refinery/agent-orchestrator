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
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

type diagnosticAPIService struct {
	*fakeSessionService
	scope         domain.InstructorDiagnosticScope
	calls         int
	timeline      bool
	before, limit int64
	reject        bool
}

func (s *diagnosticAPIService) InstructorDiagnostics(_ context.Context, scope domain.InstructorDiagnosticScope, timeline bool, before, limit int64) (domain.InstructorMemberDiagnostics, error) {
	s.scope, s.timeline, s.before, s.limit = scope, timeline, before, limit
	s.calls++
	if s.reject {
		return domain.InstructorMemberDiagnostics{}, apierr.Conflict("INSTRUCTOR_DIAGNOSTIC_SCOPE_CHANGED", "Captured scope changed", nil)
	}
	return domain.InstructorMemberDiagnostics{WorkID: scope.WorkID, MemberID: scope.MemberID, ProjectID: scope.ProjectID, SessionID: scope.SessionID, InstanceID: scope.InstanceID, RuntimeGeneration: scope.RuntimeGeneration, PersistedOnly: true, Atomic: true}, nil
}

func TestInstructorDiagnosticAPIRequiresExactEvidenceAndBoundedPage(t *testing.T) {
	svc := &diagnosticAPIService{fakeSessionService: newFakeSessionService()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Sessions: svc, InstructorEnabled: true}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	const root = "/api/v1/instructor/works/work/members/session:instance/"
	const scopeQuery = "projectId=project&sessionId=session&instanceId=instance&runtimeGeneration="
	for _, suffix := range []string{"diagnostics", "timeline"} {
		t.Run(suffix, func(t *testing.T) {
			calls := svc.calls
			for _, query := range []string{
				"", "projectId=project&sessionId=session&instanceId=instance",
				scopeQuery + "&instanceId=another", scopeQuery + "&runtimeGeneration=other",
				"projectId=&sessionId=session&instanceId=instance&runtimeGeneration=",
				scopeQuery + "&limit=0", scopeQuery + "&limit=101", scopeQuery + "&limit=2&limit=3",
				scopeQuery + "&beforeSequence=-1", scopeQuery + "&beforeSequence=1.2", scopeQuery + "&beforeSequence=1&beforeSequence=2",
			} {
				body, status, _ := doRequest(t, srv, http.MethodGet, root+suffix+"?"+query, "")
				if status != http.StatusBadRequest {
					t.Fatalf("unfenced request query=%s status=%d body=%s", query, status, body)
				}
			}
			if svc.calls != calls {
				t.Fatal("invalid or ambiguous evidence reached diagnostic store")
			}
			body, status, _ := doRequest(t, srv, http.MethodGet, root+suffix+"?"+scopeQuery+"&limit=100&beforeSequence=9", "")
			if status != http.StatusOK || !strings.Contains(string(body), `"persistedOnly":true`) || svc.scope.RuntimeGeneration != "" || svc.scope.MemberID != "session:instance" || svc.scope.WorkID != "work" || svc.scope.ProjectID != "project" || svc.scope.SessionID != "session" || svc.scope.InstanceID != "instance" || svc.before != 9 || svc.limit != 100 || svc.timeline != (suffix == "timeline") {
				t.Fatalf("exact empty generation lost scope=%+v before=%d limit=%d status=%d body=%s", svc.scope, svc.before, svc.limit, status, body)
			}
			svc.reject = true
			body, status, _ = doRequest(t, srv, http.MethodGet, root+suffix+"?"+scopeQuery, "")
			svc.reject = false
			if status != http.StatusConflict || !strings.Contains(string(body), "INSTRUCTOR_DIAGNOSTIC_SCOPE_CHANGED") {
				t.Fatalf("stale scope status=%d body=%s", status, body)
			}
		})
	}
}

func TestInstructorDiagnosticAPIUnavailableFailsClosed(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, enabled := range []bool{false, true} {
		srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Sessions: newFakeSessionService(), InstructorEnabled: enabled}, httpd.ControlDeps{}))
		body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/instructor/works/work/members/session:instance/timeline?projectId=p&sessionId=s&instanceId=i&runtimeGeneration=", "")
		srv.Close()
		want := http.StatusNotImplemented
		if !enabled {
			want = http.StatusServiceUnavailable
		}
		if status != want {
			t.Fatalf("enabled=%v status=%d body=%s", enabled, status, body)
		}
	}
}

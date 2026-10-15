package controllers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

type scopedWorkspaceAPIService struct {
	*fakeSessionService
	scope  sessionsvc.InstructorWorkspaceScope
	calls  int
	reject bool
}

func (s *scopedWorkspaceAPIService) WithScopedWorkspace(ctx context.Context, scope sessionsvc.InstructorWorkspaceScope, operation func(context.Context) error) error {
	s.scope = scope
	s.calls++
	if s.reject {
		return apierr.Conflict("INSTRUCTOR_WORKSPACE_SCOPE_CHANGED", "Captured workspace scope changed", nil)
	}
	return operation(ctx)
}

func TestInstructorWorkspaceAPIFencesEveryMethod(t *testing.T) {
	svc := &scopedWorkspaceAPIService{fakeSessionService: newFakeSessionService()}
	svc.sessions["session"] = domain.Session{SessionRecord: domain.SessionRecord{ID: "session", ProjectID: "project"}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Sessions: svc, InstructorEnabled: true}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	const root = "/api/v1/instructor/works/work/sessions/session/workspace/"
	for _, tc := range []struct{ method, suffix, query, body string }{
		{http.MethodGet, "tree", "path=", ""},
		{http.MethodGet, "search", "query=task", ""},
		{http.MethodGet, "file", "path=task.txt", ""},
		{http.MethodPut, "file", "", `{"path":"task.txt","content":"updated","expectedFileFingerprint":"fingerprint"}`},
		{http.MethodPost, "diffs", "", `{"scope":"all","paths":["task.txt"]}`},
	} {
		t.Run(tc.method+tc.suffix, func(t *testing.T) {
			before := svc.calls
			for _, query := range []string{"", "projectId=project", "instanceId=instance", "projectId=project&instanceId=instance&instanceId=other"} {
				body, status, _ := doRequest(t, srv, tc.method, root+tc.suffix+"?"+query+"&"+tc.query, tc.body)
				if status != http.StatusBadRequest {
					t.Fatalf("unfenced request: status=%d body=%s", status, body)
				}
			}
			if svc.calls != before {
				t.Fatal("missing or ambiguous fence reached service")
			}
			path := root + tc.suffix + "?projectId=project&instanceId=instance&" + tc.query
			svc.reject = true
			body, status, _ := doRequest(t, srv, tc.method, path, tc.body)
			if status != http.StatusConflict {
				t.Fatalf("stale incarnation: status=%d body=%s", status, body)
			}
			svc.reject = false
			body, status, _ = doRequest(t, srv, tc.method, path, tc.body)
			if status != http.StatusOK {
				t.Fatalf("fenced request: status=%d body=%s", status, body)
			}
			if svc.scope.WorkID != "work" || svc.scope.SessionID != "session" || svc.scope.InstanceID != "instance" || svc.scope.ProjectID != "project" {
				t.Fatalf("scope lost: %+v", svc.scope)
			}
		})
	}
}

func TestInstructorWorkspaceAPIUnavailableFailsClosed(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, enabled := range []bool{false, true} {
		srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Sessions: newFakeSessionService(), InstructorEnabled: enabled}, httpd.ControlDeps{}))
		body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/instructor/works/work/sessions/session/workspace/tree?projectId=project&instanceId=instance", "")
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

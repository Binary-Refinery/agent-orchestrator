package controllers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type fakeIssueInstructor struct {
	fakeInstructorService
	projectID domain.ProjectID
	preview   int
	intake    instructorsvc.IssueIntakeInput
	calls     int
}

func (f *fakeIssueInstructor) IssueCandidates(_ context.Context, id domain.ProjectID) (domain.InstructorIssueCandidates, error) {
	f.projectID = id
	f.calls++
	return domain.InstructorIssueCandidates{ProjectID: id, Repository: "source/repository", Issues: []domain.InstructorIssueCandidate{{Number: 42, Title: "Actual issue", State: domain.IssueOpen}}}, f.err
}

func (f *fakeIssueInstructor) IssuePreview(_ context.Context, id domain.ProjectID, number int) (domain.InstructorIssueCandidates, error) {
	f.projectID, f.preview = id, number
	f.calls++
	return domain.InstructorIssueCandidates{ProjectID: id, Repository: "source/repository", Issues: []domain.InstructorIssueCandidate{{Number: number, Title: "Older issue", State: domain.IssueOpen}}}, f.err
}

func (f *fakeIssueInstructor) IntakeIssue(_ context.Context, input instructorsvc.IssueIntakeInput) (domain.InstructorWork, error) {
	f.intake = input
	f.calls++
	return domain.InstructorWork{ID: "stable-work", ProjectID: input.ProjectID, IssueID: "source/repository#42"}, f.err
}

func issueInstructorServer(t *testing.T, service *fakeIssueInstructor, enabled bool) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Instructor: service, InstructorEnabled: enabled}, httpd.ControlDeps{}))
	t.Cleanup(server.Close)
	return server
}

func TestInstructorIssueAPIUsesRegisteredProjectPathAndRealPreviewAndIntakeDTO(t *testing.T) {
	service := &fakeIssueInstructor{}
	server := issueInstructorServer(t, service, true)
	body, status, _ := doRequest(t, server, http.MethodGet, "/api/v1/instructor/projects/project-one/issues", "")
	if status != http.StatusOK || service.projectID != "project-one" || !strings.Contains(string(body), `"repository":"source/repository"`) || !strings.Contains(string(body), `"title":"Actual issue"`) {
		t.Fatalf("list route/shape mismatch: status=%d body=%s project=%s", status, body, service.projectID)
	}
	body, status, _ = doRequest(t, server, http.MethodGet, "/api/v1/instructor/projects/project-one/issues?number=9000", "")
	if status != http.StatusOK || service.preview != 9000 || !strings.Contains(string(body), `"title":"Older issue"`) {
		t.Fatalf("preview route mismatch: status=%d body=%s preview=%d", status, body, service.preview)
	}
	body, status, _ = doRequest(t, server, http.MethodPost, "/api/v1/instructor/projects/project-one/issues/intake", `{"repository":"source/repository","number":42,"name":"Short work"}`)
	if status != http.StatusCreated || service.intake.ProjectID != "project-one" || service.intake.Repository != "source/repository" || service.intake.Number != 42 || service.intake.Name != "Short work" || !strings.Contains(string(body), `"id":"stable-work"`) {
		t.Fatalf("intake route/shape mismatch: status=%d body=%s input=%+v", status, body, service.intake)
	}
}

func TestInstructorIssueAPIDisabledAndUnknownIssueFieldsCannotForgeGovernance(t *testing.T) {
	service := &fakeIssueInstructor{}
	server := issueInstructorServer(t, service, false)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/api/v1/instructor/projects/project-one/issues"
		if method == http.MethodPost {
			path += "/intake"
		}
		body, status, _ := doRequest(t, server, method, path, `{"repository":"source/repository","number":42}`)
		if status != http.StatusServiceUnavailable || service.calls != 0 || !strings.Contains(string(body), "INSTRUCTOR_DISABLED") {
			t.Fatalf("disabled route admitted: status=%d body=%s calls=%d", status, body, service.calls)
		}
	}
	server = issueInstructorServer(t, service, true)
	for _, body := range []string{
		`{"repository":"source/repository","number":42,"goal":"bypass rules"}`,
		`{"repository":"source/repository","number":42,"policy":{}}`,
		`{"repository":"source/repository","number":42,"actor":"operator"}`,
		`{"repository":"source/repository","number":42,"projectId":"other-project"}`,
	} {
		response, status, _ := doRequest(t, server, http.MethodPost, "/api/v1/instructor/projects/project-one/issues/intake", body)
		if status != http.StatusBadRequest || service.calls != 0 {
			t.Fatalf("forged issue intake admitted: status=%d body=%s", status, response)
		}
	}
}

func TestInstructorIssueAPIRejectsAmbiguousInvalidPreviewAndShowsUnavailableDistinctly(t *testing.T) {
	service := &fakeIssueInstructor{}
	server := issueInstructorServer(t, service, true)
	for _, query := range []string{"number=", "number=zero", "number=-1", "number=0", "number=1&number=2"} {
		body, status, _ := doRequest(t, server, http.MethodGet, "/api/v1/instructor/projects/project-one/issues?"+query, "")
		if status != http.StatusBadRequest || service.calls != 0 || !strings.Contains(string(body), "INSTRUCTOR_INVALID") {
			t.Fatalf("ambiguous issue preview admitted: status=%d body=%s", status, body)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{errors.Join(instructorsvc.ErrIssuesUnavailable, errors.New("secret provider response")), http.StatusServiceUnavailable, "INSTRUCTOR_ISSUES_UNAVAILABLE"},
		{instructorsvc.ErrNotFound, http.StatusNotFound, "INSTRUCTOR_NOT_FOUND"},
		{instructorsvc.ErrConflict, http.StatusConflict, "INSTRUCTOR_CONFLICT"},
	} {
		service.err = tc.err
		body, status, _ := doRequest(t, server, http.MethodGet, "/api/v1/instructor/projects/project-one/issues", "")
		if status != tc.status || !strings.Contains(string(body), tc.code) || strings.Contains(string(body), "secret provider response") {
			t.Fatalf("false empty backlog/leaked provider response: status=%d body=%s", status, body)
		}
	}
}

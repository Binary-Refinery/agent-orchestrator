package controllers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type deadlineChecksService struct{ *fakeInstructorService }

func (*deadlineChecksService) RunChecks(context.Context, string, domain.SessionID, []string) (domain.InstructorWork, error) {
	return domain.InstructorWork{}, fmt.Errorf("%w: selected profiles exceed the actual remaining request deadline", instructorsvc.ErrChecksDeadline)
}

func TestInstructorAPIChecksDeadlineHasDistinctPreexecutionEnvelope(t *testing.T) {
	svc := &deadlineChecksService{fakeInstructorService: &fakeInstructorService{}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Instructor: svc, InstructorEnabled: true}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/instructor/works/work-1/checks", `{"sessionId":"worker-1","profiles":["push"]}`)
	var envelope struct {
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || status != http.StatusBadRequest || envelope.Code != "INSTRUCTOR_CHECKS_DEADLINE_PREEXECUTION" || envelope.RequestID == "" {
		t.Fatalf("deadline refusal lost preexecution classification/request ID: status=%d body=%s decode=%v", status, body, err)
	}
}

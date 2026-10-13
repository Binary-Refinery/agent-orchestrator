package controllers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestConversationSendExposesOnlyTypedAdmissionFailureCause(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
		cause  string
	}{
		{"governance", fmt.Errorf("%w: role requires harness opencode, got codex", ports.ErrSemanticDeliveryRejected), http.StatusConflict, "CHAT_EXECUTION_REJECTED", "role requires harness opencode, got codex"},
		{"unknown provider", errors.New("provider failure with private-provider-token"), http.StatusInternalServerError, "INTERNAL_ERROR", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := conversationTestServer(t, &fakeConversationService{sendErr: tc.err})
			request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/sessions/p1-1/conversation/messages", bytes.NewBufferString(`{"text":"private task body","clientMessageId":"assignment"}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Body.Close() }()
			var body struct {
				Code      string `json:"code"`
				Message   string `json:"message"`
				RequestID string `json:"requestId"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tc.status || body.Code != tc.code || body.RequestID == "" {
				t.Fatalf("unexpected response: status=%d body=%+v", response.StatusCode, body)
			}
			if tc.cause != "" && !strings.Contains(body.Message, tc.cause) {
				t.Fatalf("admission cause missing: %q", body.Message)
			}
			if strings.Contains(body.Message, "private task body") || strings.Contains(body.Message, "private-provider-token") {
				t.Fatalf("private provider/request content leaked: %q", body.Message)
			}
		})
	}
}

package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestGovernanceNativeHistoryRequestRejectsUnknownAndTrailingFields(t *testing.T) {
	for _, body := range []string{`{"requireNativeHistory":true,"forceFresh":true}`, `{"requireNativeHistory":true} {}`, `{"requireNativeHistory":"true"}`} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		if _, err := nativeHistoryRequestContext(httptest.NewRecorder(), r); err == nil {
			t.Fatalf("accepted invalid request %q", body)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"requireNativeHistory":true}`))
	ctx, err := nativeHistoryRequestContext(httptest.NewRecorder(), r)
	if err != nil || !domain.NativeHistoryRequired(ctx) {
		t.Fatalf("native requirement lost: %v", err)
	}
	empty := httptest.NewRequest(http.MethodPost, "/", nil)
	if _, err := nativeHistoryRequestContext(httptest.NewRecorder(), empty); err != nil {
		t.Fatal(err)
	}
}

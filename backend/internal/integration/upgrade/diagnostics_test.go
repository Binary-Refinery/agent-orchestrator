//go:build upgrade

package upgrade_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/processalive"
)

const diagnosticLimit = 64 * 1024

func boundDiagnostic(text string) string {
	if len(text) <= diagnosticLimit {
		return text
	}
	return text[:diagnosticLimit/2] + "\n[intermediate diagnostic lines omitted]\n" + text[len(text)-diagnosticLimit/2:]
}

func diagnosticLogExcerpt(data []byte) string {
	var result strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		// Routine successful read polling must not push the startup/lifecycle
		// failure out of CI diagnostics. Retain failed GETs and all other logs.
		if strings.Contains(line, `msg="http request"`) && strings.Contains(line, " method=GET ") && strings.Contains(line, " status=200 ") {
			continue
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return boundDiagnostic(result.String())
}

func (h *harness) workerRecoveryDiagnostic(id string, snapshot controllers.ConversationSnapshotResponse) string {
	var response controllers.SessionResponse
	h.api("GET", "/api/v1/sessions/"+id, nil, &response)
	s := response.Session
	// Decode only non-secret host identity. Never print its capability token,
	// address or raw descriptor, nor prompts/account data from the session DTO.
	var host struct {
		PID       int
		Protocol  string
		StartedAt time.Time
	}
	hostData, err := os.ReadFile(filepath.Join(h.data, "chat-hosts", id, "host.json"))
	if err == nil {
		err = json.Unmarshal(hostData, &host)
	}
	return fmt.Sprintf("session=%s harness=%s mode=%s controller=%s readiness=%s reasonCode=%q reason=%q activity=%s terminated=%t providerPreserved=%t conversation=%s hostPID=%d hostAlive=%t hostProtocol=%s hostStarted=%s hostReadError=%v",
		id, s.Harness, s.Mode, snapshot.Controller, s.StatusReadiness, s.StatusRecoveryReasonCode, s.StatusRecoveryReason,
		s.Activity.State, s.IsTerminated, s.ChatProviderPreserved, snapshot.ConversationID,
		host.PID, host.PID > 0 && processalive.Alive(host.PID), host.Protocol, host.StartedAt.Format(time.RFC3339Nano), err)
}

func (h *harness) providerDiagnostics() string {
	paths, _ := filepath.Glob(filepath.Join(h.fixture, "provider-*.jsonl"))
	var result strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(&result, "fixture journal %s: %v\n", filepath.Base(path), err)
			continue
		}
		var methods []string
		var pid int
		var thread string
		for _, line := range strings.Split(string(data), "\n") {
			// Deliberately omit RPC params, prompt text and any host credentials.
			var frame struct {
				PID      int
				Method   string
				ThreadID string
			}
			if json.Unmarshal([]byte(line), &frame) != nil {
				continue
			}
			pid = frame.PID
			if frame.ThreadID != "" {
				thread = frame.ThreadID
			}
			methods = append(methods, frame.Method)
		}
		fmt.Fprintf(&result, "fixture journal %s pid=%d alive=%t nativeConversation=%q methods=%q\n", filepath.Base(path), pid, pid > 0 && processalive.Alive(pid), thread, methods)
	}
	return boundDiagnostic(result.String())
}

func TestRecoveryDiagnosticsRetainFailureAcrossReadPolling(t *testing.T) {
	poll := "time=x level=INFO msg=\"http request\" id=x method=GET path=/conversation status=200 bytes=100 duration=1ms\n"
	startup := "time=x level=INFO msg=\"daemon startup\"\n"
	failure := "time=x level=ERROR msg=\"reconcile: live pass failed\" error=\"native ownership unavailable\"\n"
	failedRead := "time=x level=INFO msg=\"http request\" id=x method=GET path=/session status=503 bytes=100 duration=1ms\n"
	got := diagnosticLogExcerpt([]byte(startup + strings.Repeat(poll, 1000) + failure + strings.Repeat(poll, 5000) + failedRead))
	for _, retained := range []string{startup, failure, failedRead} {
		if !strings.Contains(got, retained) {
			t.Fatalf("lost lifecycle/failure diagnostic: %q", retained)
		}
	}
	if strings.Contains(got, "status=200") || len(got) > 1024 {
		t.Fatal("successful read polling obscured the failure")
	}
	large := "startup-marker\n" + strings.Repeat("bounded lifecycle detail\n", 10000) + "last-error-marker\n"
	bounded := diagnosticLogExcerpt([]byte(large))
	if len(bounded) > diagnosticLimit+128 || !strings.Contains(bounded, "startup-marker") || !strings.Contains(bounded, "last-error-marker") {
		t.Fatal("large diagnostic lost its boundary evidence or exceeded the output budget")
	}
}

func TestProviderDiagnosticsOmitPrivatePayloads(t *testing.T) {
	fixture := t.TempDir()
	data := `{"pid":1,"threadId":"offline-thread","method":"session/set_config_option","params":{"text":"PRIVATE_PROMPT","token":"PRIVATE_TOKEN"}}` + "\n"
	must(t, os.WriteFile(filepath.Join(fixture, "provider-1.jsonl"), []byte(data), 0o600))
	h := &harness{fixture: fixture}
	got := h.providerDiagnostics()
	if !strings.Contains(got, "session/set_config_option") || !strings.Contains(got, "offline-thread") || strings.Contains(got, "PRIVATE_") {
		t.Fatalf("provider diagnostic lost its trace or exposed payload: %s", got)
	}
}

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstructorCLIStatusCapturesSnapshotAndDeltaQuery(t *testing.T) {
	cfg := setConfigEnv(t)
	var path, base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		path, base = r.URL.Path, r.URL.Query().Get("baseId")
		_, _ = io.WriteString(w, `{"id":"snapshot-7","works":[{"id":"work-1","projectId":"p","name":"Task","state":"ready","phase":"work","next":"review","actions":["weiter"]}]}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "instructor", "status", "--base", "snapshot-6")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/instructor/status" || base != "snapshot-6" || !strings.Contains(out, "Snapshot: snapshot-7") || !strings.Contains(out, "work-1") {
		t.Fatalf("path=%s base=%s output=%s", path, base, out)
	}
}

func TestInstructorCLICommandPreservesObservedScopeAndKey(t *testing.T) {
	cfg := setConfigEnv(t)
	var got instructorCommandDTO
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/instructor/commands" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"batchId":"batch","dispatches":[],"skipped":[]}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "instructor", "command", "Weiter", "--snapshot", "snapshot", "--work", "a,b", "--key", "decision-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "Weiter" || got.SnapshotID != "snapshot" || got.IdempotencyKey != "decision-1" || len(got.WorkIDs) != 2 || got.WorkIDs[1] != "b" {
		t.Fatalf("request=%+v", got)
	}
}

func TestInstructorCLIReportsAcceptanceDistinctFromProgress(t *testing.T) {
	cfg := setConfigEnv(t)
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/api/v1/instructor/reports" {
			t.Errorf("path=%s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"id":"work"}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "instructor", "report", "--work", "work", "--session", "orch", "--kind", "accepted", "--dispatch", "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if got["kind"] != "accepted" || got["dispatchId"] != "dispatch" || got["sessionId"] != "orch" {
		t.Fatalf("report=%+v", got)
	}
}

func TestInstructorCLIPreservesConflictEnvelope(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"message":"obtain a new snapshot","code":"INSTRUCTOR_CONFLICT","requestId":"req-1"}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "instructor", "command", "Weiter", "--snapshot", "old", "--key", "key")
	if err == nil || !strings.Contains(err.Error(), "INSTRUCTOR_CONFLICT") || !strings.Contains(err.Error(), "req-1") || ExitCode(err) != 1 {
		t.Fatalf("err=%v", err)
	}
}

func TestInstructorCLIReconciliationRequiresExplicitObservation(t *testing.T) {
	cfg := setConfigEnv(t)
	var calls int
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/instructor/reconcile" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"id":"work"}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	args := []string{"instructor", "reconcile", "--snapshot", "snapshot", "--work", "work", "--dispatch", "dispatch", "--note", "observed target"}
	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...)
	if err == nil || calls != 0 {
		t.Fatalf("implicit observation err=%v calls=%d", err, calls)
	}
	_, _, err = executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, append(args, "--accepted=false")...)
	if err != nil || calls != 1 || got["accepted"] != false || got["snapshotId"] != "snapshot" || got["dispatchId"] != "dispatch" {
		t.Fatalf("explicit observation err=%v calls=%d payload=%v", err, calls, got)
	}
}

func TestInstructorCLIExceptionPreviewAndApplyForwardExplicitPayload(t *testing.T) {
	cfg := setConfigEnv(t)
	var paths []string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		paths = append(paths, r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"workId":"work","exactApproval":"EXCEPTION-GO work hash"}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	inputPath := filepath.Join(t.TempDir(), "exception.json")
	if err := os.WriteFile(inputPath, []byte(`{"snapshotId":"snapshot","workId":"work","maxFixRounds":3,"reason":"bounded fix","expiresAt":"2026-10-11T15:00:00Z","approval":"EXCEPTION-GO work hash"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"preview", "apply"} {
		if _, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "instructor", "exception", operation, "--input", inputPath); err != nil {
			t.Fatal(err)
		}
	}
	if len(paths) != 2 || paths[0] != "/api/v1/instructor/exceptions/preview" || paths[1] != "/api/v1/instructor/exceptions" || got["approval"] != "EXCEPTION-GO work hash" || got["snapshotId"] != "snapshot" {
		t.Fatalf("paths=%v body=%v", paths, got)
	}
}

func TestInstructorCLIPlanningUsesFrozenIntentAndJobReadRoutes(t *testing.T) {
	cfg := setConfigEnv(t)
	var got map[string]any
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		paths = append(paths, r.URL.Path)
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.WriteHeader(http.StatusAccepted)
		}
		_, _ = io.WriteString(w, `{"id":"plan","state":"queued"}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	for _, args := range [][]string{{"instructor", "plan", "--snapshot", "snapshot", "--key", "intent", "--request", "Clarify priorities"}, {"instructor", "plans"}, {"instructor", "plan-status", "plan"}} {
		if _, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...); err != nil {
			t.Fatal(err)
		}
	}
	if len(paths) != 3 || paths[0] != "/api/v1/instructor/plans" || paths[1] != "/api/v1/instructor/plans" || paths[2] != "/api/v1/instructor/plans/plan" || got["snapshotId"] != "snapshot" || got["idempotencyKey"] != "intent" || got["request"] != "Clarify priorities" {
		t.Fatalf("paths=%v input=%v", paths, got)
	}
}

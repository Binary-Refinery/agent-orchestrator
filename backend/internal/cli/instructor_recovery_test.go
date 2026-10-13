package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInstructorCLIRecoveryAndRebindPreserveCapturedRequest(t *testing.T) {
	for _, tc := range []struct {
		args                []string
		method, path, input string
	}{
		{[]string{"policy-rebind", "preview"}, http.MethodPost, "/api/v1/instructor/policy-rebind/preview", `{"snapshotId":"snapshot-1","workId":"work-1","reason":"approved model mapping"}`},
		{[]string{"policy-rebind", "apply"}, http.MethodPost, "/api/v1/instructor/policy-rebind", `{"snapshotId":"snapshot-1","workId":"work-1","reason":"approved model mapping","targetPolicyVersion":"policy-new","approval":"POLICY-GO work-1 exact-hash"}`},
		{[]string{"recovery", "work-1"}, http.MethodGet, "/api/v1/instructor/works/work-1/recovery", ""},
		{[]string{"recovery", "work-1"}, http.MethodPost, "/api/v1/instructor/works/work-1/recovery", `{"workId":"work-1","workRevision":123,"policyVersion":"policy-pinned","sessionId":"project-1","sessionCreatedAt":"2026-10-13T09:00:00Z","providerConversationId":"thread-1","controllerGeneration":"generation-1"}`},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			cfg := setConfigEnv(t)
			var got map[string]any
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				calls++
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if r.Method == http.MethodPost {
					if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
						t.Error(err)
					}
				}
				_, _ = io.WriteString(w, `{"workId":"work-1","ready":false,"reasonCode":"CONTROLLER_UNAVAILABLE"}`)
			}))
			defer srv.Close()
			writeRunFileFor(t, cfg, srv)
			args := append([]string{"instructor"}, tc.args...)
			if tc.input != "" {
				path := filepath.Join(t.TempDir(), "input.json")
				if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--input", path)
			}
			out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...)
			if err != nil || calls != 1 || !strings.Contains(out, "CONTROLLER_UNAVAILABLE") {
				t.Fatalf("err=%v calls=%d output=%s", err, calls, out)
			}
			if tc.input != "" {
				var want map[string]any
				if err := json.Unmarshal([]byte(tc.input), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("request changed captured scope/approval: got=%v want=%v", got, want)
				}
			}
		})
	}
}

func TestInstructorCLIRecoveryValidationDoesNotContactDaemon(t *testing.T) {
	setConfigEnv(t)
	invalid := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{"workId":`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"policy-rebind", "preview"}, {"policy-rebind", "apply"}, {"recovery"},
		{"policy-rebind", "apply", "--input", invalid}, {"recovery", "work-1", "--input", invalid},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, err := executeCLI(t, Deps{}, append([]string{"instructor"}, args...)...)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("err=%v, want usage error", err)
			}
		})
	}
}

func TestInstructorCLIRecoveryPreservesConflictAndRequestID(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/telemetry/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"message":"refresh the recovery observation","code":"SESSION_RECOVERY_CHANGED","requestId":"req-recovery"}`)
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "instructor", "recovery", "work-1")
	if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), "SESSION_RECOVERY_CHANGED") || !strings.Contains(err.Error(), "req-recovery") {
		t.Fatalf("lost conflict envelope: %v", err)
	}
}

package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteHostCLIUsesLocalControlAPI(t *testing.T) {
	cfg := setConfigEnv(t)
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/mobile/") {
			requests = append(requests, r.Method+" "+r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/mobile/enable":
			_, _ = io.WriteString(w, `{"enabled":true,"hostId":"host-a","endpoints":[{"kind":"lan","host":"192.168.1.10","port":3011,"secure":false}],"password":"pairing-secret"}`)
		case "/api/v1/mobile/status":
			_, _ = io.WriteString(w, `{"enabled":true,"hostId":"host-a","endpoints":[{"kind":"lan","host":"192.168.1.10","port":3011,"secure":false}],"password":"pairing-secret"}`)
		case "/api/v1/mobile/disable":
			_, _ = io.WriteString(w, `{"enabled":false,"hostId":"host-a"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	for _, tc := range []struct {
		verb, request string
		want          []string
		absent        string
	}{
		{"enable", "POST /api/v1/mobile/enable", []string{"host-a", "http://192.168.1.10:3011", "pairing-secret"}, ""},
		{"status", "GET /api/v1/mobile/status", []string{"host-a", "http://192.168.1.10:3011", "pairing-secret"}, ""},
		{"disable", "POST /api/v1/mobile/disable", []string{"disabled"}, "pairing-secret"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			requests = nil
			out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "remote-host", tc.verb)
			if err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 || requests[0] != tc.request {
				t.Fatalf("requests = %v, want %q", requests, tc.request)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Fatalf("output %q missing %q", out, want)
				}
			}
			if tc.absent != "" && strings.Contains(out, tc.absent) {
				t.Fatalf("output %q unexpectedly contains %q", out, tc.absent)
			}
		})
	}
}

func TestRemoteHostCLIRejectsArgsAndPreservesDaemonError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/mobile/enable" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"message":"listener failed","code":"MOBILE_ENABLE","requestId":"req-123"}`)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "remote-host", "enable", "extra")
	if ExitCode(err) != 2 {
		t.Fatalf("extra argument exit code = %d, want 2: %v", ExitCode(err), err)
	}
	_, _, err = executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "remote-host", "enable")
	if err == nil || !strings.Contains(err.Error(), "listener failed (MOBILE_ENABLE) [request req-123]") {
		t.Fatalf("daemon error = %v, want request ID and code", err)
	}
}

func TestRemoteHostCLIHeadlessStartupHint(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, Deps{}, "remote-host", "enable")
	if err == nil || !strings.Contains(err.Error(), "ao daemon") {
		t.Fatalf("missing daemon error = %v, want headless startup hint", err)
	}
}

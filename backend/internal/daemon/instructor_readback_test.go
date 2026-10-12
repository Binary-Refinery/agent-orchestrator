package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorRuntimeReadbackReportsProcessActivationWithoutSecrets(t *testing.T) {
	env := map[string]string{
		"AO_SUPERVISION": "on", "AO_OPENCODE_EFFORT_FLOOR": "max", "AO_REPORT_SYNC": "true",
		"AO_REACTION_LOOP": "true", "AO_TRIAGE_GATE": "1",
		"AO_INSTRUCTOR_POLICY": "/private/policy", "GH_TOKEN": "private-token", "OPENAI_API_KEY": "private-key",
	}
	allowed := map[string]bool{"AO_SUPERVISION": true, "AO_OPENCODE_EFFORT_FLOOR": true, "AO_REPORT_SYNC": true, "AO_REACTION_LOOP": true, "AO_TRIAGE_GATE": true}
	getenv := func(key string) string {
		if !allowed[key] {
			t.Fatalf("readback read non-allowlisted environment %s", key)
		}
		return env[key]
	}
	build := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "native-build-sha"}, {Key: "vcs.modified", Value: "true"},
		{Key: "ignored", Value: "private-build-setting"},
	}}
	var out bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&out, nil))
	log.Info("Instructor runtime readback", instructorRuntimeReadback(getenv, "0.13.0-artifaktory.2", "effective-policy-hash", true, build)...)
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"appVersion": "0.13.0-artifaktory.2", "daemonVersion": "(devel)",
		"instructor": true, "supervision": true, "openCodeEffortFloor": true,
		"reportSyncRequested": true, "reportSyncRuntime": "unsupported", "policyVersion": "effective-policy-hash",
		"reactionLoopRequested": true, "reactionLoopRuntime": "unsupported", "triageGateRequested": true, "triageGateRuntime": "unsupported",
		"buildRevision": "native-build-sha", "buildModified": "true",
	} {
		if got[key] != want {
			t.Errorf("%s=%v, want %v", key, got[key], want)
		}
	}
	if !strings.Contains(out.String(), `"pid":`) || strings.Count(out.String(), "\n") != 1 {
		t.Fatal("readback is missing the launching process or spans more than one line")
	}
	for _, secret := range []string{"private-token", "private-key", "/private/policy", "private-build-setting"} {
		if strings.Contains(out.String(), secret) {
			t.Errorf("readback leaked %q", secret)
		}
	}
}

func TestLogInstructorRuntimeReadbackUsesEffectivePolicyAndWiring(t *testing.T) {
	t.Setenv("AO_INSTRUCTOR", "0")
	for _, test := range []struct {
		name, policyPath, wantPolicy string
		enabled                      bool
	}{
		{name: "wired despite different requested environment", enabled: true, wantPolicy: instructorsvc.PolicyVersion(instructorsvc.DefaultPolicy())},
		{name: "disabled does not load policy", policyPath: "/private/missing-policy", wantPolicy: "inactive"},
		{name: "unavailable effective policy", policyPath: "/private/missing-policy", enabled: true, wantPolicy: "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&out, nil))
			svc := instructorsvc.New(instructorsvc.Deps{PolicyPath: test.policyPath})
			logInstructorRuntimeReadback(context.Background(), log, config.Config{}, test.enabled, svc)
			var got map[string]any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["instructor"] != test.enabled || got["policyVersion"] != test.wantPolicy {
				t.Fatalf("readback followed requested env or invented policy: %v", got)
			}
			if strings.Contains(out.String(), "/private/") {
				t.Fatal("policy failure leaked its path")
			}
		})
	}
}

func TestInstructorRuntimeReadbackDisabledAndUnknownBuildStayExplicit(t *testing.T) {
	attrs := instructorRuntimeReadback(func(string) string { return "" }, "", "inactive", false, nil)
	got := map[string]any{}
	for index := 0; index < len(attrs); index += 2 {
		got[attrs[index].(string)] = attrs[index+1]
	}
	for _, field := range []string{"instructor", "supervision", "openCodeEffortFloor", "reportSyncRequested", "reactionLoopRequested", "triageGateRequested"} {
		if got[field] != false {
			t.Errorf("unset %s unexpectedly active: %v", field, got[field])
		}
	}
	for _, field := range []string{"daemonVersion", "buildRevision", "buildModified"} {
		if got[field] != "unknown" {
			t.Errorf("missing %s invented build provenance: %v", field, got[field])
		}
	}
	if got["policyVersion"] != "inactive" || got["reportSyncRuntime"] != "unsupported" {
		t.Fatalf("disabled runtime readback=%v", got)
	}
}

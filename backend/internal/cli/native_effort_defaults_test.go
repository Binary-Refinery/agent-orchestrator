package cli

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestProjectNativeDefaultsSurviveCLIGetModifyPut(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"demo","config":{"worker":{"agent":"opencode","agentConfig":{"nativeEffortDefaults":{"opencode":{"provider/model":"contributor"}}}},"orchestrator":{"agent":"codex","agentConfig":{"nativeEffortDefaults":{"codex":{"model":"xhigh"}}}},"reviewers":[{"harness":"opencode","agentConfig":{"nativeEffortDefaults":{"opencode":{"provider/model":"ultra"}}}}]}}}`)
	writeRunFileFor(t, cfg, srv)
	deps := Deps{ProcessAlive: func(int) bool { return true }}
	out, _, err := executeCLI(t, deps, "project", "get", "demo", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got projectGetResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Project.Config == nil {
		t.Fatal("CLI discarded project config")
	}
	got.Project.Config.SessionPrefix = "edited"
	edited, err := json.Marshal(got.Project.Config)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := executeCLI(t, deps, "project", "set-config", "demo", "--config-json", string(edited)); err != nil {
		t.Fatal(err)
	}
	var body setConfigRequest
	if err := json.Unmarshal(capture.body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Config.Worker.AgentConfig.NativeEffortDefaults["opencode"]["provider/model"] != "contributor" || body.Config.Orchestrator.AgentConfig.NativeEffortDefaults["codex"]["model"] != "xhigh" || body.Config.Reviewers[0].AgentConfig.NativeEffortDefaults["opencode"]["provider/model"] != "ultra" {
		t.Fatalf("CLI dropped native defaults: %s", capture.body)
	}
}

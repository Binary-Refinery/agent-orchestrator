package domain

import (
	"encoding/json"
	"testing"
)

func TestNativeEffortDefaultsScopeAndRoundTrip(t *testing.T) {
	defaults := NativeEffortDefaults{"opencode": {"provider/model-a": "contributor", "other/model-a": "ultra"}, "codex": {"model-a": "xhigh"}}
	config := AgentConfig{NativeEffortDefaults: &defaults}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var restored AgentConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ harness, model, want string }{
		{"opencode", "provider/model-a", "contributor"}, {"opencode", "other/model-a", "ultra"}, {"codex", "model-a", "xhigh"}, {"codex", "provider/model-a", ""},
	} {
		got, ok := restored.NativeEffort(row.harness, row.model)
		if got != row.want || ok != (row.want != "") {
			t.Fatalf("%s/%s = %q %v", row.harness, row.model, got, ok)
		}
	}
	if restored.IsZero() {
		t.Fatal("project-only defaults were treated as empty config")
	}
}

func TestNativeEffortDefaultMergeDoesNotMutateSavedMaps(t *testing.T) {
	base := NativeEffortDefaults{"codex": {"model-a": "high"}}
	role := NativeEffortDefaults{"codex": {"model-b": "xhigh"}}
	merged := MergeNativeEffortDefaults(&base, &role)
	(*merged)["codex"]["model-a"] = "low"
	if base["codex"]["model-a"] != "high" || role["codex"]["model-b"] != "xhigh" {
		t.Fatal("resolution mutated persisted defaults")
	}
}

func TestNativeEffortDefaultsRejectNonExactSelections(t *testing.T) {
	for _, defaults := range []NativeEffortDefaults{{"": {"model": "high"}}, {"codex": {"": "high"}}, {"codex": {"model": ""}}, {"codex": {" model": "high"}}, {"codex": {"model": "high\x00"}}} {
		if err := (AgentConfig{NativeEffortDefaults: &defaults}).Validate(); err == nil {
			t.Fatalf("accepted invalid mapping %+v", defaults)
		}
	}
}

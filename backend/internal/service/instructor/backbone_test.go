package instructor

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func testQualification(t *testing.T) []byte {
	t.Helper()
	cases := map[string]qualificationCase{}
	for _, name := range qualificationCases {
		cases[name] = qualificationCase{Result: "pass", EvidenceRef: "fixture:" + name}
	}
	data, err := json.Marshal(qualification{SchemaVersion: "1", Harness: "opencode", RuntimeVersion: "1.19.0", Model: "test/model", Role: "implementer", Platform: runtime.GOOS, Interface: "chat", ConfigurationContract: "ao-opencode-backbone-v1", ObservedAt: "2026-10-16T10:00:00Z", Status: "qualified", EvidenceRef: "fixture:synthetic-admission-only", EvidenceSHA256: strings.Repeat("a", 64), Cases: cases})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBackboneQualificationRequiresExactAttestedTupleAndCases(t *testing.T) {
	raw := testQualification(t)
	if err := validateQualification(raw, "implementer", "test/model", runtime.GOOS, "chat"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, field string
		value       any
	}{
		{"model", "model", "other/model"}, {"role", "role", "reviewer"}, {"platform", "platform", "unknown"}, {"interface", "interface", "tui"}, {"runtime", "runtimeVersion", "1.19.1"}, {"contract", "configurationContract", "other"}, {"status", "status", "pending"}, {"date", "observedAt", "yesterday"}, {"evidence", "evidenceRef", ""}, {"digest", "evidenceSha256", "bad"}, {"cases", "cases", map[string]any{}}, {"missing case proof", "cases", map[string]any{"context_loading": map[string]string{"result": "pass", "evidenceRef": "fixture:one"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var object map[string]any
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			object[tc.field] = tc.value
			bad, _ := json.Marshal(object)
			if err := validateQualification(bad, "implementer", "test/model", runtime.GOOS, "chat"); err == nil {
				t.Fatal("mismatched synthetic qualification admitted")
			}
		})
	}
}

func TestBackboneQualificationRejectsMissingAmbiguousAndNullRecords(t *testing.T) {
	raw := testQualification(t)
	for _, data := range [][]byte{nil, []byte(`null`), []byte(`{}`), []byte(strings.Replace(string(raw), `"model":"test/model"`, `"model":"test/model","model":"test/model"`, 1)), []byte(strings.Replace(string(raw), `"model":"test/model"`, `"Model":"test/model"`, 1)), []byte(strings.Replace(string(raw), `"model":"test/model"`, `"model":null`, 1))} {
		if err := validateQualification(data, "implementer", "test/model", runtime.GOOS, "chat"); err == nil {
			t.Fatalf("ambiguous qualification admitted: %s", data)
		}
	}
}

func TestBackboneAbsentPolicyRemainsOmittedAndHashStable(t *testing.T) {
	legacy := DefaultPolicy()
	before := PolicyVersion(legacy)
	wire, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), `"backbone"`) {
		t.Fatal("legacy policy acquired optional field")
	}
	var roundTrip = legacy
	if err := json.Unmarshal(wire, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if PolicyVersion(roundTrip) != before {
		t.Fatal("legacy hash changed on durable round trip")
	}
	legacy.Backbone = json.RawMessage(`null`)
	if err := ValidatePolicy(legacy); err == nil {
		t.Fatal("explicit null binding fell back to legacy")
	}
}

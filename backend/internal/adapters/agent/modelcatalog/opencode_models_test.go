package modelcatalog

import (
	"reflect"
	"testing"
)

func TestOpenCodeVerboseCatalogPreservesNativeNamesAndCapabilityUnknown(t *testing.T) {
	models, err := parseOpenCodeModels([]byte(`custom/reasoner
{"name":"Reasoner","variants":{"contributor":{"reasoningEffort":"high"},"ultra":{"reasoningEffort":"xhigh"},"disabled":{"disabled":true},"default":{}},"defaultVariant":"contributor"}
custom/plain
{"name":"Plain","variants":{}}
custom/legacy
custom/v2
{"variants":[{"id":"deep"},{"id":"hidden","disabled":true},{"id":"minimal"}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]int)
	for i, model := range models {
		byID[model.ID] = i
	}
	if len(models) != 4 {
		t.Fatalf("models=%+v", models)
	}
	reasoner := models[byID["custom/reasoner"]]
	if reasoner.Label != "Reasoner" || reasoner.EffortSupport != "supported" || reasoner.DefaultEffort != "contributor" || !reflect.DeepEqual(reasoner.Efforts, []string{"contributor", "ultra", "default"}) {
		t.Fatalf("native catalog=%+v", reasoner)
	}
	if models[byID["custom/plain"]].EffortSupport != "none" || models[byID["custom/legacy"]].EffortSupport != "unknown" {
		t.Fatalf("absent variants must not be inferred as unsupported: %+v", models)
	}
	if !reflect.DeepEqual(models[byID["custom/v2"]].Efforts, []string{"deep", "minimal"}) {
		t.Fatalf("native array order=%+v", models[byID["custom/v2"]])
	}
}

func TestOpenCodeVerboseCatalogRejectsInvalidVariantMetadata(t *testing.T) {
	for _, output := range []string{
		"custom/model\n{bad json}",
		`custom/model
{"variants":"high"}`,
		`custom/model
{"variants":{"deep":"settings-not-an-object"}}`,
	} {
		if _, err := parseOpenCodeModels([]byte(output)); err == nil {
			t.Fatalf("accepted %q", output)
		}
	}
}

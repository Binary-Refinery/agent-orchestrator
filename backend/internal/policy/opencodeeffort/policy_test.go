package opencodeeffort

import (
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestMissingEffortUsesOnlyAdvertisedLegacyMax(t *testing.T) {
	for _, automation := range []bool{false, true} {
		raised, out, audit, err := Apply(true, domain.HarnessOpenCode, "s1", "", automation, []string{"low", "max"})
		if err != nil || !raised || out != "max" || audit == nil || audit.Before != "" || audit.After != "max" {
			t.Fatalf("missing native max: raised=%v out=%q audit=%+v err=%v", raised, out, audit, err)
		}
	}
	for _, supported := range [][]string{nil, {"low", "high"}, {"contributor", "ultra"}} {
		_, _, _, err := Apply(true, domain.HarnessOpenCode, "s", "", false, supported)
		if !errors.Is(err, ErrEffortRequired) {
			t.Fatalf("supported=%v: want explicit native choice, got %v", supported, err)
		}
	}
}

func TestExplicitNativeEffortIsNeverReplacedByUniversalMax(t *testing.T) {
	for _, effort := range []string{"low", "high", "max", "ultra", "contributor", "default"} {
		raised, out, audit, err := Apply(true, domain.HarnessOpenCode, "s", effort, false, []string{effort, "max"})
		if err != nil || raised || out != effort || audit != nil {
			t.Fatalf("explicit %q: raised=%v out=%q audit=%+v err=%v", effort, raised, out, audit, err)
		}
	}
}

func TestUnknownNativeEffortFailsClosed(t *testing.T) {
	for _, effort := range []string{"turbo", "MAX"} {
		_, _, _, err := Apply(true, domain.HarnessOpenCode, "s", effort, false, []string{"max"})
		if !errors.Is(err, ErrUnknownEffort) {
			t.Fatalf("effort %q: want ErrUnknownEffort, got %v", effort, err)
		}
	}
}

func TestDisabledChangesNothing(t *testing.T) {
	for _, effort := range []string{"", "low", "turbo-bogus"} {
		raised, out, audit, err := Apply(false, domain.HarnessOpenCode, "s", effort, false, nil)
		if err != nil || raised || out != effort || audit != nil {
			t.Fatalf("disabled: %v %q %v %v", raised, out, audit, err)
		}
	}
	raised, out, audit, err := Apply(true, domain.HarnessCodex, "s", "", false, nil)
	if err != nil || raised || out != "" || audit != nil {
		t.Fatal("other harness must not act")
	}
}

func TestEnabledDefaultOff(t *testing.T) {
	if EnabledFromEnv("") || EnabledFromEnv("0") || EnabledFromEnv("false") {
		t.Fatal("default must be off")
	}
	if !EnabledFromEnv("1") || !EnabledFromEnv("true") || !EnabledFromEnv("max") {
		t.Fatal("opt-in values must enable")
	}
}

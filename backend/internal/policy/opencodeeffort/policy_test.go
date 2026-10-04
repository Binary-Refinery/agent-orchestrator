package opencodeeffort

import (
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestFloorRaisesMissingAndLow(t *testing.T) {
	for _, effort := range []string{"", "  ", "default", "minimal", "low", "medium", "high", "xhigh"} {
		raised, out, audit, err := Apply(true, domain.HarnessOpenCode, "s1", effort, false)
		if err != nil {
			t.Fatalf("effort %q: %v", effort, err)
		}
		if !raised || out != Floor || audit == nil {
			t.Fatalf("effort %q: raised=%v out=%q audit=%v", effort, raised, out, audit)
		}
		if audit.Session != "s1" || audit.Before != effort || audit.After != Floor || audit.Reason == "" {
			t.Fatalf("bad audit: %+v", audit)
		}
		if audit.Reason != "effort-floor:interactive-spawn:missing-raised-to-max" && (effort == "" || effort == "  ") {
			t.Fatalf("bad reason: %q", audit.Reason)
		}
	}
	raised, _, audit, err := Apply(true, domain.AgentHarness("opencode-v2"), "s2", "low", true)
	if err != nil || !raised || audit.Reason != "effort-floor:automation-dispatch:raised-to-max" {
		t.Fatalf("automation reason: %+v %v", audit, err)
	}
}

func TestDisabledChangesNothing(t *testing.T) {
	for _, effort := range []string{"", "low", "turbo-bogus"} {
		raised, out, audit, err := Apply(false, domain.HarnessOpenCode, "s", effort, false)
		if err != nil || raised || out != effort || audit != nil {
			t.Fatalf("disabled: %v %v %q %v", raised, out, effort, err)
		}
	}
	raised, out, audit, err := Apply(true, domain.HarnessCodex, "s", "", false)
	if err != nil || raised || out != "" || audit != nil {
		t.Fatalf("other harness must not act")
	}
}

func TestNeverLowersMax(t *testing.T) {
	raised, out, audit, err := Apply(true, domain.HarnessOpenCode, "s", "max", false)
	if err != nil || raised || out != "max" || audit != nil {
		t.Fatalf("max must stay: %v %q %v %v", raised, out, audit, err)
	}
	raised, out, audit, err = Apply(true, domain.HarnessOpenCode, "s", "MAX", false)
	if err != nil || raised || out != "MAX" || audit != nil {
		t.Fatalf("MAX must stay: %v %q %v %v", raised, out, audit, err)
	}
}

func TestUnknownFailClosed(t *testing.T) {
	_, _, _, err := Apply(true, domain.HarnessOpenCode, "s", "turbo", false)
	if !errors.Is(err, ErrUnknownEffort) {
		t.Fatalf("want ErrUnknownEffort, got %v", err)
	}
}

func TestEnabledDefaultOff(t *testing.T) {
	if EnabledFromEnv("") || EnabledFromEnv("0") || EnabledFromEnv("false") {
		t.Fatalf("default must be off")
	}
	if !EnabledFromEnv("1") || !EnabledFromEnv("true") || !EnabledFromEnv("max") {
		t.Fatalf("opt-in values must enable")
	}
}

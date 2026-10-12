// Package opencodeeffort implements the opt-in opencode effort-floor policy.
//
// When enabled, spawns for the opencode harnesses with a missing or
// omitted effort use the native "max" variant only when that specific model
// advertises it. Otherwise an explicit model-supported choice is required.
package opencodeeffort

import (
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Floor is the legacy opt-in variant, never an assumed universal model level.
const Floor = "max"

// ErrUnknownEffort fails a spawn closed when the requested variant is not in
// the model's native vocabulary, so AO rejects instead of guessing a mapping.
var ErrUnknownEffort = errors.New("opencode effort floor: unknown effort variant")

// ErrEffortRequired rejects a missing choice when no native default is safe.
var ErrEffortRequired = errors.New("opencode effort floor: select an explicit native effort for this model")

// AuditEntry records one floor raise with session, before/after and reason.
type AuditEntry struct {
	Session string
	Harness domain.AgentHarness
	Before  string
	After   string
	Reason  string
}

// Enabled reports whether the opt-in floor policy is active. Default off;
// set AO_OPENCODE_EFFORT_FLOOR=1 (or "true"/"max") to enable.
func Enabled() bool {
	return EnabledFromEnv(os.Getenv("AO_OPENCODE_EFFORT_FLOOR"))
}

// EnabledFromEnv parses the opt-in switch value for tests and callers.
func EnabledFromEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "max":
		return true
	default:
		return false
	}
}

// IsOpenCodeHarness reports whether the floor policy applies to a harness.
func IsOpenCodeHarness(h domain.AgentHarness) bool {
	return h == domain.HarnessOpenCode || string(h) == "opencode-v2"
}

// Apply uses only the chosen model's advertised variants. Explicit native
// choices remain authoritative; the legacy switch supplies max only when
// omitted and supported. No ordering is guessed for custom variant names.
func Apply(enabled bool, harness domain.AgentHarness, session, effort string, automation bool, supported []string) (raised bool, out string, audit *AuditEntry, err error) {
	if !enabled || !IsOpenCodeHarness(harness) {
		return false, effort, nil, nil
	}
	before := effort
	trimmed := strings.TrimSpace(effort)
	hasMax := false
	for _, variant := range supported {
		if variant == trimmed && trimmed != "" {
			return false, trimmed, nil, nil
		}
		hasMax = hasMax || variant == Floor
	}
	if trimmed != "" {
		return false, effort, nil, ErrUnknownEffort
	}
	if !hasMax {
		return false, effort, nil, ErrEffortRequired
	}
	reason := "interactive-spawn"
	if automation {
		reason = "automation-dispatch"
	}
	entry := &AuditEntry{Session: session, Harness: harness, Before: before, After: Floor, Reason: "effort-floor:" + reason + ":missing-native-max"}
	return true, Floor, entry, nil
}

// Log emits the audit entry through slog with session, before/after, reason.
func Log(log *slog.Logger, entry *AuditEntry) {
	if log == nil || entry == nil {
		return
	}
	log.Info("opencode effort floor raised",
		"session", entry.Session,
		"harness", string(entry.Harness),
		"before", entry.Before,
		"after", entry.After,
		"reason", entry.Reason,
	)
}

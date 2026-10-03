// Package opencodeeffort implements the opt-in opencode effort-floor policy.
//
// When enabled, spawns for the opencode harnesses with a missing or
// below-maximum effort are raised to maximum. The policy never lowers an
// effort, never acts while disabled (default off), and fails closed on
// unknown variant values instead of guessing.
package opencodeeffort

import (
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Floor is the effort level missing or lower values are raised to.
const Floor = "max"

// ErrUnknownEffort fails a spawn closed when the requested variant is not in
// the known vocabulary, so AO rejects instead of guessing a mapping.
var ErrUnknownEffort = errors.New("opencode effort floor: unknown effort variant")

// rank orders the known effort vocabulary from lowest to highest. "default"
// and "minimal" are the provider reset values observed when no explicit
// effort is given; empty means omitted and is also raised to Floor.
var rank = map[string]int{
	"default": 0,
	"minimal": 0,
	"low":     1,
	"medium":  2,
	"high":    3,
	"xhigh":   4,
	"max":     5,
}

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

// Apply raises effort to Floor when the policy is enabled and the harness is
// an opencode harness. It returns raised=false when nothing changed.
// Unknown non-empty variants fail closed with ErrUnknownEffort. When enabled
// is false, or the harness is not opencode, the input is returned unchanged
// and no audit entry is produced. Effort is never lowered.
func Apply(enabled bool, harness domain.AgentHarness, session, effort string, automation bool) (raised bool, out string, audit *AuditEntry, err error) {
	if !enabled || !IsOpenCodeHarness(harness) {
		return false, effort, nil, nil
	}
	before := effort
	trimmed := strings.TrimSpace(effort)
	lowered := strings.ToLower(trimmed)
	if trimmed == "" {
		reason := "interactive-spawn"
		if automation {
			reason = "automation-dispatch"
		}
		entry := &AuditEntry{Session: session, Harness: harness, Before: before, After: Floor, Reason: "effort-floor:" + reason + ":missing-raised-to-max"}
		return true, Floor, entry, nil
	}
	r, ok := rank[lowered]
	if !ok {
		return false, effort, nil, ErrUnknownEffort
	}
	if r >= rank[Floor] {
		return false, effort, nil, nil
	}
	reason := "interactive-spawn"
	if automation {
		reason = "automation-dispatch"
	}
	entry := &AuditEntry{Session: session, Harness: harness, Before: before, After: Floor, Reason: "effort-floor:" + reason + ":raised-to-max"}
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

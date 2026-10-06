// Package supervision implements slice af-ao-supervision-v1: opt-in,
// default-off supervision guarantees for worker chains.
//
// Four parts, all fail-closed and side-effect free (no network, no
// storage, no daemon wiring in this package):
//
//  1. Wake-up guarantee (wakeup.go): a done/checkpoint report mandates
//     one orchestrator turn. Delivery is retried; terminal failure
//     denies with an audit record the caller must persist.
//  2. Stall watchdog (watchdog.go): no progress for longer than the
//     configured quiet window first nudges, then escalates to the
//     human. There is no silent-idle outcome.
//  3. Chain driver (chain.go): step state machine over chain steps
//     (worker -> review -> triage -> fix -> delta) with per-step
//     timeouts. Steps are keyed by chain position, never by role
//     name, so future roles fall under the same machine.
//  4. Budgets (budget.go): hard round/session caps; a third fix round
//     is impossible without an explicit human exception.
//
// Red lines (redline.go, enforced by design, no code path exists for
// them): no auto-push, no auto-merge, no task-worker auto-terminate,
// no finding auto-confirm, no publish-go automation, no human-gate
// bypass.
package supervision

import (
	"os"
	"strings"
)

// RejectReason is the typed reason every refusal carries (fail-closed).
type RejectReason string

// Typed fail-closed refusal reasons across all four parts.
const (
	ReasonDisabled   RejectReason = "supervision_disabled"
	ReasonNeedsHuman RejectReason = "supervision_needs_human"
	ReasonRedLine    RejectReason = "supervision_red_line"

	ReasonWakeupDelivered   RejectReason = "supervision_wakeup_delivered"
	ReasonWakeupRetry       RejectReason = "supervision_wakeup_retry"
	ReasonWakeupFailed      RejectReason = "supervision_wakeup_failed_audit"
	ReasonWakeupInvalidKind RejectReason = "supervision_wakeup_invalid_kind"

	ReasonStallNudge     RejectReason = "supervision_stall_nudge"
	ReasonStallEscalate  RejectReason = "supervision_stall_escalate"
	ReasonStallInvalid   RejectReason = "supervision_stall_invalid_window"
	ReasonStallNoOutcome RejectReason = "supervision_stall_no_silent_idle"

	ReasonChainTimeout  RejectReason = "supervision_chain_timeout"
	ReasonChainInvalid  RejectReason = "supervision_chain_invalid_transition"
	ReasonChainTerminal RejectReason = "supervision_chain_terminal"

	ReasonBudgetRound   RejectReason = "supervision_budget_round_exhausted"
	ReasonBudgetSession RejectReason = "supervision_budget_session_exhausted"
	ReasonBudgetFixCap  RejectReason = "supervision_budget_fix_cap"
)

// Decision is the fail-closed outcome of one check.
type Decision struct {
	Allow  bool
	Reason RejectReason
	Detail string
}

func allowWith(reason RejectReason, detail string) Decision {
	return Decision{Allow: true, Reason: reason, Detail: detail}
}

func deny(reason RejectReason, detail string) Decision {
	return Decision{Reason: reason, Detail: detail}
}

// Enabled reports whether the opt-in policy applies. Default off;
// set AO_SUPERVISION=1 (or "true"/"on") to enable.
func Enabled() bool { return EnabledFromEnv(os.Getenv("AO_SUPERVISION")) }

// EnabledFromEnv parses the opt-in switch for tests and callers.
func EnabledFromEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

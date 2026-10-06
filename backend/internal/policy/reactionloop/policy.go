// Package reactionloop implements Slice 4 of af-ao-automation-v2: an opt-in,
// default-off reaction loop over Codex review comments plus check runs.
//
// The loop polls each active PR, extracts P0/P1 findings from Codex review
// comments, aggregates check runs, and walks a small PR state machine:
// P0/P1 findings lead to a triage proposal that waits for an explicit human
// confirmation before any fix worker may start, and a fully green poll
// leads to a closeout draft. Decisions stay human: triage confirmation and
// merge are never automatic.
//
// Red lines (enforced by design, no code path exists for them): no
// auto-push, no auto-merge, no task-worker auto-terminate, no finding
// auto-confirm, no publish-go automation. This package only plans polls,
// drafts triage proposals and closeout notes, and records explicit human
// approvals. It performs no network calls, opens no storage, and wires
// into no daemon path.
package reactionloop

import (
	"os"
	"strings"
)

// RejectReason is the typed reason every refusal carries (fail-closed).
type RejectReason string

// Typed fail-closed refusal reasons for the policy gate, the budget gate,
// the state machine, and the red-line guard.
const (
	ReasonDisabled        RejectReason = "reaction_disabled"
	ReasonBudgetPR        RejectReason = "reaction_budget_pr_exhausted"
	ReasonBudgetDay       RejectReason = "reaction_budget_day_exhausted"
	ReasonNeedsHuman      RejectReason = "reaction_needs_human"
	ReasonInvalidStep     RejectReason = "reaction_invalid_transition"
	ReasonTerminal        RejectReason = "reaction_closeout_terminal"
	ReasonRedLine         RejectReason = "reaction_red_line"
	ReasonMissingFindings RejectReason = "reaction_missing_findings"
	ReasonNoEvidence      RejectReason = "reaction_no_evidence"
)

// Decision is the fail-closed outcome of one check.
type Decision struct {
	Allow  bool
	Reason RejectReason
	Detail string
}

func allow() Decision { return Decision{Allow: true} }

func deny(reason RejectReason, detail string) Decision {
	return Decision{Reason: reason, Detail: detail}
}

// Enabled reports whether the opt-in policy applies. Default off;
// set AO_REACTION_LOOP=1 (or "true"/"on") to enable.
func Enabled() bool { return EnabledFromEnv(os.Getenv("AO_REACTION_LOOP")) }

// EnabledFromEnv parses the opt-in switch for tests and callers.
func EnabledFromEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Budget caps how many review polls the loop may plan. Both caps are
// fail-closed: reaching either denies further polls until the counters
// reset (per-PR on a new head, per-day on day rollover by the caller).
type Budget struct {
	// MaxPerPR bounds polls for one PR (one head).
	MaxPerPR int
	// MaxPerDay bounds polls across all PRs per calendar day.
	MaxPerDay int
}

// DefaultBudget is the conservative starting cap: at most 5 polls per PR
// head and at most 20 polls per day.
func DefaultBudget() Budget { return Budget{MaxPerPR: 5, MaxPerDay: 20} }

// Usage carries the current counters for one planning decision.
type Usage struct {
	// PollsPR counts polls already planned for this PR head.
	PollsPR int
	// PollsDay counts polls already planned today across PRs.
	PollsDay int
}

// PlanPoll decides whether another review poll may be planned for one PR.
// Disabled denies closed with ReasonDisabled; exhausted caps deny with the
// matching budget reason. A clean budget allows.
func PlanPoll(enabled bool, budget Budget, usage Usage) Decision {
	if !enabled {
		return deny(ReasonDisabled, "reaction-loop policy off")
	}
	if budget.MaxPerPR <= 0 || usage.PollsPR >= budget.MaxPerPR {
		return deny(ReasonBudgetPR, "per-PR poll budget reached")
	}
	if budget.MaxPerDay <= 0 || usage.PollsDay >= budget.MaxPerDay {
		return deny(ReasonBudgetDay, "daily poll budget reached")
	}
	return allow()
}

// RedLineKind names an automation the loop must never perform.
type RedLineKind string

// Closed vocabulary of forbidden automations. No transition, planner, or
// runner in this package accepts any of them.
const (
	RedLineAutoPush      RedLineKind = "auto_push"
	RedLineAutoMerge     RedLineKind = "auto_merge"
	RedLineAutoTerminate RedLineKind = "auto_terminate_worker"
	RedLineAutoConfirm   RedLineKind = "auto_confirm_finding"
	RedLineAutoPublish   RedLineKind = "auto_publish_go"
	RedLineAutoFix       RedLineKind = "auto_fix"
)

// RedLines lists every forbidden automation for enumeration tests.
var RedLines = []RedLineKind{
	RedLineAutoPush,
	RedLineAutoMerge,
	RedLineAutoTerminate,
	RedLineAutoConfirm,
	RedLineAutoPublish,
	RedLineAutoFix,
}

// AttemptRedLine is the single choke point for forbidden automations: it
// always denies with ReasonRedLine, whether the policy is on or off, so
// no caller can route an automation through this package.
func AttemptRedLine(kind RedLineKind) Decision {
	return deny(ReasonRedLine, "forbidden automation: "+string(kind))
}

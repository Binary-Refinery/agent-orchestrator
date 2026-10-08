// Package triagegate implements the Triage-Gate Mini-Slice: Local-Chain-First
// as entry condition into the Slice-4 state machine (reactionloop).
//
// GitHub P0/P1 findings may only be processed by the Slice-4 automaton
// (watching/delta_review + poll_findings) when the bound local chain
// artifacts are belegt: Review-Report, Triage-Entscheid, and Delta-Status
// as session/commit facts handed in by the caller. Missing artifacts deny
// with wait+melden (EffectWait): never silently skipped, never counted as
// done. Unclear evidence denies fail-closed with stop+melden (EffectStop).
// The gate is opt-in and default-off (AO_TRIAGE_GATE); disabled denies
// closed so no finding enters Slice-4 without an explicit opt-in.
//
// Red lines (enforced by design, no code path exists for them): no silent
// skip of missing evidence, no done-without-evidence, no finding
// auto-confirm, no human-gate bypass. This package only rules entry; it
// starts no worker, confirms nothing, and merges/publishes nothing. It
// performs no network calls, opens no storage, and wires into no daemon
// path. Callers must run CheckEntry before reactionloop.Advance with
// poll_findings and must not advance on Wait or Stop.
package triagegate

import (
	"os"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/policy/reactionloop"
)

// RejectReason is the typed reason every refusal carries (fail-closed).
type RejectReason string

// Typed fail-closed refusal reasons for the opt-in switch, the chain
// evidence gate, invalid entries, and the red-line guard.
const (
	ReasonDisabled           RejectReason = "triagegate_disabled"
	ReasonWaitReviewReport   RejectReason = "triagegate_wait_review_report"
	ReasonWaitTriageDecision RejectReason = "triagegate_wait_triage_decision"
	ReasonWaitDeltaStatus    RejectReason = "triagegate_wait_delta_status"
	ReasonUnclearEvidence    RejectReason = "triagegate_unclear_evidence"
	ReasonInvalidEntry       RejectReason = "triagegate_invalid_entry"
	ReasonRedLine            RejectReason = "triagegate_red_line"
	ReasonEnter              RejectReason = "triagegate_chain_bound"
	ReasonPass               RejectReason = "triagegate_scope_pass"
)

// Effect is the gate outcome for the caller: enter (GREEN), wait+melden
// (HOLD), stop+melden (RED), or pass (gate not applicable to this move).
type Effect string

// Closed effect vocabulary.
const (
	// EffectEnter (GREEN): the local chain is belegt, the finding may
	// enter the Slice-4 state machine via reactionloop.Advance.
	EffectEnter Effect = "enter"
	// EffectWait (HOLD): chain evidence is missing. Wait and meld;
	// never silently skip and never count as done.
	EffectWait Effect = "wait"
	// EffectStop (RED): unclear evidence, invalid entry, disabled gate,
	// or red-line attempt. Stop and meld immediately.
	EffectStop Effect = "stop"
	// EffectPass: this state/event is no Slice-4 finding entry, so the
	// gate does not rule. Slice-4 Advance still enforces its own
	// evidence gates (including invalid-transition denial).
	EffectPass Effect = "pass"
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

// Enabled reports whether the opt-in gate applies. Default off;
// set AO_TRIAGE_GATE=1 (or "true"/"on") to enable.
func Enabled() bool { return EnabledFromEnv(os.Getenv("AO_TRIAGE_GATE")) }

// EnabledFromEnv parses the opt-in switch for tests and callers.
func EnabledFromEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Artifact is one bound chain artifact: a session/commit fact handed in
// by the caller. Present marks the fact as observed; Ref names the bound
// session/commit fact (report id, decision id, status id). A fact counts
// as belegt only when observed AND bound: Present with an empty Ref, or a
// Ref without Present, is unclear evidence and denies fail-closed, so a
// half-wired chain can never read as complete.
type Artifact struct {
	Present bool
	Ref     string
}

// bound reports whether the artifact is observed and bound to a named
// session/commit fact.
func (a Artifact) bound() bool { return a.Present && strings.TrimSpace(a.Ref) != "" }

// unclear reports half-wired evidence: observed-but-unbound or
// bound-but-unobserved. Both deny fail-closed with stop+melden.
func (a Artifact) unclear() bool { return a.Present != (strings.TrimSpace(a.Ref) != "") }

// ChainEvidence carries the three bound local chain artifacts that
// Local-Chain-First requires before a GitHub P0/P1 finding may enter
// Slice-4: the Review-Report, the Triage-Entscheid, and the Delta-Status,
// each as a session/commit fact.
type ChainEvidence struct {
	ReviewReport   Artifact
	TriageDecision Artifact
	DeltaStatus    Artifact
}

// unclear reports whether any artifact is half-wired.
func (e ChainEvidence) unclear() bool {
	return e.ReviewReport.unclear() || e.TriageDecision.unclear() || e.DeltaStatus.unclear()
}

// missing names every unbelegt artifact in chain order.
func (e ChainEvidence) missing() []string {
	var out []string
	if !e.ReviewReport.bound() {
		out = append(out, "review-report")
	}
	if !e.TriageDecision.bound() {
		out = append(out, "triage-decision")
	}
	if !e.DeltaStatus.bound() {
		out = append(out, "delta-status")
	}
	return out
}

// Outcome is the fail-closed result of one entry check.
type Outcome struct {
	Decision Decision
	Effect   Effect
}

// isFindingEntry reports whether the move is a Slice-4 finding entry:
// watching or delta_review consuming poll_findings. Only finding entries
// fall under the gate; every other move passes through untouched.
func isFindingEntry(s reactionloop.State, e reactionloop.Event) bool {
	if e != reactionloop.EventPollFindings {
		return false
	}
	return s == reactionloop.StateWatching || s == reactionloop.StateDeltaReview
}

// CheckEntry rules one Slice-4 finding entry under Local-Chain-First.
// Disabled denies closed with stop+melden. Out-of-scope moves pass with
// EffectPass (Slice-4 Advance keeps ruling them, including its own
// invalid-transition denial). In-scope moves with a fully bound chain
// allow with EffectEnter; missing artifacts deny with wait+melden in
// chain order (review-report, triage-decision, delta-status); unclear
// evidence denies fail-closed with stop+melden.
func CheckEntry(enabled bool, ev ChainEvidence, s reactionloop.State, e reactionloop.Event) Outcome {
	if !enabled {
		return Outcome{
			Decision: deny(ReasonDisabled, "triage-gate policy off"),
			Effect:   EffectStop,
		}
	}
	if !isFindingEntry(s, e) {
		return Outcome{
			Decision: allowWith(ReasonPass, "no Slice-4 finding entry, gate does not rule"),
			Effect:   EffectPass,
		}
	}
	if ev.unclear() {
		return Outcome{
			Decision: deny(ReasonUnclearEvidence, "half-wired chain evidence, refusing fail-closed"),
			Effect:   EffectStop,
		}
	}
	if missing := ev.missing(); len(missing) > 0 {
		reason := ReasonWaitReviewReport
		switch missing[0] {
		case "triage-decision":
			reason = ReasonWaitTriageDecision
		case "delta-status":
			reason = ReasonWaitDeltaStatus
		}
		return Outcome{
			Decision: deny(reason, "wait for local chain: missing "+strings.Join(missing, ", ")),
			Effect:   EffectWait,
		}
	}
	return Outcome{
		Decision: allowWith(ReasonEnter, "local chain belegt: review-report="+strings.TrimSpace(ev.ReviewReport.Ref)+
			", triage-decision="+strings.TrimSpace(ev.TriageDecision.Ref)+
			", delta-status="+strings.TrimSpace(ev.DeltaStatus.Ref)),
		Effect: EffectEnter,
	}
}

// RedLineKind names an entry handling the gate must never perform.
type RedLineKind string

// Closed vocabulary of forbidden entry handlings. No entry check in
// this package accepts any of them.
const (
	RedLineSilentSkip       RedLineKind = "silent_skip"
	RedLineDoneNoEvidence   RedLineKind = "done_without_evidence"
	RedLineAutoConfirm      RedLineKind = "auto_confirm_finding"
	RedLineHumanGateBypass  RedLineKind = "human_gate_bypass" //nolint:gosec // G101 false positive: enum name of a forbidden entry handling, not a credential.
	RedLineInvalidEntryPass RedLineKind = "invalid_entry_pass"
)

// RedLines lists every forbidden entry handling for enumeration tests.
var RedLines = []RedLineKind{
	RedLineSilentSkip,
	RedLineDoneNoEvidence,
	RedLineAutoConfirm,
	RedLineHumanGateBypass,
	RedLineInvalidEntryPass,
}

// AttemptRedLine is the single choke point for forbidden entry
// handlings: it always denies with stop+melden, whether the policy is
// on or off, so no caller can route a silent skip, an unfounded done,
// an auto-confirm, or a human-gate bypass through this package.
func AttemptRedLine(kind RedLineKind) Outcome {
	return Outcome{
		Decision: deny(ReasonRedLine, "forbidden entry handling: "+string(kind)),
		Effect:   EffectStop,
	}
}

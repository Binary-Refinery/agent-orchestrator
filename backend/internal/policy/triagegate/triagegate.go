// Package triagegate implements the Triage-Gate Mini-Slice: Local-Chain-First
// as entry condition into the Slice-4 state machine (reactionloop).
//
// GitHub P0/P1 findings may only be processed by the Slice-4 automaton
// (watching/delta_review + poll_findings) when the bound local chain
// artifacts are belegt: Review-Report, Triage-Entscheid, and Delta-Status
// as session/commit facts handed in by the caller. A fact counts as
// belegt only when observed and bound to a well-formed session/commit
// reference: session ids are opaque non-empty identities, commit ids are
// immutable hex SHAs (7 to 40 hex chars). All three bound facts must
// resolve to exactly one chain — the same scope plus the same identity
// across Review-Report, Triage-Entscheid, and Delta-Status. Missing,
// unbound, non-SHA, or foreign-chain evidence denies fail-closed with
// wait+melden (EffectWait): never silently skipped, never counted as
// done, never passed through. A poll_findings event in an invalid or
// unknown state follows the same missing-evidence/wait-report invariant
// instead of passing. The gate is opt-in and default-off (AO_TRIAGE_GATE);
// disabled denies closed so no finding enters Slice-4 without an explicit
// opt-in.
//
// Red lines (enforced by design, no code path exists for them): no silent
// skip of missing evidence, no done-without-evidence, no finding
// auto-confirm, no human-gate bypass, no invalid-entry pass. This package
// only rules entry; it starts no worker, confirms nothing, and
// merges/publishes nothing. It performs no network calls, opens no
// storage, and wires into no daemon path. Callers must run CheckEntry
// before reactionloop.Advance with poll_findings and must not advance on
// Wait or Stop.
package triagegate

import (
	"os"
	"regexp"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/policy/reactionloop"
)

// RejectReason is the typed reason every refusal carries (fail-closed).
type RejectReason string

// Typed fail-closed refusal reasons for the opt-in switch, the chain
// evidence gate, invalid entries, and the red-line guard.
const (
	ReasonDisabled              RejectReason = "triagegate_disabled"
	ReasonWaitReviewReport      RejectReason = "triagegate_wait_review_report"
	ReasonWaitTriageDecision    RejectReason = "triagegate_wait_triage_decision"
	ReasonWaitDeltaStatus       RejectReason = "triagegate_wait_delta_status"
	ReasonWaitUnboundEvidence   RejectReason = "triagegate_wait_unbound_evidence"
	ReasonWaitInconsistentChain RejectReason = "triagegate_wait_inconsistent_chain"
	ReasonInvalidEntry          RejectReason = "triagegate_invalid_entry"
	ReasonRedLine               RejectReason = "triagegate_red_line"
	ReasonEnter                 RejectReason = "triagegate_chain_bound"
	ReasonPass                  RejectReason = "triagegate_scope_pass"
)

// Effect is the gate outcome for the caller: enter (GREEN), wait+melden
// (HOLD), stop+melden (RED), or pass (gate not applicable to this move).
type Effect string

// Closed effect vocabulary.
const (
	// EffectEnter (GREEN): the local chain is belegt and consistent,
	// the finding may enter the Slice-4 state machine via
	// reactionloop.Advance.
	EffectEnter Effect = "enter"
	// EffectWait (HOLD): chain evidence is missing, unbound, or
	// inconsistent, or the entry itself is invalid. Wait and meld;
	// never silently skip and never count as done.
	EffectWait Effect = "wait"
	// EffectStop (RED): disabled gate or red-line attempt. Stop and
	// meld immediately.
	EffectStop Effect = "stop"
	// EffectPass: this move carries no poll_findings, so the gate does
	// not rule. Slice-4 Advance still enforces its own evidence gates
	// (including invalid-transition denial).
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

// bindingPattern is the closed grammar for a bound session/commit fact:
// session:<session-id>/<slot>:<fact> or commit:<sha>/<slot>:<fact>, with
// no empty and no whitespace-bearing parts. Arbitrary nonblank refs
// ("r1", bare ids, URLs, half-formed pairs) never satisfy it, so an
// unbound claim can never read as a bound fact.
var bindingPattern = regexp.MustCompile(`^(session|commit):([^/\s]+)/([^:\s]+):(\S+)$`)

// shaPattern admits only immutable hex SHAs as commit identities: 7 to
// 40 hex chars (abbreviated to full). Placeholders such as "c-1" or
// non-hex strings never qualify, so no mutable label can stand in for a
// commit fact.
var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// parseBinding validates a trimmed ref against the closed grammar and
// returns its scope (session|commit) plus chain id (session id or commit
// SHA). The slot and fact parts are grammar-checked but carry no routing
// meaning, so they are not returned. ok is false for every ref outside
// the closed grammar, including commit refs without a hex SHA.
func parseBinding(ref string) (scope, id string, ok bool) {
	m := bindingPattern.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return "", "", false
	}
	if m[1] == "commit" && !shaPattern.MatchString(m[2]) {
		return "", "", false
	}
	return m[1], m[2], true
}

// Artifact is one bound chain artifact: a session/commit fact handed in
// by the caller. Present marks the fact as observed; Ref names the bound
// session/commit fact and must satisfy the closed binding grammar. A fact
// counts as belegt only when observed AND well-formed bound; anything
// else is unbound evidence and denies fail-closed with wait+melden, so a
// half-wired chain can never read as complete.
type Artifact struct {
	Present bool
	Ref     string
}

// bound reports whether the artifact is observed and bound to a
// well-formed session/commit fact.
func (a Artifact) bound() bool {
	if !a.Present {
		return false
	}
	_, _, ok := parseBinding(a.Ref)
	return ok
}

// malformed reports unbound evidence: observed-but-ill-bound or
// bound-claimed-but-unobserved. Both deny fail-closed with wait+melden.
func (a Artifact) malformed() bool {
	_, _, ok := parseBinding(a.Ref)
	boundClaim := strings.TrimSpace(a.Ref) != ""
	if a.Present && !ok {
		return true
	}
	return !a.Present && boundClaim
}

// ChainEvidence carries the three bound local chain artifacts that
// Local-Chain-First requires before a GitHub P0/P1 finding may enter
// Slice-4: the Review-Report, the Triage-Entscheid, and the Delta-Status,
// each as a session/commit fact.
type ChainEvidence struct {
	ReviewReport   Artifact
	TriageDecision Artifact
	DeltaStatus    Artifact
}

// malformed names every unbound artifact in chain order.
func (e ChainEvidence) malformed() []string {
	var out []string
	if e.ReviewReport.malformed() {
		out = append(out, "review-report")
	}
	if e.TriageDecision.malformed() {
		out = append(out, "triage-decision")
	}
	if e.DeltaStatus.malformed() {
		out = append(out, "delta-status")
	}
	return out
}

// missing names every unbelegt artifact in chain order. Unbelegt covers
// absent facts; malformed facts are reported separately and bind first.
func (e ChainEvidence) missing() []string {
	var out []string
	if !e.ReviewReport.bound() && !e.ReviewReport.malformed() {
		out = append(out, "review-report")
	}
	if !e.TriageDecision.bound() && !e.TriageDecision.malformed() {
		out = append(out, "triage-decision")
	}
	if !e.DeltaStatus.bound() && !e.DeltaStatus.malformed() {
		out = append(out, "delta-status")
	}
	return out
}

// consistent reports whether all bound artifacts resolve to exactly one
// chain: the same scope plus the same identity across Review-Report,
// Triage-Entscheid, and Delta-Status. Scopes and identities are never
// tracked separately, so an unconnected session/commit mixture — one
// session identity beside an unrelated commit identity — reads as
// foreign and denies. Only one unanimous triple authorizes an entry.
func (e ChainEvidence) consistent() bool {
	var scope, id string
	first := true
	for _, a := range []Artifact{e.ReviewReport, e.TriageDecision, e.DeltaStatus} {
		if !a.bound() {
			continue
		}
		s, i, ok := parseBinding(a.Ref)
		if !ok {
			return false
		}
		if first {
			scope, id, first = s, i, false
			continue
		}
		if s != scope || i != id {
			return false
		}
	}
	return true
}

// Outcome is the fail-closed result of one entry check.
type Outcome struct {
	Decision Decision
	Effect   Effect
}

// validEntryState reports the Slice-4 states that may consume a
// poll_findings event: watching and delta_review. Every other state with
// poll_findings is an invalid entry and follows the
// missing-evidence/wait-report invariant instead of passing.
func validEntryState(s reactionloop.State) bool {
	return s == reactionloop.StateWatching || s == reactionloop.StateDeltaReview
}

// CheckEntry rules one move under Local-Chain-First. Disabled denies
// closed with stop+melden. Moves without poll_findings pass with
// EffectPass (Slice-4 Advance keeps ruling them, including its own
// invalid-transition denial). A poll_findings event in an invalid or
// unknown state denies with wait+melden under ReasonInvalidEntry: never
// passed, never entered, never counted as done. Valid finding entries
// with a fully bound and consistent chain allow with EffectEnter;
// malformed refs deny with wait+melden under ReasonWaitUnboundEvidence;
// missing artifacts deny with wait+melden in chain order
// (review-report, triage-decision, delta-status); bound-but-foreign
// chains deny with wait+melden under ReasonWaitInconsistentChain.
func CheckEntry(enabled bool, ev ChainEvidence, s reactionloop.State, e reactionloop.Event) Outcome {
	if !enabled {
		return Outcome{
			Decision: deny(ReasonDisabled, "triage-gate policy off"),
			Effect:   EffectStop,
		}
	}
	if e != reactionloop.EventPollFindings {
		return Outcome{
			Decision: allowWith(ReasonPass, "no poll_findings carried, gate does not rule"),
			Effect:   EffectPass,
		}
	}
	if !validEntryState(s) {
		return Outcome{
			Decision: deny(ReasonInvalidEntry, "wait for a valid Slice-4 finding entry: state "+string(s)+" consumes no poll_findings"),
			Effect:   EffectWait,
		}
	}
	if malformed := ev.malformed(); len(malformed) > 0 {
		return Outcome{
			Decision: deny(ReasonWaitUnboundEvidence, "wait for bound chain evidence: unbound "+strings.Join(malformed, ", ")),
			Effect:   EffectWait,
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
	if !ev.consistent() {
		return Outcome{
			Decision: deny(ReasonWaitInconsistentChain, "wait for one chain: bound facts name foreign sessions or commits"),
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
// an auto-confirm, a human-gate bypass, or an invalid-entry pass
// through this package.
func AttemptRedLine(kind RedLineKind) Outcome {
	return Outcome{
		Decision: deny(ReasonRedLine, "forbidden entry handling: "+string(kind)),
		Effect:   EffectStop,
	}
}

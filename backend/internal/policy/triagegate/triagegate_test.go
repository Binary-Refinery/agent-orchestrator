package triagegate

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/policy/reactionloop"
)

func boundEvidence() ChainEvidence {
	return ChainEvidence{
		ReviewReport:   Artifact{Present: true, Ref: "session:ao-source-48/report:r1"},
		TriageDecision: Artifact{Present: true, Ref: "session:ao-source-48/decision:d1"},
		DeltaStatus:    Artifact{Present: true, Ref: "commit:4a6febc/delta:s1"},
	}
}

func TestTriageGateDisabledByDefault(t *testing.T) {
	if EnabledFromEnv("") {
		t.Fatal("empty env must stay disabled")
	}
	for _, v := range []string{"0", "false", "off", "no", "random"} {
		if EnabledFromEnv(v) {
			t.Fatalf("env %q must stay disabled", v)
		}
	}
	for _, v := range []string{"1", "true", "yes", "on", " TRUE "} {
		if !EnabledFromEnv(v) {
			t.Fatalf("env %q must enable", v)
		}
	}
}

func TestDisabledGateStopsClosed(t *testing.T) {
	for _, tc := range []struct {
		state reactionloop.State
		event reactionloop.Event
	}{
		{reactionloop.StateWatching, reactionloop.EventPollFindings},
		{reactionloop.StateDeltaReview, reactionloop.EventPollFindings},
		{reactionloop.StateWatching, reactionloop.EventPollGreen},
	} {
		out := CheckEntry(false, boundEvidence(), tc.state, tc.event)
		if out.Decision.Allow || out.Decision.Reason != ReasonDisabled {
			t.Fatalf("%s+%s disabled = %+v, want disabled deny", tc.state, tc.event, out.Decision)
		}
		if out.Effect != EffectStop {
			t.Fatalf("%s+%s disabled effect = %q, want stop", tc.state, tc.event, out.Effect)
		}
	}
}

func TestBoundChainEnters(t *testing.T) {
	for _, s := range []reactionloop.State{reactionloop.StateWatching, reactionloop.StateDeltaReview} {
		out := CheckEntry(true, boundEvidence(), s, reactionloop.EventPollFindings)
		if !out.Decision.Allow || out.Decision.Reason != ReasonEnter {
			t.Fatalf("%s entry = %+v, want chain_bound allow", s, out.Decision)
		}
		if out.Effect != EffectEnter {
			t.Fatalf("%s entry effect = %q, want enter", s, out.Effect)
		}
		for _, want := range []string{"review-report=", "triage-decision=", "delta-status="} {
			if !strings.Contains(out.Decision.Detail, want) {
				t.Fatalf("%s entry detail = %q, want bound ref %q", s, out.Decision.Detail, want)
			}
		}
	}
}

func TestMissingArtifactWaitsInChainOrder(t *testing.T) {
	full := boundEvidence()
	empty := Artifact{}
	for _, tc := range []struct {
		name   string
		ev     ChainEvidence
		reason RejectReason
	}{
		{"no review-report", ChainEvidence{TriageDecision: full.TriageDecision, DeltaStatus: full.DeltaStatus}, ReasonWaitReviewReport},
		{"no triage-decision", ChainEvidence{ReviewReport: full.ReviewReport, DeltaStatus: full.DeltaStatus}, ReasonWaitTriageDecision},
		{"no delta-status", ChainEvidence{ReviewReport: full.ReviewReport, TriageDecision: full.TriageDecision}, ReasonWaitDeltaStatus},
		{"nothing bound", ChainEvidence{}, ReasonWaitReviewReport},
		{"review+delta without triage", ChainEvidence{ReviewReport: full.ReviewReport, DeltaStatus: full.DeltaStatus}, ReasonWaitTriageDecision},
		{"only review", ChainEvidence{ReviewReport: full.ReviewReport}, ReasonWaitTriageDecision},
		{"only delta", ChainEvidence{DeltaStatus: full.DeltaStatus}, ReasonWaitReviewReport},
		{"empty artifacts", ChainEvidence{ReviewReport: empty, TriageDecision: empty, DeltaStatus: empty}, ReasonWaitReviewReport},
	} {
		out := CheckEntry(true, tc.ev, reactionloop.StateWatching, reactionloop.EventPollFindings)
		if out.Decision.Allow || out.Decision.Reason != tc.reason {
			t.Fatalf("%s = %+v, want %q deny", tc.name, out.Decision, tc.reason)
		}
		if out.Effect != EffectWait {
			t.Fatalf("%s effect = %q, want wait", tc.name, out.Effect)
		}
		if strings.TrimSpace(out.Decision.Detail) == "" {
			t.Fatalf("%s carries no Meldung, wait must meld", tc.name)
		}
	}
}

func TestUnclearEvidenceStopsRed(t *testing.T) {
	full := boundEvidence()
	for _, tc := range []struct {
		name string
		ev   ChainEvidence
	}{
		{"report observed but unbound", ChainEvidence{ReviewReport: Artifact{Present: true}, TriageDecision: full.TriageDecision, DeltaStatus: full.DeltaStatus}},
		{"report bound but unobserved", ChainEvidence{ReviewReport: Artifact{Ref: "session:x/report:r"}, TriageDecision: full.TriageDecision, DeltaStatus: full.DeltaStatus}},
		{"decision whitespace ref", ChainEvidence{ReviewReport: full.ReviewReport, TriageDecision: Artifact{Present: true, Ref: "  "}, DeltaStatus: full.DeltaStatus}},
		{"delta unobserved ref", ChainEvidence{ReviewReport: full.ReviewReport, TriageDecision: full.TriageDecision, DeltaStatus: Artifact{Ref: "commit:x/delta:s"}}},
	} {
		out := CheckEntry(true, tc.ev, reactionloop.StateWatching, reactionloop.EventPollFindings)
		if out.Decision.Allow || out.Decision.Reason != ReasonUnclearEvidence {
			t.Fatalf("%s = %+v, want unclear_evidence deny", tc.name, out.Decision)
		}
		if out.Effect != EffectStop {
			t.Fatalf("%s effect = %q, want stop", tc.name, out.Effect)
		}
	}
}

func TestOutOfScopePassesWithoutRuling(t *testing.T) {
	for _, tc := range []struct {
		state reactionloop.State
		event reactionloop.Event
	}{
		{reactionloop.StateWatching, reactionloop.EventPollGreen},
		{reactionloop.StateWatching, reactionloop.EventPollInconclusive},
		{reactionloop.StateTriageProposed, reactionloop.EventHumanConfirmTriage},
		{reactionloop.StateTriageProposed, reactionloop.EventHumanRejectTriage},
		{reactionloop.StateFixAuthorized, reactionloop.EventFixCompleted},
		{reactionloop.StateDeltaReview, reactionloop.EventPollGreen},
		{reactionloop.StateCloseoutDrafted, reactionloop.EventPollFindings},
		{reactionloop.StateCloseoutDrafted, reactionloop.EventPollGreen},
		{reactionloop.State("unknown"), reactionloop.EventPollFindings},
		{reactionloop.StateWatching, reactionloop.Event("unknown")},
	} {
		out := CheckEntry(true, ChainEvidence{}, tc.state, tc.event)
		if !out.Decision.Allow || out.Decision.Reason != ReasonPass {
			t.Fatalf("%s+%s = %+v, want scope_pass allow", tc.state, tc.event, out.Decision)
		}
		if out.Effect != EffectPass {
			t.Fatalf("%s+%s effect = %q, want pass", tc.state, tc.event, out.Effect)
		}
	}
}

func TestRedLinesAlwaysDenied(t *testing.T) {
	for _, kind := range RedLines {
		out := AttemptRedLine(kind)
		if out.Decision.Allow || out.Decision.Reason != ReasonRedLine {
			t.Fatalf("red line %q = %+v, want red_line deny", kind, out.Decision)
		}
		if out.Effect != EffectStop {
			t.Fatalf("red line %q effect = %q, want stop", kind, out.Effect)
		}
	}
	if len(RedLines) != 5 {
		t.Fatalf("want 5 enumerated red lines, got %d", len(RedLines))
	}
}

func TestWaitAndStopAlwaysCarryMeldung(t *testing.T) {
	inputs := []Outcome{
		CheckEntry(false, boundEvidence(), reactionloop.StateWatching, reactionloop.EventPollFindings),
		CheckEntry(true, ChainEvidence{}, reactionloop.StateWatching, reactionloop.EventPollFindings),
		CheckEntry(true, ChainEvidence{ReviewReport: Artifact{Present: true}}, reactionloop.StateDeltaReview, reactionloop.EventPollFindings),
		AttemptRedLine(RedLineSilentSkip),
		AttemptRedLine(RedLineDoneNoEvidence),
	}
	for i, out := range inputs {
		if out.Decision.Allow {
			t.Fatalf("input %d allowed, want deny: %+v", i, out.Decision)
		}
		if strings.TrimSpace(out.Decision.Detail) == "" {
			t.Fatalf("input %d deny carries no Meldung: %+v", i, out.Decision)
		}
		if strings.TrimSpace(string(out.Decision.Reason)) == "" {
			t.Fatalf("input %d deny carries no typed reason", i)
		}
	}
}

func TestNeverSilentSkipOrDone(t *testing.T) {
	artifacts := []Artifact{
		{},
		{Present: true},
		{Ref: "session:x/fakt:f"},
		{Present: true, Ref: "session:x/fakt:f"},
		{Present: true, Ref: "   "},
	}
	states := []reactionloop.State{
		reactionloop.StateWatching,
		reactionloop.StateDeltaReview,
		reactionloop.StateTriageProposed,
		reactionloop.StateFixAuthorized,
		reactionloop.StateCloseoutDrafted,
	}
	events := []reactionloop.Event{
		reactionloop.EventPollFindings,
		reactionloop.EventPollGreen,
		reactionloop.EventPollInconclusive,
		reactionloop.EventHumanConfirmTriage,
		reactionloop.EventFixCompleted,
	}
	for _, a := range artifacts {
		for _, b := range artifacts {
			for _, c := range artifacts {
				ev := ChainEvidence{ReviewReport: a, TriageDecision: b, DeltaStatus: c}
				complete := a.bound() && b.bound() && c.bound()
				for _, s := range states {
					for _, e := range events {
						out := CheckEntry(true, ev, s, e)
						inScope := (s == reactionloop.StateWatching || s == reactionloop.StateDeltaReview) &&
							e == reactionloop.EventPollFindings
						if out.Effect == EffectEnter && (!inScope || !complete) {
							t.Fatalf("enter without bound in-scope chain: ev=%+v %s+%s", ev, s, e)
						}
						if out.Decision.Allow && out.Effect != EffectEnter && out.Effect != EffectPass {
							t.Fatalf("allow without enter/pass: ev=%+v %s+%s = %+v", ev, s, e, out)
						}
						if !out.Decision.Allow && out.Effect != EffectWait && out.Effect != EffectStop {
							t.Fatalf("deny without wait/stop: ev=%+v %s+%s = %+v", ev, s, e, out)
						}
					}
				}
			}
		}
	}
}

package triagegate

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/policy/reactionloop"
)

// fullSHA is a real immutable commit SHA from this repository's history.
const fullSHA = "4a6febc7c89994ccd5a423f9c7824da5b4fc65b0"

func boundEvidence() ChainEvidence {
	return ChainEvidence{
		ReviewReport:   Artifact{Present: true, Ref: "session:ao-source-48/report:r1"},
		TriageDecision: Artifact{Present: true, Ref: "session:ao-source-48/decision:d1"},
		DeltaStatus:    Artifact{Present: true, Ref: "session:ao-source-48/delta:s1"},
	}
}

func boundCommitEvidence() ChainEvidence {
	return ChainEvidence{
		ReviewReport:   Artifact{Present: true, Ref: "commit:" + fullSHA + "/report:r1"},
		TriageDecision: Artifact{Present: true, Ref: "commit:" + fullSHA + "/decision:d1"},
		DeltaStatus:    Artifact{Present: true, Ref: "commit:" + fullSHA + "/delta:s1"},
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
		{reactionloop.StateTriageProposed, reactionloop.EventPollFindings},
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
	chains := []ChainEvidence{boundEvidence(), boundCommitEvidence()}
	for _, s := range []reactionloop.State{reactionloop.StateWatching, reactionloop.StateDeltaReview} {
		for ci, ev := range chains {
			out := CheckEntry(true, ev, s, reactionloop.EventPollFindings)
			if !out.Decision.Allow || out.Decision.Reason != ReasonEnter {
				t.Fatalf("chain %d %s entry = %+v, want chain_bound allow", ci, s, out.Decision)
			}
			if out.Effect != EffectEnter {
				t.Fatalf("chain %d %s entry effect = %q, want enter", ci, s, out.Effect)
			}
			for _, want := range []string{"review-report=", "triage-decision=", "delta-status="} {
				if !strings.Contains(out.Decision.Detail, want) {
					t.Fatalf("chain %d %s entry detail = %q, want bound ref %q", ci, s, out.Decision.Detail, want)
				}
			}
		}
	}
}

func TestArbitraryRefsNeverBind(t *testing.T) {
	for _, ref := range []string{
		"r1",
		"report-1",
		"ao-source-48/report:r1",
		"session:/report:r1",
		"session:ao-source-48/:r1",
		"session:ao-source-48/report:",
		"session:ao-source-48/report: ",
		"session:ao source/report:r1",
		"commit:4a6febc",
		"commit:4a6febc/delta:",
		"commit:c-1/delta:s1",
		"commit:xyz/delta:s1",
		"commit:12345/delta:s1",
		"commit:4a6febc!/delta:s1",
		"commit:4a6febc7c89994ccd5a423f9c7824da5b4fc65b00/delta:s1",
		"http://host/session:ao-source-48/report:r1",
		"SESSION:ao-source-48/report:r1",
		"session:ao-source-48:report:r1",
		"",
		"   ",
	} {
		a := Artifact{Present: true, Ref: ref}
		if a.bound() {
			t.Fatalf("ref %q binds, want unbound", ref)
		}
		if !a.malformed() && strings.TrimSpace(ref) != "" {
			t.Fatalf("ref %q with Present is neither bound nor malformed", ref)
		}
		ev := ChainEvidence{
			ReviewReport:   a,
			TriageDecision: boundEvidence().TriageDecision,
			DeltaStatus:    boundEvidence().DeltaStatus,
		}
		out := CheckEntry(true, ev, reactionloop.StateWatching, reactionloop.EventPollFindings)
		if out.Decision.Allow || out.Decision.Reason != ReasonWaitUnboundEvidence {
			t.Fatalf("ref %q = %+v, want wait_unbound_evidence deny", ref, out.Decision)
		}
		if out.Effect != EffectWait {
			t.Fatalf("ref %q effect = %q, want wait", ref, out.Effect)
		}
		if strings.TrimSpace(out.Decision.Detail) == "" {
			t.Fatalf("ref %q carries no Meldung, wait must meld", ref)
		}
	}
	unobserved := Artifact{Ref: "session:ao-source-48/report:r1"}
	if unobserved.bound() || !unobserved.malformed() {
		t.Fatalf("unobserved ref claim = bound %v malformed %v, want unbound malformed",
			unobserved.bound(), unobserved.malformed())
	}
	for _, ref := range []string{
		"session:ao-source-48/report:r1",
		"commit:4a6febc/delta:s1",
		"commit:" + fullSHA + "/delta:s1",
		"commit:ABCDEF1/delta:s1",
		"session:s-1/a:b",
	} {
		a := Artifact{Present: true, Ref: ref}
		if !a.bound() || a.malformed() {
			t.Fatalf("ref %q = bound %v malformed %v, want bound", ref, a.bound(), a.malformed())
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

func TestInconsistentChainWaitsReport(t *testing.T) {
	full := boundEvidence()
	foreignSession := Artifact{Present: true, Ref: "session:ao-source-49/report:r9"}
	otherSHA := Artifact{Present: true, Ref: "commit:bbbbbbb/delta:s9"}
	sessionDelta := Artifact{Present: true, Ref: "commit:" + fullSHA + "/delta:s1"}
	for _, tc := range []struct {
		name string
		ev   ChainEvidence
	}{
		{"foreign triage session", ChainEvidence{ReviewReport: full.ReviewReport, TriageDecision: foreignSession, DeltaStatus: full.DeltaStatus}},
		{"split commit shas", ChainEvidence{ReviewReport: Artifact{Present: true, Ref: "commit:aaaaaaa/report:r9"}, TriageDecision: Artifact{Present: true, Ref: "commit:bbbbbbb/decision:d9"}, DeltaStatus: Artifact{Present: true, Ref: "commit:aaaaaaa/delta:s9"}}},
		{"unconnected session/commit mix", ChainEvidence{ReviewReport: full.ReviewReport, TriageDecision: full.TriageDecision, DeltaStatus: sessionDelta}},
		{"commit beside foreign session", ChainEvidence{ReviewReport: full.ReviewReport, TriageDecision: foreignSession, DeltaStatus: otherSHA}},
	} {
		out := CheckEntry(true, tc.ev, reactionloop.StateWatching, reactionloop.EventPollFindings)
		if out.Decision.Allow || out.Decision.Reason != ReasonWaitInconsistentChain {
			t.Fatalf("%s = %+v, want wait_inconsistent_chain deny", tc.name, out.Decision)
		}
		if out.Effect != EffectWait {
			t.Fatalf("%s effect = %q, want wait", tc.name, out.Effect)
		}
		if strings.TrimSpace(out.Decision.Detail) == "" {
			t.Fatalf("%s carries no Meldung, wait must meld", tc.name)
		}
	}
	if !boundEvidence().consistent() || !boundCommitEvidence().consistent() {
		t.Fatal("unanimous session and commit triples must each read as one chain")
	}
}

func TestInvalidFindingEntryWaitsReport(t *testing.T) {
	for _, s := range []reactionloop.State{
		reactionloop.StateTriageProposed,
		reactionloop.StateFixAuthorized,
		reactionloop.StateCloseoutDrafted,
		reactionloop.State("unknown"),
		reactionloop.State(""),
	} {
		for _, ev := range []ChainEvidence{boundEvidence(), boundCommitEvidence(), ChainEvidence{}} {
			out := CheckEntry(true, ev, s, reactionloop.EventPollFindings)
			if out.Decision.Allow || out.Decision.Reason != ReasonInvalidEntry {
				t.Fatalf("%s+poll_findings = %+v, want invalid_entry deny", s, out.Decision)
			}
			if out.Effect != EffectWait {
				t.Fatalf("%s+poll_findings effect = %q, want wait", s, out.Effect)
			}
			if strings.TrimSpace(out.Decision.Detail) == "" {
				t.Fatalf("%s+poll_findings carries no Meldung, wait must meld", s)
			}
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
		{reactionloop.StateCloseoutDrafted, reactionloop.EventPollGreen},
		{reactionloop.State("unknown"), reactionloop.EventPollGreen},
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
		CheckEntry(true, ChainEvidence{ReviewReport: Artifact{Present: true, Ref: "r1"}}, reactionloop.StateDeltaReview, reactionloop.EventPollFindings),
		CheckEntry(true, ChainEvidence{ReviewReport: Artifact{Present: true, Ref: "commit:c-1/delta:s1"}}, reactionloop.StateWatching, reactionloop.EventPollFindings),
		CheckEntry(true, boundEvidence(), reactionloop.StateTriageProposed, reactionloop.EventPollFindings),
		AttemptRedLine(RedLineSilentSkip),
		AttemptRedLine(RedLineDoneNoEvidence),
		AttemptRedLine(RedLineInvalidEntryPass),
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
	goodA := Artifact{Present: true, Ref: "session:s-1/report:r1"}
	goodB := Artifact{Present: true, Ref: "session:s-1/decision:d1"}
	goodC := Artifact{Present: true, Ref: "session:s-1/delta:s1"}
	commitC := Artifact{Present: true, Ref: "commit:" + fullSHA + "/delta:s1"}
	foreign := Artifact{Present: true, Ref: "session:s-2/decision:d9"}
	loose := Artifact{Present: true, Ref: "c-1"}
	combos := []ChainEvidence{
		{},
		{ReviewReport: goodA},
		{ReviewReport: goodA, TriageDecision: goodB, DeltaStatus: goodC},
		{ReviewReport: goodA, TriageDecision: goodB, DeltaStatus: commitC},
		{ReviewReport: goodA, TriageDecision: foreign, DeltaStatus: goodC},
		{ReviewReport: loose, TriageDecision: goodB, DeltaStatus: goodC},
		{ReviewReport: goodA, TriageDecision: goodB, DeltaStatus: Artifact{Present: true, Ref: "commit:c-1/delta:s1"}},
	}
	entryStates := []reactionloop.State{reactionloop.StateWatching, reactionloop.StateDeltaReview}
	otherStates := []reactionloop.State{
		reactionloop.StateTriageProposed,
		reactionloop.StateFixAuthorized,
		reactionloop.StateCloseoutDrafted,
		reactionloop.State("unknown"),
	}
	nonFinding := []reactionloop.Event{
		reactionloop.EventPollGreen,
		reactionloop.EventPollInconclusive,
		reactionloop.EventHumanConfirmTriage,
		reactionloop.EventFixCompleted,
	}
	for ci, ev := range combos {
		enterOK := ev.malformed() == nil &&
			ev.missing() == nil &&
			ev.consistent()
		for _, s := range entryStates {
			out := CheckEntry(true, ev, s, reactionloop.EventPollFindings)
			if enterOK && (out.Effect != EffectEnter || !out.Decision.Allow) {
				t.Fatalf("combo %d %s must enter: %+v", ci, s, out)
			}
			if !enterOK && (out.Decision.Allow || out.Effect != EffectWait) {
				t.Fatalf("combo %d %s must wait+melden, got %+v", ci, s, out)
			}
		}
		for _, s := range otherStates {
			out := CheckEntry(true, ev, s, reactionloop.EventPollFindings)
			if out.Decision.Allow || out.Effect != EffectWait {
				t.Fatalf("combo %d %s+poll_findings must wait+melden, got %+v", ci, s, out)
			}
		}
		for _, s := range append(append([]reactionloop.State{}, entryStates...), otherStates...) {
			for _, e := range nonFinding {
				out := CheckEntry(true, ev, s, e)
				if !out.Decision.Allow || out.Effect != EffectPass {
					t.Fatalf("combo %d %s+%s without findings must pass, got %+v", ci, s, e, out)
				}
			}
		}
	}
}

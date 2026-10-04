package reactionloop

import (
	"testing"
)

func findingsOutcome() PollOutcome {
	return SummarizePoll([]ReviewComment{
		{ID: "c1", IsCodex: true, Body: "P0: race on shutdown"},
	}, []CheckRun{{Name: "unit", Status: "completed", Conclusion: "success"}})
}

func greenOutcome() PollOutcome {
	return SummarizePoll(
		[]ReviewComment{{ID: "c1", IsCodex: true, Body: "Clean, no markers."}},
		[]CheckRun{{Name: "unit", Status: "completed", Conclusion: "success"}},
	)
}

func TestFindingsLeadToTriageProposal(t *testing.T) {
	step := Advance(true, StateWatching, EventPollFindings, false, findingsOutcome())
	if !step.Decision.Allow {
		t.Fatalf("triage step denied: %+v", step.Decision)
	}
	if step.Next != StateTriageProposed {
		t.Fatalf("next = %q, want triage_proposed", step.Next)
	}
	if step.Proposal == nil || step.Proposal.P0 != 1 || len(step.Proposal.Findings) != 1 {
		t.Fatalf("proposal = %+v, want one P0 finding", step.Proposal)
	}
}

func TestGreenLeadsToCloseoutDraftOnly(t *testing.T) {
	step := Advance(true, StateWatching, EventPollGreen, false, greenOutcome())
	if !step.Decision.Allow {
		t.Fatalf("closeout step denied: %+v", step.Decision)
	}
	if step.Next != StateCloseoutDrafted {
		t.Fatalf("next = %q, want closeout_drafted", step.Next)
	}
	if step.Closeout == nil || step.Proposal != nil {
		t.Fatalf("green step must draft closeout, not triage: %+v", step)
	}
}

func TestTriageNeedsHumanConfirmation(t *testing.T) {
	denied := Advance(true, StateTriageProposed, EventHumanConfirmTriage, false, PollOutcome{})
	if denied.Decision.Allow || denied.Decision.Reason != ReasonNeedsHuman {
		t.Fatalf("unconfirmed triage = %+v, want needs_human deny", denied.Decision)
	}
	if denied.Next != StateTriageProposed {
		t.Fatalf("denied confirm must hold triage, got %q", denied.Next)
	}
	allowed := Advance(true, StateTriageProposed, EventHumanConfirmTriage, true, PollOutcome{})
	if !allowed.Decision.Allow || allowed.Next != StateFixAuthorized {
		t.Fatalf("confirmed triage = %+v, want fix_authorized", allowed)
	}
	rejected := Advance(true, StateTriageProposed, EventHumanRejectTriage, true, PollOutcome{})
	if !rejected.Decision.Allow || rejected.Next != StateWatching {
		t.Fatalf("rejected triage = %+v, want watching", rejected)
	}
}

// S4-P1-1: rejecting a triage proposal is a human decision too. Without
// the human flag the machine must deny with ReasonNeedsHuman and hold
// the triage state instead of silently dropping the proposal.
func TestRejectNeedsHumanConfirmation(t *testing.T) {
	denied := Advance(true, StateTriageProposed, EventHumanRejectTriage, false, PollOutcome{})
	if denied.Decision.Allow || denied.Decision.Reason != ReasonNeedsHuman {
		t.Fatalf("unconfirmed reject = %+v, want needs_human deny", denied.Decision)
	}
	if denied.Next != StateTriageProposed {
		t.Fatalf("denied reject must hold triage, got %q", denied.Next)
	}
}

func TestFixThenDeltaReview(t *testing.T) {
	done := Advance(true, StateFixAuthorized, EventFixCompleted, false, PollOutcome{})
	if !done.Decision.Allow || done.Next != StateDeltaReview {
		t.Fatalf("fix completed = %+v, want delta_review", done)
	}
	back := Advance(true, StateDeltaReview, EventPollFindings, false, findingsOutcome())
	if !back.Decision.Allow || back.Next != StateTriageProposed {
		t.Fatalf("delta findings = %+v, want triage_proposed", back)
	}
	clean := Advance(true, StateDeltaReview, EventPollGreen, false, greenOutcome())
	if !clean.Decision.Allow || clean.Next != StateCloseoutDrafted {
		t.Fatalf("delta green = %+v, want closeout_drafted", clean)
	}
}

func TestInvalidTransitionsDenied(t *testing.T) {
	for _, tc := range []struct {
		state State
		event Event
	}{
		{StateWatching, EventHumanConfirmTriage},
		{StateWatching, EventFixCompleted},
		{StateTriageProposed, EventPollGreen},
		{StateFixAuthorized, EventPollFindings},
		{StateFixAuthorized, EventHumanConfirmTriage},
	} {
		step := Advance(true, tc.state, tc.event, true, PollOutcome{})
		if step.Decision.Allow || step.Decision.Reason != ReasonInvalidStep {
			t.Fatalf("%s+%s = %+v, want invalid deny", tc.state, tc.event, step.Decision)
		}
		if step.Next != tc.state {
			t.Fatalf("%s+%s moved to %q, want hold", tc.state, tc.event, step.Next)
		}
	}
}

func TestCloseoutIsTerminal(t *testing.T) {
	for _, e := range []Event{EventPollFindings, EventPollGreen, EventPollInconclusive, EventFixCompleted} {
		step := Advance(true, StateCloseoutDrafted, e, true, findingsOutcome())
		if step.Decision.Allow || step.Decision.Reason != ReasonTerminal {
			t.Fatalf("closeout+%s = %+v, want terminal deny", e, step.Decision)
		}
	}
}

func TestDisabledMachineHolds(t *testing.T) {
	step := Advance(false, StateWatching, EventPollFindings, true, findingsOutcome())
	if step.Decision.Allow || step.Decision.Reason != ReasonDisabled {
		t.Fatalf("disabled machine = %+v, want disabled deny", step.Decision)
	}
	if step.Next != StateWatching {
		t.Fatalf("disabled machine moved to %q, want hold", step.Next)
	}
}

func TestNoAutoMergeOrAutoFixPath(t *testing.T) {
	states := []State{StateWatching, StateTriageProposed, StateFixAuthorized, StateDeltaReview, StateCloseoutDrafted}
	events := []Event{EventPollFindings, EventPollGreen, EventPollInconclusive, EventHumanConfirmTriage, EventHumanRejectTriage, EventFixCompleted}
	for _, s := range states {
		for _, e := range events {
			step := Advance(true, s, e, true, findingsOutcome())
			if step.Next == StateCloseoutDrafted && s == StateTriageProposed {
				t.Fatalf("triage must never jump straight to closeout: %s+%s", s, e)
			}
		}
	}
	// Merge and publish exist nowhere in the event vocabulary: the only
	// automation surface is AttemptRedLine, which always denies.
	for _, kind := range RedLines {
		if d := AttemptRedLine(kind); d.Allow {
			t.Fatalf("red line %q allowed", kind)
		}
	}
}

func TestInconclusivePollHolds(t *testing.T) {
	pending := SummarizePoll(nil, []CheckRun{{Name: "e2e", Status: "in_progress"}})
	if pending.AllGreen() {
		t.Fatal("pending checks must not read as green")
	}
	step := Advance(true, StateWatching, EventPollInconclusive, false, pending)
	if !step.Decision.Allow || step.Next != StateWatching {
		t.Fatalf("inconclusive poll = %+v, want hold in watching", step)
	}
}

// S4-P1-2: empty evidence must never close out. A green event over a poll
// that observed no check run denies with ReasonNoEvidence and holds the
// state instead of drafting closeout.
func TestGreenWithoutEvidenceDenied(t *testing.T) {
	for _, s := range []State{StateWatching, StateDeltaReview} {
		empty := SummarizePoll(nil, nil)
		step := Advance(true, s, EventPollGreen, false, empty)
		if step.Decision.Allow || step.Decision.Reason != ReasonNoEvidence {
			t.Fatalf("%s+green without evidence = %+v, want no_evidence deny", s, step.Decision)
		}
		if step.Next != s {
			t.Fatalf("denied green must hold %q, got %q", s, step.Next)
		}
		if step.Closeout != nil {
			t.Fatalf("%s+green without evidence drafted closeout: %+v", s, step.Closeout)
		}
	}
}

// S4-D2: one-sided evidence must never close out either. Green checks
// without any observed review comments deny with ReasonNoEvidence and
// hold, from both polling states.
func TestGreenWithoutReviewEvidenceDenied(t *testing.T) {
	checks := []CheckRun{{Name: "unit", Status: "completed", Conclusion: "success"}}
	for _, s := range []State{StateWatching, StateDeltaReview} {
		out := SummarizePoll(nil, checks)
		if out.AllGreen() {
			t.Fatalf("%s: checks-only poll reads green: %+v", s, out)
		}
		step := Advance(true, s, EventPollGreen, false, out)
		if step.Decision.Allow || step.Decision.Reason != ReasonNoEvidence {
			t.Fatalf("%s+green without review = %+v, want no_evidence deny", s, step.Decision)
		}
		if step.Next != s || step.Closeout != nil {
			t.Fatalf("%s+green without review moved or drafted: %+v", s, step)
		}
	}
}

// S4-D1: a substantive "P1: no timeout on requests" verdict stays
// blocking: it proposes triage on a findings event and refuses closeout
// on a green event, from both polling states.
func TestSubstantiveVerdictBlocksCloseout(t *testing.T) {
	comments := []ReviewComment{{ID: "c1", IsCodex: true, Body: "P1: no timeout on requests"}}
	checks := []CheckRun{{Name: "unit", Status: "completed", Conclusion: "success"}}
	out := SummarizePoll(comments, checks)
	if !out.HasBlocking() {
		t.Fatal("substantive P1 verdict must stay blocking")
	}
	triage := Advance(true, StateWatching, EventPollFindings, false, out)
	if !triage.Decision.Allow || triage.Next != StateTriageProposed {
		t.Fatalf("P1 findings = %+v, want triage_proposed", triage)
	}
	for _, s := range []State{StateWatching, StateDeltaReview} {
		step := Advance(true, s, EventPollGreen, false, out)
		if step.Decision.Allow || step.Closeout != nil {
			t.Fatalf("%s+green over P1 = %+v, want deny without closeout", s, step)
		}
		if step.Next != s {
			t.Fatalf("%s+green over P1 moved to %q, want hold", s, step.Next)
		}
	}
}

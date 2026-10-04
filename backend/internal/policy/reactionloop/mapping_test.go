package reactionloop

import (
	"testing"
)

func TestExtractFindingsMapsP0P1(t *testing.T) {
	comments := []ReviewComment{
		{ID: "c1", IsCodex: true, Body: "P0: data race in worker pool\nP1: missing timeout on dial"},
		{ID: "c2", IsCodex: true, Body: "[P0] nil map write on retry path"},
		{ID: "c3", IsCodex: true, Body: "p1: lowercase marker still counts"},
	}
	got := ExtractFindings(comments)
	if len(got) != 4 {
		t.Fatalf("findings = %+v, want 4", got)
	}
	if got[0].Severity != SevP0 || got[0].CommentID != "c1" {
		t.Fatalf("first finding = %+v, want P0 from c1", got[0])
	}
	if got[1].Severity != SevP1 {
		t.Fatalf("second finding = %+v, want P1", got[1])
	}
}

func TestExtractFindingsIgnoresNonCodexAndP2(t *testing.T) {
	comments := []ReviewComment{
		{ID: "human", IsCodex: false, Body: "P0: I quote the bot but this is discussion"},
		{ID: "bot", IsCodex: true, Body: "P2: nitpick, no action needed\nAP01 is a ticket ref, nothing more"},
		{ID: "bot2", IsCodex: true, Body: "P10 is a ticket number, not severity"},
	}
	if got := ExtractFindings(comments); len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func TestSummarizePollChecks(t *testing.T) {
	out := SummarizePoll(nil, []CheckRun{
		{Name: "unit", Status: "completed", Conclusion: "success"},
		{Name: "lint", Status: "completed", Conclusion: "failure"},
		{Name: "e2e", Status: "in_progress", Conclusion: ""},
	})
	if len(out.Failed) != 1 || out.Failed[0] != "lint" {
		t.Fatalf("failed = %v, want [lint]", out.Failed)
	}
	if len(out.Pending) != 1 || out.Pending[0] != "e2e" {
		t.Fatalf("pending = %v, want [e2e]", out.Pending)
	}
	if out.AllGreen() {
		t.Fatal("board with failed/pending checks must not be green")
	}
}

func TestSummarizePollAllGreen(t *testing.T) {
	out := SummarizePoll(
		[]ReviewComment{{ID: "c1", IsCodex: true, Body: "Looks good, no severity markers."}},
		[]CheckRun{{Name: "unit", Status: "completed", Conclusion: "success"}},
	)
	if !out.AllGreen() {
		t.Fatalf("outcome = %+v, want all green", out)
	}
	if out.HasBlocking() {
		t.Fatal("no P0/P1 must mean nothing blocking")
	}
}

func TestPollWithoutFindingsIsGreenNotTriage(t *testing.T) {
	out := SummarizePoll(nil, nil)
	if out.HasBlocking() {
		t.Fatal("empty poll must carry no blocking findings")
	}
	step := Advance(true, StateWatching, EventPollFindings, false, out)
	if step.Decision.Allow || step.Decision.Reason != ReasonMissingFindings {
		t.Fatalf("findings event without P0/P1 = %+v, want missing_findings deny", step.Decision)
	}
	if step.Next != StateWatching {
		t.Fatalf("denied step must hold watching, got %q", step.Next)
	}
}

// S4-P2-3: prose about P0/P1 is not a verdict. Negations, weaker-severity
// titles, and questions must never become blocking findings.
func TestExtractFindingsSkipsProse(t *testing.T) {
	comments := []ReviewComment{
		{ID: "c1", IsCodex: true, Body: "No P0 findings in this round.\nkeine P1-Befunde mehr offen\nP0: none\nP1 - nichts gefunden"},
		{ID: "c2", IsCodex: true, Body: "P2: style nit, mentions P0 handling in passing\n[P3] docs wording near P1 logic"},
		{ID: "c3", IsCodex: true, Body: "Is this really a P0?"},
	}
	if got := ExtractFindings(comments); len(got) != 0 {
		t.Fatalf("prose findings = %+v, want none", got)
	}
}

func TestExtractFindingsKeepsRealVerdicts(t *testing.T) {
	comments := []ReviewComment{
		{ID: "c1", IsCodex: true, Body: "P0/P1: both classes present, worst counts"},
		{ID: "c2", IsCodex: true, Body: "P1: missing timeout is worse than the old P2 nit"},
	}
	got := ExtractFindings(comments)
	if len(got) != 2 {
		t.Fatalf("verdict findings = %+v, want 2", got)
	}
	if got[0].Severity != SevP0 || got[1].Severity != SevP1 {
		t.Fatalf("verdict severities = %+v, want P0 then P1", got)
	}
}

// S4-P1-2: a poll that observed nothing proves nothing. Empty evidence
// must never read as green, with or without comments.
func TestEmptyPollIsNotGreen(t *testing.T) {
	if out := SummarizePoll(nil, nil); out.AllGreen() {
		t.Fatal("empty poll must not be green")
	}
	comments := []ReviewComment{{ID: "c1", IsCodex: true, Body: "Clean, no markers."}}
	if out := SummarizePoll(comments, nil); out.AllGreen() {
		t.Fatalf("poll without observed checks must not be green: %+v", out)
	}
}

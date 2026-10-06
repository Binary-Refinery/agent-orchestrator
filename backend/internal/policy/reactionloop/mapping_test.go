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
		{ID: "c3", IsCodex: true, Body: "Is the P0 label really warranted here? Please advise"},
	}
	if got := ExtractFindings(comments); len(got) != 0 {
		t.Fatalf("prose findings = %+v, want none", got)
	}
}

// S4-D3: quoted, listed, and bracket-denied prose must not convert. Each
// fixture carries a live marker match, so a skip proves the guard fired
// instead of the marker simply missing.
func TestExtractFindingsSkipsDeltaProse(t *testing.T) {
	for _, body := range []string{
		"> [P1] Historical issue already fixed",
		"> Quoted P0 verdict from the old thread",
		"1. [P2] Clarify P0 handling",
		"- [P2] Note on P1 scope",
		"No [P0] findings remain",
		"What about the P1 here? still valid",
		"If P0, then what?",
	} {
		got := ExtractFindings([]ReviewComment{{ID: "c", IsCodex: true, Body: body}})
		if len(got) != 0 {
			t.Fatalf("body %q -> %+v, want no findings", body, got)
		}
	}
}

// S4-D1: a denial word after the marker only denies when nothing
// substantive follows. "P1: no timeout on requests" states the missing
// timeout, so it must stay a blocking P1 verdict.
func TestDenialAfterKeepsSubstantiveVerdict(t *testing.T) {
	got := ExtractFindings([]ReviewComment{
		{ID: "c1", IsCodex: true, Body: "P1: no timeout on requests"},
	})
	if len(got) != 1 || got[0].Severity != SevP1 {
		t.Fatalf("substantive verdict = %+v, want one P1", got)
	}
	for _, body := range []string{"P0: none", "P0: none found", "P1: keine", "P1 - nichts gefunden"} {
		if got := ExtractFindings([]ReviewComment{{ID: "c", IsCodex: true, Body: body}}); len(got) != 0 {
			t.Fatalf("denial %q -> %+v, want no findings", body, got)
		}
	}
}

// S4-D3 exception round: benign trailing denial context must not revive
// the verdict. "P1: none found in this review" reports absence, so it
// stays non-blocking — while substance after the denial ("P0: none,
// review the timeout handling") keeps the line blocking.
func TestDenialTrailingContextSkipped(t *testing.T) {
	for _, body := range []string{
		"P1: none found in this review",
		"P0: no issues in this round",
		"P1: nothing found here",
		"P1: keine Befunde mehr",
	} {
		if got := ExtractFindings([]ReviewComment{{ID: "c", IsCodex: true, Body: body}}); len(got) != 0 {
			t.Fatalf("denial context %q -> %+v, want no findings", body, got)
		}
	}
	for _, body := range []string{
		"P1: no timeout on requests",
		"P0: none, review the timeout handling",
	} {
		got := ExtractFindings([]ReviewComment{{ID: "c", IsCodex: true, Body: body}})
		if len(got) != 1 {
			t.Fatalf("substantive line %q -> %+v, want one finding", body, got)
		}
	}
}

// S4 mixed-marker exception round: a line may hold a real verdict and a
// benign denial side by side. The verdict survives, only the denied part
// is ignored: "P1: timeout; P0: none found in this review" yields exactly
// one SevP1 finding.
func TestMixedMarkerLineKeepsRealVerdict(t *testing.T) {
	got := ExtractFindings([]ReviewComment{
		{ID: "c1", IsCodex: true, Body: "P1: timeout; P0: none found in this review"},
	})
	if len(got) != 1 || got[0].Severity != SevP1 {
		t.Fatalf("mixed line = %+v, want one SevP1", got)
	}
	mirrored := ExtractFindings([]ReviewComment{
		{ID: "c1", IsCodex: true, Body: "P0: none found; P1: timeout on requests"},
	})
	if len(mirrored) != 1 || mirrored[0].Severity != SevP1 {
		t.Fatalf("mirrored line = %+v, want one SevP1", mirrored)
	}
	both := ExtractFindings([]ReviewComment{
		{ID: "c1", IsCodex: true, Body: "P0: race on shutdown; P1: timeout on requests"},
	})
	if len(both) != 2 || both[0].Severity != SevP0 || both[1].Severity != SevP1 {
		t.Fatalf("two-verdict line = %+v, want P0 then P1", both)
	}
	neither := ExtractFindings([]ReviewComment{
		{ID: "c1", IsCodex: true, Body: "No P0, no P1 findings in this review"},
	})
	if len(neither) != 0 {
		t.Fatalf("double denial = %+v, want no findings", neither)
	}
}

// S4 coordinated-negation round: a denial keeps its force across comma
// coordinated lists. "No P0, nor P1 findings remain" denies both, so no
// marker may emit — neither the P0 nor the P1.
func TestCoordinatedNegationDeniesBoth(t *testing.T) {
	for _, body := range []string{
		"No P0, nor P1 findings remain",
		"Neither P0 nor P1 remain",
		"No P0 or P1 issues in this review",
	} {
		if got := ExtractFindings([]ReviewComment{{ID: "c", IsCodex: true, Body: body}}); len(got) != 0 {
			t.Fatalf("coordinated denial %q -> %+v, want no findings", body, got)
		}
	}
}

// S4 per-clause title round: a weaker-severity title governs its own
// semicolon clause only. "P0: race; P2: style note mentioning P1" keeps
// the P0 verdict and drops the P1 from the later P2-title prose clause.
func TestLaterP2TitleClauseDropsP1(t *testing.T) {
	got := ExtractFindings([]ReviewComment{
		{ID: "c1", IsCodex: true, Body: "P0: race; P2: style note mentioning P1"},
	})
	if len(got) != 1 || got[0].Severity != SevP0 {
		t.Fatalf("p2-title clause = %+v, want one SevP0", got)
	}
	// A comma does not break title scope: the whole line stays prose.
	skipped := ExtractFindings([]ReviewComment{
		{ID: "c1", IsCodex: true, Body: "P2: style nit, mentions P0 handling in passing"},
	})
	if len(skipped) != 0 {
		t.Fatalf("comma p2 prose = %+v, want no findings", skipped)
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

// S4-P1-2/S4-D2: a poll that observed nothing proves nothing, on either
// side. Empty evidence must never read as green: neither without checks,
// nor without review comments — even when the observed half is green.
func TestEmptyPollIsNotGreen(t *testing.T) {
	if out := SummarizePoll(nil, nil); out.AllGreen() {
		t.Fatal("empty poll must not be green")
	}
	comments := []ReviewComment{{ID: "c1", IsCodex: true, Body: "Clean, no markers."}}
	if out := SummarizePoll(comments, nil); out.AllGreen() {
		t.Fatalf("poll without observed checks must not be green: %+v", out)
	}
	checks := []CheckRun{{Name: "unit", Status: "completed", Conclusion: "success"}}
	if out := SummarizePoll(nil, checks); out.AllGreen() {
		t.Fatalf("poll without observed review comments must not be green: %+v", out)
	}
}

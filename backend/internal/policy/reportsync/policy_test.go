package reportsync

import (
	"strings"
	"testing"
)

func goodDraft() string {
	return "VERLAUF\narbeit\nSTAND\nhalb\nPROBLEM/BEFUND\nbefund\nBELEG\nsha 123\nOFFEN\nrest\nNAECHSTE SCHRITTE\ntests\nGATE\nSoll ich mergen?"
}

func TestDisabledFailsClosed(t *testing.T) {
	if d := ValidateDraft(false, goodDraft()); d.Allow || d.Reason != ReasonDisabled {
		t.Fatalf("draft = %+v, want disabled deny", d)
	}
	if _, d := PlanSync(false, SyncInput{Phase: PhaseStarted, Facts: []string{"sha"}, Repo: PilotRepos[0], HasAuth: true}); d.Allow {
		t.Fatalf("sync = %+v, want deny when off", d)
	}
}

func TestValidatorAccepts(t *testing.T) {
	if d := ValidateDraft(true, goodDraft()); !d.Allow {
		t.Fatalf("good draft denied: %+v", d)
	}
	noGate := "VERLAUF\na\nSTAND\nb\nPROBLEM/BEFUND\nc\nBELEG\nd\nOFFEN\ne\nNAECHSTE SCHRITTE\nf\nGATE\nkein Gate offen"
	if d := ValidateDraft(true, noGate); !d.Allow {
		t.Fatalf("explicit no-gate denied: %+v", d)
	}
}

func TestValidatorRejectsMissingEmpty(t *testing.T) {
	missing := "VERLAUF\na\nSTAND\nb\nGATE\nFrage?"
	if d := ValidateDraft(true, missing); d.Allow || d.Reason != ReasonIncomplete {
		t.Fatalf("missing sections = %+v, want incomplete", d)
	}
	empty := strings.Replace(goodDraft(), "halb", "   ", 1)
	if d := ValidateDraft(true, empty); d.Allow || d.Reason != ReasonIncomplete {
		t.Fatalf("empty section = %+v, want incomplete", d)
	}
}

func TestValidatorRejectsVagueGate(t *testing.T) {
	vague := strings.Replace(goodDraft(), "Soll ich mergen?", "Alles gut, weiter so.", 1)
	if d := ValidateDraft(true, vague); d.Allow || d.Reason != ReasonGateVague {
		t.Fatalf("vague gate = %+v, want gate_vague", d)
	}
}

// P2-2: a lone "?" (or another terse fragment) is not a concrete gate.
func TestValidatorRejectsLoneQuestionMarkGate(t *testing.T) {
	for _, gate := range []string{"?", "  ?  ", "Mergen?", "Ja?", "? - - -", "? ... ..."} {
		draft := strings.Replace(goodDraft(), "Soll ich mergen?", gate, 1)
		if d := ValidateDraft(true, draft); d.Allow || d.Reason != ReasonGateVague {
			t.Fatalf("gate %q = %+v, want gate_vague", gate, d)
		}
	}
	// Concrete decision questions and explicit no-gate still pass.
	for _, gate := range []string{"Soll ich den PR mergen?", "Kein Gate offen."} {
		draft := strings.Replace(goodDraft(), "Soll ich mergen?", gate, 1)
		if d := ValidateDraft(true, draft); !d.Allow {
			t.Fatalf("gate %q denied: %+v", gate, d)
		}
	}
}

// P2-1: empty markdown sections and duplicate headings fail closed.
func TestValidatorRejectsEmptyMarkdownAndDuplicateHeading(t *testing.T) {
	md := "## VERLAUF\narbeit\n## STAND\n## PROBLEM/BEFUND\nbefund\n## BELEG\nsha\n## OFFEN\nrest\n## NAECHSTE SCHRITTE\ntests\n## GATE\nSoll ich den PR mergen?"
	if d := ValidateDraft(true, md); d.Allow || d.Reason != ReasonIncomplete {
		t.Fatalf("empty markdown section = %+v, want incomplete", d)
	}
	dup := goodDraft() + "\nSTAND\nnochmal"
	if d := ValidateDraft(true, dup); d.Allow || d.Reason != ReasonIncomplete {
		t.Fatalf("duplicate heading = %+v, want incomplete", d)
	}
}

func TestSyncDedup(t *testing.T) {
	seen := map[string]bool{"i1:gestartet": true}
	in := SyncInput{Phase: PhaseStarted, Facts: []string{"sha abc"}, Repo: PilotRepos[0], HasAuth: true, Seen: seen, SeenKey: "i1:gestartet"}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonDuplicate {
		t.Fatalf("dup = %+v, want duplicate deny", d)
	}
}

// P2-3: a missing event id fails closed instead of bypassing dedup.
func TestSyncRequiresSeenKey(t *testing.T) {
	in := SyncInput{Phase: PhaseStarted, Facts: []string{"sha abc"}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonNoKey {
		t.Fatalf("empty seen key = %+v, want missing_key deny", d)
	}
	// Even a Seen store claiming the empty key stays fail-closed.
	in.Seen = map[string]bool{"": true}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonNoKey {
		t.Fatalf("empty seen key with marked store = %+v, want missing_key deny", d)
	}
}

func TestSyncNoPostingWithoutEvent(t *testing.T) {
	in := SyncInput{Facts: []string{"sha"}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonNoEvent {
		t.Fatalf("no event = %+v, want no_event deny", d)
	}
	bad := in
	bad.Phase = "bewertet"
	if _, d := PlanSync(true, bad); d.Allow || d.Reason != ReasonNoEvent {
		t.Fatalf("unknown phase = %+v, want no_event deny", d)
	}
}

func TestSyncUngroundedAndApproval(t *testing.T) {
	// facts mentioning merge without approval event are fine as facts only
	// if they carry no valuation; valuation words need approval.
	in := SyncInput{Phase: PhaseMerged, Facts: []string{"PR #1 gemergt"}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}, SeenKey: "k1"}
	if _, d := PlanSync(true, in); d.Allow {
		t.Fatalf("valuation without approval allowed: %+v", d)
	}
	in.Approval = true
	if _, d := PlanSync(true, in); !d.Allow {
		t.Fatalf("approval-gated facts denied: %+v", d)
	}
	// merge claim without any facts fails
	nofacts := SyncInput{Phase: PhaseMerged, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}, SeenKey: "k2"}
	if _, d := PlanSync(true, nofacts); d.Allow || d.Reason != ReasonUngrounded {
		t.Fatalf("no facts = %+v, want ungrounded", d)
	}
}

// P1-3: publish-go/approval statements without an approval event fail.
func TestSyncRejectsUngroundedApprovalClaim(t *testing.T) {
	in := SyncInput{Phase: PhaseReview, Facts: []string{"Publish-Go: GRANTED"}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}, SeenKey: "k-p13"}
	if a, d := PlanSync(true, in); d.Allow || d.Reason != ReasonUngrounded || a.FactOnly {
		t.Fatalf("approval claim = %+v action=%+v, want ungrounded deny", d, a)
	}
	in.Approval = true
	if _, d := PlanSync(true, in); !d.Allow {
		t.Fatalf("approval-backed claim denied: %+v", d)
	}
}

// P2-4: empty/whitespace facts are not grounding.
func TestSyncRejectsEmptyFacts(t *testing.T) {
	in := SyncInput{Phase: PhaseCIResult, Facts: []string{"", "   "}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}, SeenKey: "k-p24"}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonUngrounded {
		t.Fatalf("empty facts = %+v, want ungrounded deny", d)
	}
}

func TestSyncScopeAndAuth(t *testing.T) {
	in := SyncInput{Phase: PhaseStarted, Facts: []string{"sha"}, Repo: "other/repo", HasAuth: true, Seen: map[string]bool{}, SeenKey: "k-s1"}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonRepoOutOfcope {
		t.Fatalf("out of scope = %+v", d)
	}
	in = SyncInput{Phase: PhaseStarted, Facts: []string{"sha"}, Repo: PilotRepos[1], HasAuth: false, Seen: map[string]bool{}, SeenKey: "k-s2"}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonNoAuth {
		t.Fatalf("no auth = %+v", d)
	}
}

func TestTokenNeverInOutput(t *testing.T) {
	in := SyncInput{Phase: PhaseCIResult, Facts: []string{"ci green", "sha abc"}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}, SeenKey: "k-t1"}
	a, d := PlanSync(true, in)
	if !d.Allow {
		t.Fatalf("denied: %+v", d)
	}
	if !strings.HasPrefix(a.Comment, "FAKTEN") {
		t.Fatalf("comment not fact-prefixed: %q", a.Comment)
	}
	for _, s := range []string{"alpha", "beta", "gamma", "delta", "eps"} {
		got := Sanitize("token=" + s + "-token-1")
		if strings.Contains(got, s) {
			t.Fatalf("sanitize leaked value for input class %q", s)
		}
	}
}

// P1-2: every token assignment is redacted, whatever its format or count.
func TestSanitizeRedactsAllAssignments(t *testing.T) {
	in := "token=alpha1 token=beta2 TOKEN: gamma3 token:delta4 TokEn = \"eps5\" token=\tzeta6 token:\teta7"
	got := Sanitize(in)
	for _, want := range []string{"alpha1", "beta2", "gamma3", "delta4", "eps5", "zeta6", "eta7"} {
		if strings.Contains(got, want) {
			t.Fatalf("assignment value leaked: %q", got)
		}
	}
	if strings.Count(got, "[redacted]") != 7 {
		t.Fatalf("expected 7 redactions, got %q", got)
	}
	prefixed := Sanitize("ghp_alpha1 and gho_beta2 and github_pat_gamma3")
	for _, want := range []string{"alpha1", "beta2", "gamma3"} {
		if strings.Contains(prefixed, want) {
			t.Fatalf("prefixed token leaked: %q", prefixed)
		}
	}
}

func TestSyncEmitsOnlyFactsAndStatus(t *testing.T) {
	in := SyncInput{Phase: PhasePRCreated, Facts: []string{"PR https://example/x/1"}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}, SeenKey: "k-f1"}
	a, d := PlanSync(true, in)
	if !d.Allow {
		t.Fatalf("denied: %+v", d)
	}
	if !a.FactOnly || a.BoardStatus == "" || !strings.HasPrefix(a.Comment, "FAKTEN") {
		t.Fatalf("action not fact-only: %+v", a)
	}
}

func TestEnabledFromEnvDefaultOff(t *testing.T) {
	if EnabledFromEnv("") || EnabledFromEnv("0") || EnabledFromEnv("false") {
		t.Fatal("must default off")
	}
	if !EnabledFromEnv("1") || !EnabledFromEnv("true") {
		t.Fatal("opt-in values must enable")
	}
}

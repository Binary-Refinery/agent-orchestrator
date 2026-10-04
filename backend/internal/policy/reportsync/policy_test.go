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

func TestSyncDedup(t *testing.T) {
	seen := map[string]bool{"i1:gestartet": true}
	in := SyncInput{Phase: PhaseStarted, Facts: []string{"sha abc"}, Repo: PilotRepos[0], HasAuth: true, Seen: seen, SeenKey: "i1:gestartet"}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonDuplicate {
		t.Fatalf("dup = %+v, want duplicate deny", d)
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
	in := SyncInput{Phase: PhaseMerged, Facts: []string{"PR #1 gemergt"}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}}
	if _, d := PlanSync(true, in); d.Allow {
		t.Fatalf("valuation without approval allowed: %+v", d)
	}
	in.Approval = true
	if _, d := PlanSync(true, in); !d.Allow {
		t.Fatalf("approval-gated facts denied: %+v", d)
	}
	// merge claim without any facts fails
	nofacts := SyncInput{Phase: PhaseMerged, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}}
	if _, d := PlanSync(true, nofacts); d.Allow || d.Reason != ReasonUngrounded {
		t.Fatalf("no facts = %+v, want ungrounded", d)
	}
}

func TestSyncScopeAndAuth(t *testing.T) {
	in := SyncInput{Phase: PhaseStarted, Facts: []string{"sha"}, Repo: "other/repo", HasAuth: true, Seen: map[string]bool{}}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonRepoOutOfcope {
		t.Fatalf("out of scope = %+v", d)
	}
	in = SyncInput{Phase: PhaseStarted, Facts: []string{"sha"}, Repo: PilotRepos[1], HasAuth: false, Seen: map[string]bool{}}
	if _, d := PlanSync(true, in); d.Allow || d.Reason != ReasonNoAuth {
		t.Fatalf("no auth = %+v", d)
	}
}

func TestTokenNeverInOutput(t *testing.T) {
	in := SyncInput{Phase: PhaseCIResult, Facts: []string{"token=ghp_secret1234567890 ok", "sha abc"}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}}
	a, d := PlanSync(true, in)
	if !d.Allow {
		t.Fatalf("denied: %+v", d)
	}
	if strings.Contains(a.Comment, "ghp_secret") || strings.Contains(a.Comment, "token=ghp") {
		t.Fatalf("token leaked in comment: %q", a.Comment)
	}
	if !strings.HasPrefix(a.Comment, "FAKTEN") {
		t.Fatalf("comment not fact-prefixed: %q", a.Comment)
	}
	for _, s := range []string{"ghp_abc123 token=mytoken123", "TOKEN: secret99", "github_pat_xyz"} {
		got := Sanitize(s)
		if strings.Contains(got, "abc123") || strings.Contains(got, "mytoken123") || strings.Contains(got, "secret99") || strings.Contains(got, "xyz") {
			t.Fatalf("sanitize leaked: %q -> %q", s, got)
		}
	}
}

func TestSyncEmitsOnlyFactsAndStatus(t *testing.T) {
	in := SyncInput{Phase: PhasePRCreated, Facts: []string{"PR https://example/x/1"}, Repo: PilotRepos[0], HasAuth: true, Seen: map[string]bool{}}
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

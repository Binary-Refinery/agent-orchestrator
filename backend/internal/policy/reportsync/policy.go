// Package reportsync implements Slice 3 of af-ao-automation-v2: an opt-in,
// default-off, fail-closed report validator plus fact-only issue sync.
//
// Red lines (enforced by design, no code path exists for them): no auto-push,
// no auto-merge, no task-worker auto-terminate, no finding auto-confirm,
// no publish-go automation. This package only validates drafts and builds
// fact comments plus board-status updates. Anything beyond facts requires
// an explicit approval event, which this package never fabricates.
package reportsync

import (
	"os"
	"strings"
)

// RejectReason is the typed reason every refusal carries (fail-closed).
type RejectReason string

const (
	ReasonDisabled      RejectReason = "reportsync_disabled"
	ReasonIncomplete    RejectReason = "report_incomplete"
	ReasonGateVague     RejectReason = "report_gate_vague"
	ReasonNoEvent       RejectReason = "sync_no_event"
	ReasonNoKey         RejectReason = "sync_missing_key"
	ReasonNoDeps        RejectReason = "sync_missing_deps"
	ReasonPostFailed    RejectReason = "sync_post_failed"
	ReasonDuplicate     RejectReason = "sync_duplicate"
	ReasonUngrounded    RejectReason = "sync_ungrounded_claim"
	ReasonRepoOutOfcope RejectReason = "sync_repo_out_of_scope"
	ReasonNoAuth        RejectReason = "sync_no_auth"
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
// set AO_REPORT_SYNC=1 (or "true"/"on") to enable.
func Enabled() bool { return EnabledFromEnv(os.Getenv("AO_REPORT_SYNC")) }

// EnabledFromEnv parses the opt-in switch for tests and callers.
func EnabledFromEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Sections is the required 7-section report format, in order.
var Sections = []string{
	"VERLAUF",
	"STAND",
	"PROBLEM/BEFUND",
	"BELEG",
	"OFFEN",
	"NAECHSTE SCHRITTE",
	"GATE",
}

// ValidateDraft checks the 7-section format and GATE concreteness.
// Missing, empty, or duplicated sections deny with ReasonIncomplete. A GATE
// without a concrete decision question or an explicit "kein Gate offen"
// denies with ReasonGateVague. Disabled denies closed with ReasonDisabled.
func ValidateDraft(enabled bool, draft string) Decision {
	if !enabled {
		return deny(ReasonDisabled, "report-sync policy off")
	}
	bodies, dups := parseSections(draft)
	for _, s := range Sections {
		if dups[s] {
			return deny(ReasonIncomplete, "duplicate section "+s)
		}
		body, ok := bodies[s]
		if !ok {
			return deny(ReasonIncomplete, "missing section "+s)
		}
		if strings.TrimSpace(body) == "" {
			return deny(ReasonIncomplete, "empty section "+s)
		}
	}
	gate := strings.TrimSpace(bodies["GATE"])
	if isExplicitNoGate(gate) {
		return allow()
	}
	if isConcreteQuestion(gate) {
		return allow()
	}
	return deny(ReasonGateVague, "GATE needs a concrete decision question or explicit kein Gate offen")
}

// parseSections maps each known heading to its body text. Headings are
// detected per line (case-insensitive, optional leading markdown hashes,
// optional trailing colon with same-line body). It also reports headings
// seen more than once so duplicates fail closed.
func parseSections(draft string) (bodies map[string]string, dups map[string]bool) {
	bodies = map[string]string{}
	counts := map[string]int{}
	var current string
	currentSeen := false
	flush := func() {}
	_ = flush
	for _, line := range strings.Split(draft, "\n") {
		if sec, rest := headingOf(line); sec != "" {
			counts[sec]++
			current = sec
			currentSeen = true
			if rest != "" {
				bodies[sec] += rest + "\n"
			}
			continue
		}
		if currentSeen {
			bodies[current] += line + "\n"
		}
	}
	dups = map[string]bool{}
	for sec, n := range counts {
		if n > 1 {
			dups[sec] = true
		}
	}
	return bodies, dups
}

// headingOf reports whether a line is a section heading and, for the
// "HEADING: body" form, the same-line body.
func headingOf(line string) (sec string, rest string) {
	t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
	t = strings.TrimSpace(t)
	u := strings.ToUpper(t)
	for _, s := range Sections {
		if u == s {
			return s, ""
		}
		if strings.HasPrefix(u, s+":") {
			return s, strings.TrimSpace(t[len(s)+1:])
		}
	}
	return "", ""
}

// isExplicitNoGate accepts an explicit "kein Gate offen" statement.
func isExplicitNoGate(gate string) bool {
	return strings.Contains(strings.ToUpper(gate), "KEIN GATE OFFEN")
}

// isConcreteQuestion accepts only a real decision question: a "?" plus at
// least three words of substance. A lone "?" (or other terse fragments)
// is vague, not concrete.
func isConcreteQuestion(gate string) bool {
	if !strings.Contains(gate, "?") {
		return false
	}
	cleaned := strings.ReplaceAll(gate, "?", " ")
	return len(strings.Fields(cleaned)) >= 3
}

// Phase is a reportable lifecycle event. Only these produce postings.
type Phase string

const (
	PhaseStarted   Phase = "gestartet"
	PhaseCommitted Phase = "committed"
	PhasePRCreated Phase = "pr_angelegt"
	PhaseCIResult  Phase = "ci_ergebnis"
	PhaseReview    Phase = "review_befund"
	PhaseMerged    Phase = "gemergt"
	PhaseAborted   Phase = "abgebrochen"
)

// ValidPhases is the closed vocabulary of syncable events.
var ValidPhases = []Phase{
	PhaseStarted, PhaseCommitted, PhasePRCreated,
	PhaseCIResult, PhaseReview, PhaseMerged, PhaseAborted,
}

// PilotRepos bounds credential use (Token path A): only these repos.
var PilotRepos = []string{
	"Binary-Refinery/agent-orchestrator",
	"Artifaktory/Artifaktory",
}

// Action is the only thing sync may emit: a fact comment and/or a board
// status update. There is deliberately no push/merge/terminate action.
type Action struct {
	Comment     string
	BoardStatus string
	FactOnly    bool
}

// SyncInput carries one phase event plus its grounding facts.
type SyncInput struct {
	Phase Phase
	// Facts are the grounded observations (SHAs, URLs, check names).
	// Empty or whitespace-only entries are ignored; at least one
	// substantive fact is required.
	Facts []string
	// Repo is "owner/name"; must be a pilot repo.
	Repo string
	// HasAuth reuses existing gh auth; false fails closed without posting.
	HasAuth bool
	// Approval is true only when an explicit human approval event exists.
	// Anything beyond bare facts (valuations, releases, merge claims,
	// publish-go/approval statements) requires it.
	Approval bool
	// Seen deduplicates per phase event; key e.g. issue+phase.
	Seen map[string]bool
	// SeenKey identifies this event for dedup. Empty fails closed so a
	// missing event id can never bypass dedup.
	SeenKey string
}

// PlanSync builds the fact-only posting for one phase event, or denies
// closed. Dedup: a SeenKey already in Seen denies with ReasonDuplicate and
// never reposts (no retry spam: callers must not retry on deny).
func PlanSync(enabled bool, in SyncInput) (Action, Decision) {
	var zero Action
	if !enabled {
		return zero, deny(ReasonDisabled, "report-sync policy off")
	}
	if strings.TrimSpace(string(in.Phase)) == "" || !validPhase(in.Phase) {
		return zero, deny(ReasonNoEvent, "no phase event, no posting")
	}
	if !in.HasAuth {
		return zero, deny(ReasonNoAuth, "no gh auth reused, refusing")
	}
	if !repoAllowed(in.Repo) {
		return zero, deny(ReasonRepoOutOfcope, "repo outside pilot scope")
	}
	if strings.TrimSpace(in.SeenKey) == "" {
		return zero, deny(ReasonNoKey, "missing event id, refusing to bypass dedup")
	}
	if in.Seen[in.SeenKey] {
		return zero, deny(ReasonDuplicate, "phase event already synced")
	}
	facts := substantiveFacts(in.Facts)
	if len(facts) == 0 {
		return zero, deny(ReasonUngrounded, "phase event needs at least one substantive fact")
	}
	comment := "FAKTEN [" + string(in.Phase) + "] " + strings.Join(facts, " | ")
	if d := factOnly(comment, in.Approval); !d.Allow {
		return zero, d
	}
	comment = Sanitize(comment)
	return Action{Comment: comment, BoardStatus: boardStatus(in.Phase), FactOnly: true}, allow()
}

// substantiveFacts drops empty and whitespace-only entries.
func substantiveFacts(facts []string) []string {
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		if strings.TrimSpace(f) == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// validPhase checks the closed phase vocabulary.
func validPhase(p Phase) bool {
	for _, v := range ValidPhases {
		if v == p {
			return true
		}
	}
	return false
}

func repoAllowed(repo string) bool {
	for _, r := range PilotRepos {
		if strings.EqualFold(strings.TrimSpace(repo), r) {
			return true
		}
	}
	return false
}

// bannedWithoutApproval lists valuations, releases, merge claims, and
// publish-go/approval statements that need an explicit approval event on
// top of facts.
var bannedWithoutApproval = []string{
	"freigabe", "freigegeben", "lgtm", "approved", "approval",
	"gemergt", "merged", "publiziert", "publish-go", "publish go",
	"granted", "genehmigt", "genehmigung",
	"bestätigt", "bestaetigt", "abgenommen", "abnahme",
}

// factOnly rejects comments carrying valuations or ungrounded claims
// unless an explicit approval event exists.
func factOnly(comment string, approval bool) Decision {
	if approval {
		return allow()
	}
	folded := strings.ToLower(comment)
	for _, b := range bannedWithoutApproval {
		if strings.Contains(folded, b) {
			return deny(ReasonUngrounded, "needs approval event: "+b)
		}
	}
	return allow()
}

func boardStatus(p Phase) string {
	switch p {
	case PhaseStarted:
		return "gestartet"
	case PhaseCommitted:
		return "committed"
	case PhasePRCreated:
		return "pr_angelegt"
	case PhaseCIResult:
		return "ci_ergebnis"
	case PhaseReview:
		return "review_befund"
	case PhaseMerged:
		return "gemergt"
	case PhaseAborted:
		return "abgebrochen"
	default:
		return ""
	}
}

// Sanitize redacts token-shaped material so tokens never land in
// repo/logs/artifacts. It replaces ghp_/gho_/github_pat_ values and every
// token assignment value ("token=...", "token:...", including spaced and
// quoted forms like `Token = "abc"`) with "[redacted]".
func Sanitize(s string) string {
	out := s
	for _, prefix := range []string{"ghp_", "gho_", "github_pat_"} {
		out = redactPrefixed(out, prefix)
	}
	return redactTokenAssignments(out)
}

func redactPrefixed(s, prefix string) string {
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			return s
		}
		j := i + len(prefix)
		for j < len(s) && isTokenChar(s[j]) {
			j++
		}
		s = s[:i] + "[redacted]" + s[j:]
	}
}

// redactTokenAssignments redacts every token assignment value: the word
// "token" (any case) followed by optional spaces and "=" or ":", then the
// value with optional spaces/quotes. It walks the string once, copying
// output forward, so an already-redacted assignment is never re-matched and
// no later assignment is skipped.
func redactTokenAssignments(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	start := 0
	for start < len(s) {
		rel := strings.Index(lower[start:], "token")
		if rel < 0 {
			b.WriteString(s[start:])
			break
		}
		i := start + rel
		j := i + len("token")
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		if j >= len(s) || (s[j] != '=' && s[j] != ':') {
			// "token" without an assignment separator: copy through and
			// continue scanning after the word.
			b.WriteString(s[start:j])
			start = j
			continue
		}
		j++
		for j < len(s) && (s[j] == ' ' || s[j] == '"' || s[j] == '\'') {
			j++
		}
		k := j
		for k < len(s) && isTokenChar(s[k]) {
			k++
		}
		if k == j {
			// Separator with no value: copy through and continue after it.
			b.WriteString(s[start:j])
			start = j
			continue
		}
		b.WriteString(s[start:j])
		b.WriteString("[redacted]")
		start = k
	}
	return b.String()
}

func isTokenChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '_' || c == '-'
}

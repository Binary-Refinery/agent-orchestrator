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
// Missing or empty sections deny with ReasonIncomplete. A GATE without a
// concrete decision question ("?") or an explicit "kein Gate offen" denies
// with ReasonGateVague. Disabled denies closed with ReasonDisabled.
func ValidateDraft(enabled bool, draft string) Decision {
	if !enabled {
		return deny(ReasonDisabled, "report-sync policy off")
	}
	upper := strings.ToUpper(draft)
	bodies := splitSections(upper, draft)
	for _, s := range Sections {
		body, ok := bodies[s]
		if !ok {
			return deny(ReasonIncomplete, "missing section "+s)
		}
		if strings.TrimSpace(stripHeading(body)) == "" {
			return deny(ReasonIncomplete, "empty section "+s)
		}
	}
	gate := strings.TrimSpace(stripHeading(bodies["GATE"]))
	if strings.Contains(gate, "?") {
		return allow()
	}
	folded := strings.ToUpper(gate)
	if strings.Contains(folded, "KEIN GATE OFFEN") {
		return allow()
	}
	return deny(ReasonGateVague, "GATE needs a concrete decision question or explicit kein Gate offen")
}

// splitSections maps each known heading to the raw text following it up to
// the next known heading. Matching is case-insensitive on upper-cased input.
func splitSections(upper, raw string) map[string]string {
	idx := map[string]int{}
	for _, s := range Sections {
		if i := strings.Index(upper, s); i >= 0 {
			// Keep first occurrence only (fail-closed on ambiguity is
			// handled by emptiness checks downstream).
			if _, seen := idx[s]; !seen {
				idx[s] = i
			}
		}
	}
	out := map[string]string{}
	type pos struct {
		name string
		at   int
	}
	var order []pos
	for _, s := range Sections {
		if at, ok := idx[s]; ok {
			order = append(order, pos{s, at})
		}
	}
	for i := 0; i < len(order); i++ {
		end := len(raw)
		for j := 0; j < len(order); j++ {
			if order[j].at > order[i].at && order[j].at < end {
				end = order[j].at
			}
		}
		out[order[i].name] = raw[order[i].at:end]
	}
	return out
}

// stripHeading removes the heading line itself so emptiness checks look at
// body content only.
func stripHeading(chunk string) string {
	if i := strings.Index(chunk, "\n"); i >= 0 {
		return chunk[i+1:]
	}
	return ""
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
	Facts []string
	// Repo is "owner/name"; must be a pilot repo.
	Repo string
	// HasAuth reuses existing gh auth; false fails closed without posting.
	HasAuth bool
	// Approval is true only when an explicit human approval event exists.
	// Anything beyond bare facts (valuations, releases, merge claims
	// without facts) requires it.
	Approval bool
	// Seen deduplicates per phase event; key e.g. issue+phase.
	Seen map[string]bool
	// SeenKey identifies this event for dedup.
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
	if in.SeenKey != "" && in.Seen[in.SeenKey] {
		return zero, deny(ReasonDuplicate, "phase event already synced")
	}
	if len(in.Facts) == 0 {
		return zero, deny(ReasonUngrounded, "merge/publish claims need facts")
	}
	comment := "FAKTEN [" + string(in.Phase) + "] " + strings.Join(in.Facts, " | ")
	if d := factOnly(comment, in.Approval); !d.Allow {
		return zero, d
	}
	comment = Sanitize(comment)
	return Action{Comment: comment, BoardStatus: boardStatus(in.Phase), FactOnly: true}, allow()
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

// bannedWithoutApproval lists valuations/releases/merge claims that need an
// explicit approval event on top of facts.
var bannedWithoutApproval = []string{
	"freigabe", "freigegeben", "lgtm", "approved",
	"gemergt", "merged", "publiziert", "release freigegeben",
	"bestätigt", "bestaetigt",
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
// repo/logs/artifacts. It replaces ghp_/gho_/github_pat_ values and any
// "token=..." / "token:..." assignment value with "[redacted]".
func Sanitize(s string) string {
	out := s
	for _, prefix := range []string{"ghp_", "gho_", "github_pat_"} {
		out = redactPrefixed(out, prefix)
	}
	out = redactAssigned(out, "token=")
	out = redactAssigned(out, "token:")
	return out
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

func redactAssigned(s, key string) string {
	lower := strings.ToLower(s)
	for {
		i := strings.Index(lower, key)
		if i < 0 {
			return s
		}
		j := i + len(key)
		for j < len(s) && (s[j] == ' ' || s[j] == '"' || s[j] == '\'') {
			j++
		}
		k := j
		for k < len(s) && isTokenChar(s[k]) {
			k++
		}
		if k == j {
			break
		}
		s = s[:j] + "[redacted]" + s[k:]
		lower = strings.ToLower(s)
	}
	return s
}

func isTokenChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '_' || c == '-'
}

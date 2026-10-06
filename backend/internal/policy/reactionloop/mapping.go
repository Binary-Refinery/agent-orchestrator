package reactionloop

import (
	"regexp"
	"strings"
)

// Severity classifies one extracted review remark. Only P0 and P1 drive
// the triage proposal; everything else is informational context.
type Severity string

// Closed severity vocabulary.
const (
	SevP0    Severity = "P0"
	SevP1    Severity = "P1"
	SevOther Severity = "other"
)

// ReviewComment is one PR review comment as handed to the loop by the
// caller. Only Codex-authored comments feed the P0/P1 extraction; human
// and other-bot comments are ignored so a human quoting "P0" in
// discussion can never fabricate a finding.
type ReviewComment struct {
	ID      string
	Body    string
	IsCodex bool
}

// Finding is one extracted P0/P1 remark: the comment it came from, its
// severity, and the trimmed source line as summary.
type Finding struct {
	CommentID string
	Severity  Severity
	Summary   string
}

// CheckRun is one CI check run as handed to the loop by the caller.
type CheckRun struct {
	Name string
	// Status is the provider status: queued, in_progress, completed, etc.
	Status string
	// Conclusion is the provider conclusion: success, failure, etc.
	Conclusion string
}

// PollOutcome is the aggregated result of polling one PR: the P0/P1
// findings plus the check-run picture.
type PollOutcome struct {
	Findings []Finding
	// Failed names completed check runs whose conclusion is not green.
	Failed []string
	// Pending names check runs that have not completed yet.
	Pending []string
	// Observed counts the check runs seen in this poll.
	Observed int
	// CommentsSeen counts the review comments observed in this poll.
	// A poll that observed no comments proves nothing about the review
	// side: AllGreen requires CommentsSeen > 0, so a missing or
	// incomplete review collection can never read as green even when
	// the observed checks are green.
	CommentsSeen int
}

// severityMarker matches P0/P1 verdict labels in the forms reviewers use:
// P0, P1, P0/P1, [P0], (P1), "P0:" — case-insensitive, word-bounded so
// P10 or AP01 never match.
var severityMarker = regexp.MustCompile(`(?i)(?:^|[\s\[\(.:;/-])P([01])(?:$|[\s\]\)}.,:;/-])`)

// HasBlocking reports whether the outcome carries P0/P1 findings that
// require a triage proposal.
func (o PollOutcome) HasBlocking() bool { return len(o.Findings) > 0 }

// P0Count counts P0 findings in the outcome.
func (o PollOutcome) P0Count() int { return countSev(o.Findings, SevP0) }

// P1Count counts P1 findings in the outcome.
func (o PollOutcome) P1Count() int { return countSev(o.Findings, SevP1) }

// AllGreen reports whether the PR needs nothing: no P0/P1 findings, no
// failed checks, no pending checks, at least one observed check run,
// and at least one observed review comment. Empty or one-sided evidence
// never reads as green. Only an all-green poll may draft a closeout
// note.
func (o PollOutcome) AllGreen() bool {
	return o.Observed > 0 && o.CommentsSeen > 0 &&
		len(o.Findings) == 0 && len(o.Failed) == 0 && len(o.Pending) == 0
}

func countSev(findings []Finding, sev Severity) int {
	n := 0
	for _, f := range findings {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

// lowerSeverityTitle matches a line led by a weaker severity label (P2
// and up), optionally behind a list marker: a P0/P1 mention inside such
// a line is prose about another class, never a blocking verdict of its
// own. Examples: "P2: ...", "[P3] ...", "1. [P2] Clarify P0 handling".
var lowerSeverityTitle = regexp.MustCompile(`(?i)^\s*(?:\d+[.)]\s*|[-*+]\s*)?[\[\(]?\s*P[2-9]\b`)

// negationBefore matches a denial word shortly before the marker, in
// English or German, brackets included: "no P0", "No [P0] findings
// remain", "without any P1", "keine P0-Befunde". Coordinating denials
// ("nor", "neither") keep their force across comma lists: "No P0, nor
// P1 findings remain" denies both. Such lines report the absence of
// severe findings, so they must not become blocking findings themselves.
var negationBefore = regexp.MustCompile(`(?i)\b(no|not|without|zero|none|neither|nor|kein\w*|ohne)\b[\s\wäöü/\-[\(]{0,12}P[01]\b`)

// denialAfter matches a denial word right after a verdict marker:
// "P0: none", "P1 - nichts gefunden". The denial only covers the line
// when nothing substantive follows it (see fillerAfter): "P1: no
// timeout on requests" states a real verdict — the missing timeout —
// and must stay blocking.
var denialAfter = regexp.MustCompile(`(?i)\bP[01]\b\s*[:\-–—]?\s*(none|nothing|kein\w*|nichts|ohne|no)\b`)

// fillerAfter lists words that may trail a denial without reviving the
// verdict: "P0: none found", "P1 - nichts gefunden", "P1: none found
// in this review". It covers benign trailing denial context (locations
// like "in this review", restatements like "issues"/"findings", German
// counterparts), but never articles or substance nouns: "the" or
// "timeout" after the denial word means the line still says something
// about P0/P1, so the verdict stands. Any other trailing word keeps the
// line blocking. Residual ambiguity resolves toward the verdict: a
// spurious triage proposal stays human-gated, while a missed verdict
// could wrongly green-light a closeout.
var fillerAfter = map[string]bool{
	"found": true, "remaining": true, "remain": true, "remains": true,
	"left": true, "mehr": true, "noch": true, "offen": true,
	"vorhanden": true, "vorliegend": true, "gefunden": true,
	"übrig": true, "uebrig": true, "in": true, "this": true,
	"that": true, "these": true, "those": true, "review": true,
	"round": true, "runde": true, "here": true, "anymore": true,
	"current": true, "issues": true, "issue": true, "findings": true,
	"finding": true, "yet": true, "any": true, "so": true, "far": true,
	"befund": true, "befunde": true, "befunden": true, "dieser": true,
	"diese": true, "dieses": true,
}

// denialCoversLine reports whether the denial word ending at end (a match
// end from denialAfter) exhausts the line: only filler words or bare
// punctuation may follow.
func denialCoversLine(line string, end int) bool {
	for _, w := range strings.Fields(line[end:]) {
		clean := strings.ToLower(strings.Trim(w, ".,;:!?()[]\"'"))
		if clean == "" {
			continue
		}
		if !fillerAfter[clean] {
			return false
		}
	}
	return true
}

// isQuoted reports blockquote lines: they cite older text instead of
// stating a verdict, e.g. "> [P1] Historical issue already fixed".
func isQuoted(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), ">")
}

// ExtractFindings pulls P0/P1 verdicts out of Codex review comments, one
// finding per non-denied marker. A mixed line keeps its real verdicts
// while only the benign denial part is ignored: in "P1: timeout;
// P0: none found in this review" the P1 stays blocking and the denied
// P0 is dropped. Non-Codex comments never contribute, so human
// discussion cannot inject findings. Lines that merely talk about
// P0/P1 — weaker-severity titles (P2 and up, listed or not),
// negations, blockquotes, and questions — are prose, not verdicts, and
// are skipped.
func ExtractFindings(comments []ReviewComment) []Finding {
	var out []Finding
	for _, c := range comments {
		if !c.IsCodex {
			continue
		}
		for _, line := range strings.Split(c.Body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || isQuoted(line) {
				continue
			}
			if strings.Contains(trimmed, "?") {
				continue
			}
			out = append(out, extractLine(c.ID, line, trimmed)...)
		}
	}
	return out
}

// semiStart returns the start of the semicolon clause holding the marker
// at ms: ';' separates independent verdicts, while a comma does not
// break title scope ("P2: style nit, mentions P0 handling" stays fully
// skipped). A P2 title therefore governs its own clause only: in
// "P0: race; P2: style note mentioning P1" the P0 stays a verdict and
// the P1 inside the later P2-title clause is prose.
func semiStart(line string, ms int) int {
	for i := ms - 1; i >= 0; i-- {
		if line[i] == ';' {
			return i + 1
		}
	}
	return 0
}

// clauseStart returns the start of the denial clause holding the marker
// at ms: clauses split at ';' and ',', so a denial in one clause can
// never govern a marker in the next ("P0: none found; P1: timeout" keeps
// its P1), while a denial inside the clause still applies ("No P0,
// no P1" denies both).
func clauseStart(line string, ms int) int {
	for i := ms - 1; i >= 0; i-- {
		if line[i] == ';' || line[i] == ',' {
			return i + 1
		}
	}
	return 0
}

// extractLine evaluates every P0/P1 marker in one line on its own: a
// marker inside a weaker-severity title clause is prose, a marker
// preceded by a denial word inside its clause is denied, as is a marker
// whose denial word exhausts the line up to the next marker or the line
// end. Every surviving marker yields one finding.
func extractLine(commentID, line, trimmed string) []Finding {
	locs := severityMarker.FindAllStringSubmatchIndex(line, -1)
	var out []Finding
	for i, loc := range locs {
		// loc[0:2] span the whole match including the delimiter runes
		// the marker class consumes (":", "; ", "["); the marker
		// itself is the "P" just before group 1 plus the digit.
		ms, me := loc[2]-1, loc[3]
		if lowerSeverityTitle.MatchString(line[semiStart(line, ms):me]) {
			continue
		}
		if negationBefore.MatchString(line[clauseStart(line, ms):me]) {
			continue
		}
		after := line[ms:]
		if m := denialAfter.FindStringSubmatchIndex(after); m != nil && m[0] == 0 {
			rest := line[ms+m[1] : boundary(line, locs, i)]
			if denialCoversLine(rest, 0) {
				continue
			}
		}
		sev := SevP1
		if line[loc[2]:loc[3]] == "0" {
			sev = SevP0
		}
		out = append(out, Finding{CommentID: commentID, Severity: sev, Summary: trimmed})
	}
	return out
}

// boundary ends a marker's denial scope at the next marker or the line
// end, so "P0: none; P1: timeout" denies only the P0.
func boundary(line string, locs [][]int, i int) int {
	if i+1 < len(locs) {
		return locs[i+1][0]
	}
	return len(line)
}

// SummarizePoll aggregates one poll over Codex review comments plus check
// runs into a single outcome for the state machine.
func SummarizePoll(comments []ReviewComment, checks []CheckRun) PollOutcome {
	out := PollOutcome{
		Findings:     ExtractFindings(comments),
		Observed:     len(checks),
		CommentsSeen: len(comments),
	}
	for _, c := range checks {
		switch strings.ToLower(strings.TrimSpace(c.Status)) {
		case "completed", "complete", "done":
			switch strings.ToLower(strings.TrimSpace(c.Conclusion)) {
			case "success", "skipped", "neutral":
				continue
			default:
				out.Failed = append(out.Failed, c.Name)
			}
		default:
			out.Pending = append(out.Pending, c.Name)
		}
	}
	return out
}

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
}

// severityMarker matches P0/P1 markers in the forms reviewers use: P0,
// P1, [P0], (P1), "P0:" — case-insensitive, word-bounded so P10 or
// AP01 never match.
var severityMarker = regexp.MustCompile(`(?i)(?:^|[\s\[\(.:;-])P([01])(?:$|[\s\]\)}.,:;-])`)

// HasBlocking reports whether the outcome carries P0/P1 findings that
// require a triage proposal.
func (o PollOutcome) HasBlocking() bool { return len(o.Findings) > 0 }

// P0Count counts P0 findings in the outcome.
func (o PollOutcome) P0Count() int { return countSev(o.Findings, SevP0) }

// P1Count counts P1 findings in the outcome.
func (o PollOutcome) P1Count() int { return countSev(o.Findings, SevP1) }

// AllGreen reports whether the PR needs nothing: no P0/P1 findings, no
// failed checks, and no pending checks. Only an all-green poll may draft
// a closeout note.
func (o PollOutcome) AllGreen() bool {
	return len(o.Findings) == 0 && len(o.Failed) == 0 && len(o.Pending) == 0
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

// ExtractFindings pulls P0/P1 findings out of Codex review comments, one
// finding per marker-bearing line. Non-Codex comments never contribute,
// so human discussion cannot inject findings.
func ExtractFindings(comments []ReviewComment) []Finding {
	var out []Finding
	for _, c := range comments {
		if !c.IsCodex {
			continue
		}
		for _, line := range strings.Split(c.Body, "\n") {
			m := severityMarker.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			sev := SevP1
			if m[1] == "0" {
				sev = SevP0
			}
			summary := strings.TrimSpace(line)
			if summary == "" {
				continue
			}
			out = append(out, Finding{CommentID: c.ID, Severity: sev, Summary: summary})
		}
	}
	return out
}

// SummarizePoll aggregates one poll over Codex review comments plus check
// runs into a single outcome for the state machine.
func SummarizePoll(comments []ReviewComment, checks []CheckRun) PollOutcome {
	out := PollOutcome{Findings: ExtractFindings(comments)}
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

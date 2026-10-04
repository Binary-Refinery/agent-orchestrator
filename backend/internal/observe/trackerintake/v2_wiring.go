// V2 daemon wiring: opt-in, default-off, fail-closed callers that connect the
// pure V2 decision core (policy_v2.go) to real daemon take paths.
//
// Red lines (never automated here): auto-push, auto-merge, task-worker
// auto-terminate, finding auto-confirm, publish-go automation.
package trackerintake

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// V2Audit is the minimal audit surface: every denial is logged.
type V2Audit struct {
	Logger *slog.Logger
}

func (a V2Audit) log(ctx context.Context, reason V2RejectReason, msg string, args ...any) {
	l := a.Logger
	if l == nil {
		l = slog.Default()
	}
	l.WarnContext(ctx, "v2 deny: "+msg, append([]any{"reason", string(reason)}, args...)...)
}

// V2TakeCaller is the Take-path WIP-gate caller: queue head only,
// maxParallel cap, stall timeout. Typed denial, audit-logged.
func V2TakeCaller(ctx context.Context, queue []V2Take, candidate V2Take, active []V2ActiveTake, cfg domain.TrackerIntakeConfig, audit V2Audit) V2Decision {
	d := V2WIPGate(queue, candidate, active, cfg)
	if !d.Allow {
		audit.log(ctx, d.Reason, "wip gate", "detail", d.Detail, "issue", string(candidate.IssueID))
	}
	return d
}

// V2ReasonFactsUnavailable is the typed fail-closed refusal when required
// preflight facts cannot be established. The take path denies rather than
// passing on assumed facts.
const V2ReasonFactsUnavailable V2RejectReason = "v2_facts_unavailable"

// V2PreflightFacts are the daemon facts a pre-take check requires. They are
// supplied by the Store layer; absence of any required fact denies the take.
type V2PreflightFacts struct {
	HarnessReady    bool
	RepoClean       bool
	BudgetRemaining int
	// BranchFiles maps active worker branch -> touched paths for the
	// overlap-suspect check. Nil means no branch facts are known.
	BranchFiles map[string][]string
	// CandidateFiles are paths the candidate take would touch. Empty means
	// no suspects are knowable pre-spawn (honest empty, not an assumption).
	CandidateFiles []string
}

// V2FactProvider is the optional Store surface for pre-take facts. Stores
// that do not implement it cannot establish preflight facts, so the take
// path denies fail-closed with V2ReasonFactsUnavailable.
type V2FactProvider interface {
	V2PreflightFacts(ctx context.Context, projectID string) (V2PreflightFacts, bool, error)
}

// V2ProbeFromFacts builds the pre-take probe from established facts only.
// Base resolvability is proven by the caller (tracker scope resolved).
func V2ProbeFromFacts(facts V2PreflightFacts, baseResolvable bool) V2PreflightProbe {
	return V2PreflightProbe{
		BaseResolvable:    baseResolvable,
		HarnessReady:      facts.HarnessReady,
		RepoClean:         facts.RepoClean,
		BudgetRemaining:   facts.BudgetRemaining,
		CandidateFiles:    facts.CandidateFiles,
		ActiveBranchFiles: facts.BranchFiles,
	}
}

// V2PreflightProbe carries daemon facts for the pre-take check.
type V2PreflightProbe struct {
	BaseResolvable  bool
	HarnessReady    bool
	RepoClean       bool
	BudgetRemaining int
	// CandidateFiles are paths the candidate take would touch.
	CandidateFiles []string
	// ActiveBranchFiles maps active worker branch -> touched paths.
	ActiveBranchFiles map[string][]string
}

// V2OverlapSuspects intersects candidate files with active worker-branch
// files (case-insensitive, path-normalized). Returned sorted, deduplicated.
func V2OverlapSuspects(candidate []string, active map[string][]string) []string {
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	inActive := map[string]bool{}
	for _, files := range active {
		for _, f := range files {
			if n := norm(f); n != "" {
				inActive[n] = true
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range candidate {
		n := norm(f)
		if n == "" || seen[n] || !inActive[n] {
			continue
		}
		seen[n] = true
		out = append(out, strings.TrimSpace(f))
	}
	sort.Strings(out)
	return out
}

// V2PreflightCaller runs the pre-take checks at the Take path. File overlap
// with active worker branches defers as suspect only — never a hard lock
// without a human.
func V2PreflightCaller(ctx context.Context, probe V2PreflightProbe, cfg domain.TrackerIntakeConfig, audit V2Audit) V2Decision {
	in := V2PreflightInput{
		BaseResolvable:  probe.BaseResolvable,
		HarnessReady:    probe.HarnessReady,
		RepoClean:       probe.RepoClean,
		BudgetRemaining: probe.BudgetRemaining,
		OverlapSuspects: V2OverlapSuspects(probe.CandidateFiles, probe.ActiveBranchFiles),
	}
	d := V2Preflight(in, cfg)
	if !d.Allow {
		audit.log(ctx, d.Reason, "preflight", "detail", d.Detail)
	}
	return d
}

// V2Queue is the in-memory FIFO queue fed by label-event polling.
// No webhooks, no ingress: entries arrive only via V2PollFeed.
type V2Queue struct {
	takes []V2Take
	index map[domain.IssueID]int
}

// V2PollFeedResult reports one polling pass over tracker issues.
type V2PollFeedResult struct {
	Admitted     int
	Disqualified int
	Denied       []V2Decision
}

// V2PollFeed polls open issues for the configured label event and feeds the
// FIFO queue. Label removal disqualifies a queued entry. Empty required label
// admits nothing (fail-closed). Poll cadence is the observer Tick (N minutes,
// configurable); this function is one synchronous pass.
func (q *V2Queue) V2PollFeed(ctx context.Context, issues []domain.Issue, cfg domain.TrackerIntakeConfig, audit V2Audit) V2PollFeedResult {
	var res V2PollFeedResult
	if q.index == nil {
		q.index = map[domain.IssueID]int{}
	}
	if !V2Enabled(cfg) {
		d := v2Deny(V2ReasonDisabled, "v2 policy off")
		audit.log(ctx, d.Reason, "feed skipped", "detail", d.Detail)
		res.Denied = append(res.Denied, d)
		return res
	}
	want := ""
	if cfg.AutomationV2 != nil {
		want = strings.TrimSpace(cfg.AutomationV2.Label)
	}
	if want == "" {
		d := v2Deny(V2ReasonNoLabel, "v2 requires a configured label; none set")
		audit.log(ctx, d.Reason, "feed admits nothing", "detail", d.Detail)
		res.Denied = append(res.Denied, d)
		return res
	}
	labeled := map[domain.IssueID]bool{}
	for _, issue := range issues {
		if ctx.Err() != nil {
			d := v2Deny(V2ReasonNoLabel, "context cancelled; fail-closed")
			res.Denied = append(res.Denied, d)
			return res
		}
		if issue.State != domain.IssueOpen {
			continue
		}
		d := V2IssueAdmitted(issue, cfg)
		if !d.Allow {
			continue
		}
		id := CanonicalIssueID(issue.ID)
		if id == "" {
			continue
		}
		labeled[id] = true
		if _, ok := q.index[id]; ok {
			continue
		}
		q.takes = append(q.takes, V2Take{IssueID: id})
		q.index[id] = len(q.takes) - 1
		res.Admitted++
	}
	// Label removal disqualifies: drop queued takes no longer labeled.
	kept := q.takes[:0]
	for _, t := range q.takes {
		if labeled[t.IssueID] {
			kept = append(kept, t)
			continue
		}
		delete(q.index, t.IssueID)
		res.Disqualified++
		audit.log(ctx, V2ReasonNoLabel, "disqualified on label removal", "issue", string(t.IssueID))
	}
	q.takes = kept
	q.index = map[domain.IssueID]int{}
	for i, t := range q.takes {
		q.index[t.IssueID] = i
	}
	return res
}

// Takes returns the FIFO-ordered queue snapshot.
func (q *V2Queue) Takes() []V2Take {
	out := make([]V2Take, len(q.takes))
	copy(out, q.takes)
	return out
}

// Clear drops all queued takes. Used fail-closed when a feed pass is denied.
func (q *V2Queue) Clear() {
	q.takes = nil
	q.index = map[domain.IssueID]int{}
}

// PopHead removes and returns the queue head for a completed take.
func (q *V2Queue) PopHead() (V2Take, bool) {
	if len(q.takes) == 0 {
		return V2Take{}, false
	}
	head := q.takes[0]
	q.takes = append([]V2Take(nil), q.takes[1:]...)
	q.index = map[domain.IssueID]int{}
	for i, t := range q.takes {
		q.index[t.IssueID] = i
	}
	return head, true
}

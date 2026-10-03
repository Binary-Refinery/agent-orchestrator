package trackerintake

import (
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// V2RejectReason is the typed, fail-closed reason every v2 refusal carries.
type V2RejectReason string

const (
	V2ReasonDisabled       V2RejectReason = "v2_disabled"
	V2ReasonNoLabel        V2RejectReason = "v2_no_label"
	V2ReasonWIPFull        V2RejectReason = "v2_wip_full"
	V2ReasonReviewStalled  V2RejectReason = "v2_review_stalled"
	V2ReasonNotFirst       V2RejectReason = "v2_not_first_in_queue"
	V2ReasonFileOverlap    V2RejectReason = "v2_file_overlap_suspect"
	V2ReasonBaseUnresolved V2RejectReason = "v2_base_unresolved"
	V2ReasonHarnessMissing V2RejectReason = "v2_harness_not_ready"
	V2ReasonRepoDirty      V2RejectReason = "v2_repo_dirty"
	V2ReasonBudget         V2RejectReason = "v2_budget_exhausted"
)

// V2Decision is the fail-closed outcome of one v2 policy check.
type V2Decision struct {
	Allow  bool
	Reason V2RejectReason
	Detail string
}

func v2Allow() V2Decision { return V2Decision{Allow: true} }

func v2Deny(reason V2RejectReason, detail string) V2Decision {
	return V2Decision{Reason: reason, Detail: detail}
}

// V2Enabled reports whether the opt-in v2 policy applies.
func V2Enabled(cfg domain.TrackerIntakeConfig) bool {
	return cfg.AutomationV2 != nil && cfg.AutomationV2.Enabled
}

// V2IssueAdmitted implements the intake rule: with v2 enabled, an issue joins
// the AO queue only when it carries the configured label event. AO never
// selects a label itself: an empty required label admits nothing.
func V2IssueAdmitted(issue domain.Issue, cfg domain.TrackerIntakeConfig) V2Decision {
	if !V2Enabled(cfg) {
		return v2Deny(V2ReasonDisabled, "v2 policy off")
	}
	want := strings.TrimSpace(cfg.AutomationV2.Label)
	if want == "" {
		return v2Deny(V2ReasonNoLabel, "v2 requires a configured label; none set")
	}
	for _, l := range issue.Labels {
		if strings.EqualFold(strings.TrimSpace(l), want) {
			return v2Allow()
		}
	}
	return v2Deny(V2ReasonNoLabel, "issue lacks required label "+want)
}

// V2Take describes one queued candidate in FIFO order (index 0 = head).
type V2Take struct {
	IssueNumber int
	IssueID     domain.IssueID
}

// V2ActiveTake is the minimal durable fact the WIP-gate needs per active session.
type V2ActiveTake struct {
	Stalled bool // in needs_review/CI-failing
	// StalledFor is how long this take has been stalled (zero when not stalled).
	StalledFor time.Duration
}

// V2WIPGate admits only the queue head, only when active takes are below
// maxParallel and no take is stalled past the timeout. Strict FIFO.
func V2WIPGate(queue []V2Take, candidate V2Take, active []V2ActiveTake, cfg domain.TrackerIntakeConfig) V2Decision {
	if !V2Enabled(cfg) {
		return v2Deny(V2ReasonDisabled, "v2 policy off")
	}
	if len(queue) == 0 {
		return v2Deny(V2ReasonNotFirst, "empty queue admits nothing")
	}
	if queue[0].IssueID != candidate.IssueID {
		return v2Deny(V2ReasonNotFirst, "strict FIFO: queue head goes first")
	}
	timeout := time.Duration(cfg.AutomationV2.ResolvedReviewTimeoutSeconds()) * time.Second
	for _, a := range active {
		if a.Stalled && a.StalledFor >= timeout {
			return v2Deny(V2ReasonReviewStalled, "stalled needs_review/CI-failing take blocks new takes")
		}
	}
	if len(active) >= cfg.AutomationV2.ResolvedMaxParallel() {
		return v2Deny(V2ReasonWIPFull, "active takes at maxParallel")
	}
	return v2Allow()
}

// V2PreflightInput carries the pre-take checks. Suspected file overlap with an
// active worker branch defers (never hard-locks without a human).
type V2PreflightInput struct {
	BaseResolvable bool
	HarnessReady   bool
	RepoClean      bool
	// OverlapSuspects lists files the candidate touches that an active worker
	// branch also touches. Non-empty defers with reason.
	OverlapSuspects []string
	// BudgetRemaining is the remaining take budget for this window; <1 refuses.
	BudgetRemaining int
}

// V2Preflight runs the pre-take checks in spec order.
func V2Preflight(in V2PreflightInput, cfg domain.TrackerIntakeConfig) V2Decision {
	if !V2Enabled(cfg) {
		return v2Deny(V2ReasonDisabled, "v2 policy off")
	}
	if in.BudgetRemaining < 1 {
		return v2Deny(V2ReasonBudget, "take budget exhausted")
	}
	if !in.BaseResolvable {
		return v2Deny(V2ReasonBaseUnresolved, "base branch not resolvable")
	}
	if !in.HarnessReady {
		return v2Deny(V2ReasonHarnessMissing, "harness not ready")
	}
	if !in.RepoClean {
		return v2Deny(V2ReasonRepoDirty, "repo not clean")
	}
	if len(in.OverlapSuspects) > 0 {
		return v2Deny(V2ReasonFileOverlap, "file overlap suspect: "+strings.Join(in.OverlapSuspects, ", "))
	}
	return v2Allow()
}

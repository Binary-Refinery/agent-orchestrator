// Production binding for the reportsync policy (Slice 3, P1-1).
//
// Runner is the production path that turns the opt-in policy into tracker
// comments plus board status updates. It only emits what PlanSync allows:
// fact-only comments and status updates. There is no push, merge,
// terminate, confirm, or publish path by design.
package reportsync

import (
	"context"
)

// CommentPoster posts fact comments to the tracker. Implementations reuse
// existing gh auth (Token path A); this package never takes, logs, or
// stores tokens.
type CommentPoster interface {
	PostComment(ctx context.Context, repo string, issue int, body string) error
}

// BoardUpdater applies board status updates.
type BoardUpdater interface {
	SetStatus(ctx context.Context, repo string, issue int, status string) error
}

// Runner binds the opt-in policy switch to a tracker and a board. The zero
// value is disabled and dependency-free: every method fails closed.
type Runner struct {
	Enabled bool
	Tracker CommentPoster
	Board   BoardUpdater
}

// ValidateReport is the production draft gate: it applies ValidateDraft
// under the runner's opt-in switch.
func (r *Runner) ValidateReport(draft string) Decision {
	if r == nil || !r.Enabled {
		return deny(ReasonDisabled, "report-sync policy off")
	}
	return ValidateDraft(true, draft)
}

// SyncPhase plans one phase event via PlanSync and, only on Allow, posts
// the FAKTEN comment and updates the board status, then records the event
// in the caller's Seen store. Disabled runners, missing dependencies, a
// missing Seen store, denied plans, and post failures all leave the
// tracker and the board untouched.
func (r *Runner) SyncPhase(ctx context.Context, repo string, issue int, in SyncInput) Decision {
	if r == nil || !r.Enabled {
		return deny(ReasonDisabled, "report-sync policy off")
	}
	if r.Tracker == nil || r.Board == nil {
		return deny(ReasonNoDeps, "tracker/board wiring missing, refusing")
	}
	if in.Seen == nil {
		return deny(ReasonNoDeps, "seen store missing, refusing to bypass dedup")
	}
	in.Repo = repo
	action, d := PlanSync(true, in)
	if !d.Allow {
		return d
	}
	if err := r.Tracker.PostComment(ctx, repo, issue, action.Comment); err != nil {
		return deny(ReasonPostFailed, "comment post failed")
	}
	if err := r.Board.SetStatus(ctx, repo, issue, action.BoardStatus); err != nil {
		return deny(ReasonPostFailed, "board update failed")
	}
	in.Seen[in.SeenKey] = true
	return allow()
}

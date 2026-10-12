package review

import "github.com/aoagents/agent-orchestrator/backend/internal/domain"

// Local targets retain an empty PR reference. The immutable work phase scope,
// worker and SHA supply identity; no fixture URL or provider artifact is created.
func planLocalReview(sha string, runs []domain.ReviewRun, harness domain.ReviewerHarness) []PRReviewState {
	state := PRReviewState{Title: "Local governed commit", TargetSHA: sha, Status: ReviewStateNeedsReview}
	for _, run := range runs {
		if run.PRURL != "" || run.TargetSHA != sha || run.Harness != harness {
			continue
		}
		if state.LatestRun == nil || run.CreatedAt.After(state.LatestRun.CreatedAt) {
			latest := run
			state.LatestRun = &latest
		}
	}
	if run := state.LatestRun; run != nil {
		switch {
		case run.Status == domain.ReviewRunRunning:
			state.Status = ReviewStateRunning
		case run.Verdict == domain.VerdictApproved:
			state.Status = ReviewStateUpToDate
		case run.Verdict == domain.VerdictChangesRequested:
			state.Status = ReviewStateChangesRequested
		}
	}
	return []PRReviewState{state}
}

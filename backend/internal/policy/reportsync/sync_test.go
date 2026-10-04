// External production-path regression for Slice 3 (P1-1): the Runner binds
// the opt-in switch to a tracker and a board. An enabled runner turns a
// phase event into a FAKTEN comment plus a board status; a disabled runner
// or a denied plan touches neither. No real tokens appear anywhere here:
// all facts are synthetic non-secret placeholders.
package reportsync_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/policy/reportsync"
)

type fakeTracker struct {
	posts []string
	err   error
}

func (f *fakeTracker) PostComment(_ context.Context, _ string, _ int, body string) error {
	if f.err != nil {
		return f.err
	}
	f.posts = append(f.posts, body)
	return nil
}

type fakeBoard struct {
	statuses []string
}

func (f *fakeBoard) SetStatus(_ context.Context, _ string, _ int, status string) error {
	f.statuses = append(f.statuses, status)
	return nil
}

func TestRunnerSyncPhasePostsCommentAndStatus(t *testing.T) {
	tracker := &fakeTracker{}
	board := &fakeBoard{}
	r := &reportsync.Runner{Enabled: true, Tracker: tracker, Board: board}
	seen := map[string]bool{}
	d := r.SyncPhase(context.Background(), reportsync.PilotRepos[0], 7, reportsync.SyncInput{
		Phase:   reportsync.PhaseStarted,
		Facts:   []string{"sha abc123"},
		HasAuth: true,
		Seen:    seen,
		SeenKey: "7:gestartet",
	})
	if !d.Allow {
		t.Fatalf("enabled run denied: %+v", d)
	}
	if len(tracker.posts) != 1 || !strings.HasPrefix(tracker.posts[0], "FAKTEN") {
		t.Fatalf("tracker posts = %v, want one FAKTEN comment", tracker.posts)
	}
	if len(board.statuses) != 1 || board.statuses[0] == "" {
		t.Fatalf("board statuses = %v, want one status update", board.statuses)
	}
	if !seen["7:gestartet"] {
		t.Fatal("event not recorded in seen store")
	}
	// Replay is deduped: no second posting, no retry spam.
	if d := r.SyncPhase(context.Background(), reportsync.PilotRepos[0], 7, reportsync.SyncInput{
		Phase:   reportsync.PhaseStarted,
		Facts:   []string{"sha abc123"},
		HasAuth: true,
		Seen:    seen,
		SeenKey: "7:gestartet",
	}); d.Allow || d.Reason != reportsync.ReasonDuplicate {
		t.Fatalf("replay = %+v, want duplicate deny", d)
	}
	if len(tracker.posts) != 1 || len(board.statuses) != 1 {
		t.Fatalf("replay posted: tracker=%v board=%v", tracker.posts, board.statuses)
	}
}

func TestRunnerDisabledPostsNothing(t *testing.T) {
	tracker := &fakeTracker{}
	board := &fakeBoard{}
	r := &reportsync.Runner{Tracker: tracker, Board: board} // Enabled defaults false
	d := r.SyncPhase(context.Background(), reportsync.PilotRepos[0], 7, reportsync.SyncInput{
		Phase:   reportsync.PhaseCommitted,
		Facts:   []string{"sha abc123"},
		HasAuth: true,
		Seen:    map[string]bool{},
		SeenKey: "7:committed",
	})
	if d.Allow || d.Reason != reportsync.ReasonDisabled {
		t.Fatalf("disabled run = %+v, want disabled deny", d)
	}
	if len(tracker.posts) != 0 || len(board.statuses) != 0 {
		t.Fatalf("disabled runner posted: tracker=%v board=%v", tracker.posts, board.statuses)
	}
}

func TestRunnerDeniedPlanPostsNothing(t *testing.T) {
	tracker := &fakeTracker{}
	board := &fakeBoard{}
	r := &reportsync.Runner{Enabled: true, Tracker: tracker, Board: board}
	// No facts: fail-closed, nothing posted.
	d := r.SyncPhase(context.Background(), reportsync.PilotRepos[0], 7, reportsync.SyncInput{
		Phase:   reportsync.PhaseCIResult,
		HasAuth: true,
		Seen:    map[string]bool{},
		SeenKey: "7:ci",
	})
	if d.Allow || d.Reason != reportsync.ReasonUngrounded {
		t.Fatalf("factless run = %+v, want ungrounded deny", d)
	}
	// Missing dependencies: fail-closed, nothing posted.
	bare := &reportsync.Runner{Enabled: true}
	if d := bare.SyncPhase(context.Background(), reportsync.PilotRepos[0], 7, reportsync.SyncInput{
		Phase:   reportsync.PhaseStarted,
		Facts:   []string{"sha abc123"},
		HasAuth: true,
		Seen:    map[string]bool{},
		SeenKey: "7:gestartet",
	}); d.Allow || d.Reason != reportsync.ReasonNoDeps {
		t.Fatalf("depless run = %+v, want missing_deps deny", d)
	}
	if len(tracker.posts) != 0 || len(board.statuses) != 0 {
		t.Fatalf("denied plans posted: tracker=%v board=%v", tracker.posts, board.statuses)
	}
}

func TestRunnerValidateReportUsesSwitch(t *testing.T) {
	on := &reportsync.Runner{Enabled: true}
	draft := "VERLAUF\na\nSTAND\nb\nPROBLEM/BEFUND\nc\nBELEG\nd\nOFFEN\ne\nNAECHSTE SCHRITTE\nf\nGATE\nSoll ich den PR mergen?"
	if d := on.ValidateReport(draft); !d.Allow {
		t.Fatalf("enabled validation denied: %+v", d)
	}
	off := &reportsync.Runner{}
	if d := off.ValidateReport(draft); d.Allow || d.Reason != reportsync.ReasonDisabled {
		t.Fatalf("disabled validation = %+v, want disabled deny", d)
	}
}

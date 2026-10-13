package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestStatusReadinessWaitsForRecoveryAndAllowsRetry(t *testing.T) {
	m, st, rt, _ := newManager()
	rec := domain.SessionRecord{ID: "s1", ProjectID: "mer", Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: time.Unix(100, 0)},
		Metadata: domain.SessionMetadata{Branch: "ao/s1", WorkspacePath: "/wt/s1", RuntimeHandleID: "s1"}}
	st.sessions[rec.ID] = rec
	if got := m.SessionStatusReadiness(rec); got != "checking" {
		t.Fatalf("before recovery = %s", got)
	}
	rt.aliveErr = errors.New("runtime probe unavailable")
	if err := m.ReconcileBackground(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.SessionStatusReadiness(rec); got != "unavailable" {
		t.Fatalf("failed probe = %s", got)
	}
	if st.sessions[rec.ID].Activity != rec.Activity {
		t.Fatal("failed probe changed activity")
	}
	rt.aliveErr = nil
	rt.aliveByHandle = map[string]bool{"s1": true}
	if _, err := m.ResumeAgentWithMode(context.Background(), rec.ID); err != nil {
		t.Fatal(err)
	}
	if got := m.SessionStatusReadiness(st.sessions[rec.ID]); got != "ready" {
		t.Fatalf("retry = %s", got)
	}
	if rt.created != 0 {
		t.Fatal("retry spawned a duplicate of a surviving runtime")
	}
}

type stubbornAliveRuntime struct {
	*fakeRuntime
	entered chan domain.SessionID
	release chan struct{}
}

func (r *stubbornAliveRuntime) IsAlive(_ context.Context, handle ports.RuntimeHandle) (bool, error) {
	r.entered <- domain.SessionID(handle.ID)
	<-r.release
	return true, nil
}

func TestStatusReadinessDoesNotOfferRetryWhileRecoveryOwnsSession(t *testing.T) {
	m, st, rt, _ := newManager()
	rec := domain.SessionRecord{ID: "s1", ProjectID: "mer", Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive},
		Metadata: domain.SessionMetadata{Branch: "ao/s1", WorkspacePath: "/wt/s1", RuntimeHandleID: "s1"}}
	st.sessions[rec.ID] = rec
	blocked := &stubbornAliveRuntime{fakeRuntime: rt, entered: make(chan domain.SessionID, 1), release: make(chan struct{})}
	m.runtime = blocked
	m.statusVerificationLimit = time.Nanosecond
	finished := make(chan error, 1)
	go func() { finished <- m.ReconcileBackground(context.Background()) }()
	<-blocked.entered
	time.Sleep(time.Millisecond)
	if got := m.SessionStatusReadiness(rec); got != "checking" {
		t.Fatalf("owned recovery = %s, want checking until retry can acquire the session", got)
	}
	close(blocked.release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if got := m.SessionStatusReadiness(rec); got != "ready" {
		t.Fatalf("late recovery = %s", got)
	}
	if st.sessions[rec.ID].Activity != rec.Activity {
		t.Fatal("recovery invented activity")
	}
}

type deadlineAwareRuntime struct {
	*fakeRuntime
	entered chan struct{}
}

func (r *deadlineAwareRuntime) IsAlive(ctx context.Context, _ ports.RuntimeHandle) (bool, error) {
	close(r.entered)
	<-ctx.Done()
	return false, ctx.Err()
}

func TestStatusReadinessDeadlineReleasesSessionForRetry(t *testing.T) {
	m, st, rt, _ := newManager()
	rec := domain.SessionRecord{ID: "s1", ProjectID: "mer", Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive},
		Metadata: domain.SessionMetadata{Branch: "ao/s1", WorkspacePath: "/wt/s1", RuntimeHandleID: "s1"}}
	st.sessions[rec.ID] = rec
	deadlineRuntime := &deadlineAwareRuntime{fakeRuntime: rt, entered: make(chan struct{})}
	m.runtime = deadlineRuntime
	m.statusVerificationLimit = 10 * time.Millisecond
	finished := make(chan error, 1)
	go func() { finished <- m.ReconcileBackground(context.Background()) }()
	<-deadlineRuntime.entered
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if got := m.SessionStatusReadiness(rec); got != "unavailable" {
		t.Fatalf("deadline = %s, want unavailable", got)
	}
	rt.aliveByHandle = map[string]bool{"s1": true}
	m.runtime = rt
	if _, err := m.ResumeAgentWithMode(context.Background(), rec.ID); err != nil {
		t.Fatalf("retry after deadline: %v", err)
	}
	if got := m.SessionStatusReadiness(st.sessions[rec.ID]); got != "ready" {
		t.Fatalf("retry = %s, want ready", got)
	}
}

func TestStatusReadinessDiscoveryFailureIsUnavailable(t *testing.T) {
	m, st, _, _ := newManager()
	st.listAllErr = errors.New("storage unavailable")
	if err := m.ReconcileBackground(context.Background()); err == nil {
		t.Fatal("expected discovery failure")
	}
	if got := m.SessionStatusReadiness(domain.SessionRecord{ID: "s1"}); got != "unavailable" {
		t.Fatalf("readiness = %s", got)
	}
}

func TestStatusReadinessFreshSpawnAfterDiscoveryFailureIsReady(t *testing.T) {
	m, st, _, _ := newManager()
	st.listAllErr = errors.New("storage unavailable")
	if err := m.ReconcileBackground(context.Background()); err == nil {
		t.Fatal("expected discovery failure")
	}
	st.listAllErr = nil
	rec, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "mer",
		Kind:      domain.KindWorker,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.SessionStatusReadiness(rec); got != "ready" {
		t.Fatalf("fresh spawn readiness = %s, want ready", got)
	}
}

func TestStatusReadinessFreshSpawnAfterSuccessfulRecoveryDoesNotChangeRevision(t *testing.T) {
	m, _, _, _ := newManager()
	if err := m.ReconcileBackground(context.Background()); err != nil {
		t.Fatal(err)
	}
	revision := m.StatusRecoveryRevision()
	rec, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "mer",
		Kind:      domain.KindWorker,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.StatusRecoveryRevision(); got != revision {
		t.Fatalf("fresh spawn recovery revision = %d, want %d", got, revision)
	}
	if got := m.SessionStatusReadiness(rec); got != "ready" {
		t.Fatalf("fresh spawn readiness = %s, want ready", got)
	}
}

func TestStatusRecoveryReasonExpiresWithOwnerAndRetry(t *testing.T) {
	for _, replacedBeforeFinish := range []bool{false, true} {
		t.Run(fmt.Sprint(replacedBeforeFinish), func(t *testing.T) {
			m, st, _, _ := newManager()
			rec := domain.SessionRecord{ID: "s1", ProjectID: "mer", Mode: domain.SessionModeChat,
				Activity: domain.Activity{State: domain.ActivityActive},
				Metadata: domain.SessionMetadata{ControllerGeneration: "original"}}
			st.sessions[rec.ID] = rec
			m.beginStatusRecovery(rec.ID)
			if code, reason := m.SessionStatusRecoveryReason(rec); code != "" || reason != "" {
				t.Fatalf("pending recovery published a reason: %q %q", code, reason)
			}
			replacement := rec
			replacement.Metadata.ControllerGeneration = "replacement"
			if replacedBeforeFinish {
				st.sessions[rec.ID] = replacement
			}
			m.finishStatusRecovery(context.Background(), rec, fmt.Errorf("%w: work is waiting_human", ErrInstructorResumeBlocked))
			if code, reason := m.SessionStatusRecoveryReason(rec); code != "INSTRUCTOR_RESUME_BLOCKED" || !strings.Contains(reason, "waiting_human") {
				t.Fatalf("failed recovery cause = %q %q", code, reason)
			}
			if code, reason := m.SessionStatusRecoveryReason(replacement); code != "" || reason != "" {
				t.Fatalf("new controller inherited old failure: %q %q", code, reason)
			}
			if got := m.SessionStatusReadiness(replacement); got != "ready" {
				t.Fatalf("new controller readiness = %s", got)
			}
			m.beginStatusRecovery(rec.ID)
			if code, reason := m.SessionStatusRecoveryReason(rec); code != "" || reason != "" {
				t.Fatalf("retry retained old failure: %q %q", code, reason)
			}
			m.finishStatusRecovery(context.Background(), rec, nil)
			if code, reason := m.SessionStatusRecoveryReason(rec); code != "" || reason != "" {
				t.Fatalf("successful recovery retained cause: %q %q", code, reason)
			}
		})
	}
}

func TestStatusRecoveryReasonIsBoundedAndPreservesOwnerUncertainty(t *testing.T) {
	m, st, _, _ := newManager()
	rec := domain.SessionRecord{ID: "s1", Activity: domain.Activity{State: domain.ActivityActive}}
	st.sessions[rec.ID] = rec
	cause := errors.Join(ports.ErrChatRecoveryInconclusive, fmt.Errorf("history: %w\n%s", ports.ErrChatHistoryUnsettled, strings.Repeat("ä", 1000)))
	m.finishStatusRecovery(context.Background(), rec, cause)
	code, reason := m.SessionStatusRecoveryReason(rec)
	if code != "CHAT_RECOVERY_INCONCLUSIVE" || !strings.Contains(reason, "may still own work") {
		t.Fatalf("recovery cause = %q %q", code, reason)
	}
	if len([]rune(reason)) > 512 || strings.ContainsAny(reason, "\n\r") || strings.ContainsRune(reason, '\uFFFD') {
		t.Fatalf("unbounded or invalid explanation: %q", reason)
	}
	if st.sessions[rec.ID].Activity != rec.Activity {
		t.Fatal("unknown ownership changed durable activity")
	}
}

func TestStatusRecoveryReasonPreservesFailedRelaunchCauseForExitedSession(t *testing.T) {
	m, st, _, _ := newManager()
	rec := domain.SessionRecord{ID: "s1", Activity: domain.Activity{State: domain.ActivityActive}}
	st.sessions[rec.ID] = rec
	m.beginStatusRecovery(rec.ID)
	exited := rec
	exited.Activity.State = domain.ActivityExited
	st.sessions[rec.ID] = exited
	m.finishStatusRecovery(context.Background(), rec, fmt.Errorf("%w: role requires harness opencode, got codex", ErrInstructorResumeBlocked))
	if got := m.SessionStatusReadiness(exited); got != "ready" {
		t.Fatalf("verified exited status = %q", got)
	}
	if code, reason := m.SessionStatusRecoveryReason(exited); code != "INSTRUCTOR_RESUME_BLOCKED" || !strings.Contains(reason, "role requires harness opencode, got codex") {
		t.Fatalf("exited session recovery cause = %q %q", code, reason)
	}
	exited.IsTerminated = true
	if code, reason := m.SessionStatusRecoveryReason(exited); code != "" || reason != "" {
		t.Fatalf("terminated session retained recovery cause = %q %q", code, reason)
	}
}

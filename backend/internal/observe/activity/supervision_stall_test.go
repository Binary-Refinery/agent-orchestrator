package activity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/supervision"
)

var errStallProbe = errors.New("terminal output unavailable")

type fakeStallSupervisor struct {
	calls []stallCall
	check supervision.StallCheck
}

type stallCall struct {
	id    domain.SessionID
	since time.Duration
}

func (f *fakeStallSupervisor) SupervisionStallCheck(_ context.Context, id domain.SessionID, _ domain.ProjectID, since time.Duration) supervision.StallCheck {
	f.calls = append(f.calls, stallCall{id: id, since: since})
	if f.check.Decision.Allow || f.check.Outcome != "" {
		return f.check
	}
	return supervision.StallCheck{Outcome: supervision.OutcomeQuiet}
}

func TestPollDrivesSupervisionStallPassWhenWired(t *testing.T) {
	now := time.Unix(500, 0).UTC()
	session := activeSession(now, domain.HarnessCodex)
	session.ProjectID = "mer"
	sup := &fakeStallSupervisor{}
	observer := New(
		fakeSessions{rows: []domain.SessionRecord{session}},
		&fakeSink{},
		&fakeRuntime{err: errStallProbe},
		fakeAgents{},
		Config{Clock: func() time.Time { return now }, Logger: testLogger()},
	)
	observer.SetStallSupervisor(sup)

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sup.calls) != 1 || sup.calls[0].id != session.ID {
		t.Fatalf("stall calls = %+v, want exactly one for %q", sup.calls, session.ID)
	}
	if want := 3 * time.Minute; sup.calls[0].since != want {
		t.Fatalf("stall since = %v, want %v", sup.calls[0].since, want)
	}
}

func TestPollStallPassOffByDefault(t *testing.T) {
	now := time.Unix(500, 0).UTC()
	observer := New(
		fakeSessions{rows: []domain.SessionRecord{activeSession(now, domain.HarnessCodex)}},
		&fakeSink{},
		&fakeRuntime{err: errStallProbe},
		fakeAgents{},
		Config{Clock: func() time.Time { return now }, Logger: testLogger()},
	)
	// No supervisor wired: the production default stays silent.
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPollStallPassSkipsTerminatedAndProgressless(t *testing.T) {
	now := time.Unix(500, 0).UTC()
	terminated := activeSession(now, domain.HarnessCodex)
	terminated.IsTerminated = true
	progressless := activeSession(now, domain.HarnessCodex)
	progressless.Activity.LastActivityAt = time.Time{}
	sup := &fakeStallSupervisor{}
	observer := New(
		fakeSessions{rows: []domain.SessionRecord{terminated, progressless}},
		&fakeSink{},
		&fakeRuntime{err: errStallProbe},
		fakeAgents{},
		Config{Clock: func() time.Time { return now }, Logger: testLogger()},
	)
	observer.SetStallSupervisor(sup)

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sup.calls) != 0 {
		t.Fatalf("stall calls = %+v, want none for terminated/progressless sessions", sup.calls)
	}
}

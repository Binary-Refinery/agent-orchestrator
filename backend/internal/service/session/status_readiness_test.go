package session

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type readinessCommander struct {
	fakeCommander
	revision       uint64
	unavailable    bool
	reasonForReady bool
}

func (c *readinessCommander) StatusRecoveryRevision() uint64 { return c.revision }
func (c *readinessCommander) SessionStatusReadiness(domain.SessionRecord) string {
	if c.unavailable {
		return "unavailable"
	}
	return "ready"
}
func (c *readinessCommander) SessionStatusRecoveryReason(domain.SessionRecord) (string, string) {
	if !c.unavailable && !c.reasonForReady {
		return "", ""
	}
	return "INSTRUCTOR_RESUME_BLOCKED", "Work is paused; resolve its gate before retrying."
}

type recoveryDuringReadStore struct {
	*fakeStore
	onRead func()
}

func TestRecoveryReasonOnlyPublishedForCurrentUnavailableSnapshot(t *testing.T) {
	for _, operation := range []string{"list", "get"} {
		t.Run(operation, func(t *testing.T) {
			manager := &readinessCommander{unavailable: true}
			st := &recoveryDuringReadStore{fakeStore: newFakeStore()}
			st.sessions["s1"] = domain.SessionRecord{ID: "s1", ProjectID: "p1"}
			svc := NewWithDeps(Deps{Manager: manager, Store: st})
			read := func() domain.Session {
				t.Helper()
				if operation == "get" {
					got, err := svc.Get(context.Background(), "s1")
					if err != nil {
						t.Fatal(err)
					}
					return got
				}
				got, err := svc.List(context.Background(), ListFilter{})
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != 1 {
					t.Fatalf("sessions = %d", len(got))
				}
				return got[0]
			}
			got := read()
			if got.StatusReadiness != "unavailable" || got.StatusRecoveryReasonCode != "INSTRUCTOR_RESUME_BLOCKED" || got.StatusRecoveryReason == "" {
				t.Fatalf("unavailable snapshot omitted recovery cause: %+v", got)
			}
			st.onRead = func() { manager.revision++ }
			got = read()
			if got.StatusReadiness != "checking" || got.StatusRecoveryReasonCode != "" || got.StatusRecoveryReason != "" {
				t.Fatalf("racing snapshot retained stale cause: %+v", got)
			}
			manager.unavailable = false
			got = read()
			if got.StatusReadiness != "ready" || got.StatusRecoveryReasonCode != "" || got.StatusRecoveryReason != "" {
				t.Fatalf("ready snapshot retained recovery cause: %+v", got)
			}
			// Exited is a verified activity, but its failed recovery still needs
			// an explanation until the next successful attempt.
			rec := st.sessions["s1"]
			rec.Activity.State = domain.ActivityExited
			st.sessions["s1"] = rec
			manager.reasonForReady = true
			got = read()
			if got.StatusReadiness != "ready" || got.StatusRecoveryReasonCode != "INSTRUCTOR_RESUME_BLOCKED" {
				t.Fatalf("exited session omitted recovery cause: %+v", got)
			}
		})
	}
}

func (s *recoveryDuringReadStore) ListPRFactsForSessions(ctx context.Context, ids []domain.SessionID) (map[domain.SessionID][]domain.PRFacts, error) {
	if s.onRead != nil {
		s.onRead()
		s.onRead = nil
	}
	return s.fakeStore.ListPRFactsForSessions(ctx, ids)
}
func (s *recoveryDuringReadStore) GetSession(ctx context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	rec, found, err := s.fakeStore.GetSession(ctx, id)
	if s.onRead != nil {
		s.onRead()
		s.onRead = nil
	}
	return rec, found, err
}

func TestRecoveryCannotPublishPreRecoverySnapshotAsReady(t *testing.T) {
	for _, operation := range []string{"list", "get"} {
		t.Run(operation, func(t *testing.T) {
			manager := &readinessCommander{}
			st := &recoveryDuringReadStore{fakeStore: newFakeStore(), onRead: func() { manager.revision++ }}
			st.sessions["s1"] = domain.SessionRecord{ID: "s1", ProjectID: "p1"}
			svc := NewWithDeps(Deps{Manager: manager, Store: st})
			read := func() domain.Session {
				t.Helper()
				if operation == "get" {
					session, err := svc.Get(context.Background(), "s1")
					if err != nil {
						t.Fatal(err)
					}
					return session
				}
				sessions, err := svc.List(context.Background(), ListFilter{})
				if err != nil {
					t.Fatal(err)
				}
				if len(sessions) != 1 {
					t.Fatalf("sessions = %d", len(sessions))
				}
				return sessions[0]
			}
			if got := read().StatusReadiness; got != "checking" {
				t.Fatalf("racing snapshot = %s", got)
			}
			if got := read().StatusReadiness; got != "ready" {
				t.Fatalf("fresh snapshot = %s", got)
			}
		})
	}
}

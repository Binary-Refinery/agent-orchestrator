package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestMarkChatReconnectedPreservesActivityAndRecency(t *testing.T) {
	for _, state := range []domain.ActivityState{domain.ActivityActive, domain.ActivityIdle, domain.ActivityBlocked, domain.ActivityWaitingInput} {
		t.Run(string(state), func(t *testing.T) {
			m, st, _ := newManager()
			rec := working("reconnect")
			rec.Mode = domain.SessionModeChat
			rec.Activity = domain.Activity{State: state, LastActivityAt: time.Unix(100, 0)}
			rec.FirstSignalAt = time.Unix(90, 0)
			rec.Metadata.ProviderConversationID = "live-provider"
			rec.Metadata.ControllerGeneration = "claimed-generation"
			st.sessions[rec.ID] = rec
			if err := m.MarkChatReconnected(ctx, rec.ID, rec.Metadata); err != nil {
				t.Fatal(err)
			}
			got := st.sessions[rec.ID]
			if got.Activity != rec.Activity || !got.UpdatedAt.Equal(rec.UpdatedAt) || !got.FirstSignalAt.Equal(rec.FirstSignalAt) {
				t.Fatalf("reconnect changed activity/recency: before=%+v after=%+v", rec, got)
			}
		})
	}
}

func TestMarkChatReconnectedRejectsChangedOwnership(t *testing.T) {
	for _, change := range []string{"terminated", "provider", "generation", "mode"} {
		t.Run(change, func(t *testing.T) {
			m, st, _ := newManager()
			rec := working("reconnect")
			rec.Mode = domain.SessionModeChat
			rec.Metadata.ProviderConversationID = "provider"
			rec.Metadata.ControllerGeneration = "generation"
			metadata := rec.Metadata
			switch change {
			case "terminated":
				rec.IsTerminated = true
			case "provider":
				rec.Metadata.ProviderConversationID = "replacement"
			case "generation":
				rec.Metadata.ControllerGeneration = "replacement"
			case "mode":
				rec.Mode = domain.SessionModeTUI
			}
			st.sessions[rec.ID] = rec
			if err := m.MarkChatReconnected(ctx, rec.ID, metadata); err == nil {
				t.Fatal("reconnected a different owner")
			}
			if st.sessions[rec.ID] != rec {
				t.Fatal("rejected reconnect mutated the session")
			}
		})
	}
}

func TestColdChatSpawnStillResetsActivity(t *testing.T) {
	m, st, _ := newManager()
	rec := working("cold-resume")
	rec.Mode = domain.SessionModeChat
	rec.Metadata.ProviderConversationID = "same-native-history"
	st.sessions[rec.ID] = rec
	if err := m.MarkSpawned(ctx, rec.ID, rec.Metadata); err != nil {
		t.Fatal(err)
	}
	if st.sessions[rec.ID].Activity.State != domain.ActivityIdle {
		t.Fatal("cold spawn inherited activity from a dead process")
	}
}

type reconnectRepairStore struct {
	*fakeStore
	calls    int
	expected domain.SessionRecord
	applied  bool
	err      error
}

func (s *reconnectRepairStore) RestoreLiveChatReconnectActivity(_ context.Context, expected domain.SessionRecord) (bool, error) {
	s.calls++
	s.expected = expected
	return s.applied, s.err
}

func TestMarkChatReconnectedExitedRepairRequiresAtomicStore(t *testing.T) {
	m, st, _ := newManager()
	rec := working("unavailable-reconnect")
	rec.Mode = domain.SessionModeChat
	rec.Activity.State = domain.ActivityExited
	rec.Metadata.ProviderConversationID = "live-provider"
	rec.Metadata.ControllerGeneration = "claimed-generation"
	st.sessions[rec.ID] = rec
	if err := m.MarkChatReconnected(ctx, rec.ID, rec.Metadata); !errors.Is(err, ports.ErrChatRecoveryInconclusive) {
		t.Fatalf("missing atomic store: %v", err)
	}
	if st.sessions[rec.ID] != rec {
		t.Fatal("unsupported repair changed facts")
	}
}

func TestMarkChatReconnectedExitedRepairPropagatesRaceAndStoreError(t *testing.T) {
	readErr := errors.New("read current conversation failed")
	for _, tc := range []struct {
		name      string
		applied   bool
		err       error
		wantError bool
	}{
		{"exact_commit", true, nil, false},
		{"stale_revision", false, nil, true},
		{"read_error", false, readErr, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &reconnectRepairStore{fakeStore: newFakeStore(), applied: tc.applied, err: tc.err}
			m := New(st, nil)
			rec := working("fenced-reconnect")
			rec.Mode = domain.SessionModeChat
			rec.Activity.State = domain.ActivityExited
			rec.Revision = 17
			rec.Metadata.ProviderConversationID = "live-provider"
			rec.Metadata.ControllerGeneration = "claimed-generation"
			st.sessions[rec.ID] = rec
			err := m.MarkChatReconnected(ctx, rec.ID, rec.Metadata)
			if (err != nil) != tc.wantError || (tc.err != nil && !errors.Is(err, tc.err)) {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
			if st.calls != 1 || st.expected != rec || st.sessions[rec.ID] != rec {
				t.Fatal("repair lost exact owner/revision or changed facts outside store")
			}
		})
	}
}

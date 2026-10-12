package daemon

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type nativePauseStore struct {
	instructorTestStore
	members   []domain.InstructorSessionMembership
	instances map[domain.SessionID]string
	readError map[domain.SessionID]error
}

func (s nativePauseStore) ListInstructorSessionMembers(context.Context, string) ([]domain.InstructorSessionMembership, error) {
	return s.members, nil
}
func (s nativePauseStore) InstructorSessionForMembership(ctx context.Context, id domain.SessionID) (domain.SessionRecord, string, bool, error) {
	if err := s.readError[id]; err != nil {
		return domain.SessionRecord{}, "", false, err
	}
	r, ok, e := s.GetSession(ctx, id)
	return r, s.instances[id], ok, e
}

type nativePauseControl struct {
	reportDeliverySession
	attempts []domain.SessionID
	failures map[domain.SessionID]error
}

func (s *nativePauseControl) InterruptInstructorProducer(_ context.Context, id domain.SessionID, _ string) error {
	s.attempts = append(s.attempts, id)
	return s.failures[id]
}

func TestInstructorNativeStopAttemptsEveryValidMemberAfterFailure(t *testing.T) {
	firstFailure, secondFailure := errors.New("first interrupt failed"), errors.New("second interrupt failed")
	readFailure := errors.New("membership read failed")
	st := nativePauseStore{
		instructorTestStore: instructorTestStore{
			works: []domain.InstructorWork{{ID: "w", ProjectID: "p", State: "parked", Revision: 2}},
			sessions: []domain.SessionRecord{
				{ID: "worker-a", ProjectID: "p", Kind: domain.KindWorker},
				{ID: "worker-b", ProjectID: "p", Kind: domain.KindWorker},
				{ID: "worker-c", ProjectID: "p", Kind: domain.KindWorker},
				{ID: "reused", ProjectID: "p", Kind: domain.KindWorker},
				{ID: "shared-coordinator", ProjectID: "p", Kind: domain.KindOrchestrator},
				{ID: "exited", ProjectID: "p", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityExited}},
				{ID: "wrong-project", ProjectID: "other", Kind: domain.KindWorker},
			},
		},
		members: []domain.InstructorSessionMembership{
			{SessionID: "worker-a", InstanceID: "a"},
			{SessionID: "unreadable", InstanceID: "read"},
			{SessionID: "reused", InstanceID: "original"},
			{SessionID: "shared-coordinator", InstanceID: "shared"},
			{SessionID: "exited", InstanceID: "exit"},
			{SessionID: "wrong-project", InstanceID: "wrong"},
			{SessionID: "worker-b", InstanceID: "b"},
			{SessionID: "worker-c", InstanceID: "c"},
		},
		instances: map[domain.SessionID]string{"worker-a": "a", "worker-b": "b", "worker-c": "c", "reused": "replacement", "shared-coordinator": "shared", "exited": "exit", "wrong-project": "wrong"},
		readError: map[domain.SessionID]error{"unreadable": readFailure},
	}
	ctl := &nativePauseControl{failures: map[domain.SessionID]error{"worker-a": firstFailure, "worker-c": secondFailure}}
	d := instructorDelivery{store: st, sessions: ctl}
	err := d.Deliver(context.Background(), domain.InstructorDispatch{ID: "stop", WorkID: "w", ProjectID: "p", WorkRevision: 2, Action: "stopp"})
	if !errors.Is(err, instructorsvc.ErrDeliveryUncertain) || !errors.Is(err, firstFailure) || !errors.Is(err, secondFailure) || !errors.Is(err, readFailure) || !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("pause failures lost their visible audit: %v", err)
	}
	if !slices.Equal(ctl.attempts, []domain.SessionID{"worker-a", "worker-b", "worker-c"}) {
		t.Fatalf("pause skipped a current worker or interrupted an unrelated incarnation: attempts=%v", ctl.attempts)
	}
	for _, id := range []string{"worker-a", "worker-c", "unreadable", "wrong-project"} {
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("pause failure omitted member %s: %v", id, err)
		}
	}
}

type auditedPauseStore struct {
	nativePauseStore
	dispatch domain.InstructorDispatch
}

func (s auditedPauseStore) ListInstructorDispatches(context.Context, string, string) ([]domain.InstructorDispatch, error) {
	return []domain.InstructorDispatch{s.dispatch}, nil
}

func TestInstructorNativeStopRetriesOnlyPreservedPreInterruptAudit(t *testing.T) {
	for _, state := range []string{"leased", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			st := auditedPauseStore{
				nativePauseStore: nativePauseStore{instructorTestStore: instructorTestStore{works: []domain.InstructorWork{{ID: "w", ProjectID: "p", State: "parked", Revision: 3}}}},
				dispatch:         domain.InstructorDispatch{ID: "stop", WorkID: "w", ProjectID: "p", WorkRevision: 3, Action: "stopp", State: state},
			}
			control := &nativePauseControl{}
			delivery := instructorDelivery{store: st, sessions: control}
			err := delivery.Deliver(context.Background(), domain.InstructorDispatch{ID: "stop", WorkID: "w", ProjectID: "p", WorkRevision: 2, Action: "stopp"})
			if errors.Is(err, instructorsvc.ErrDeliveryRetryable) != (state == "leased") || err == nil || len(control.attempts) != 0 {
				t.Fatalf("stop retried an uncertain/overridden effect: state=%s err=%v attempts=%v", state, err, control.attempts)
			}
		})
	}
}

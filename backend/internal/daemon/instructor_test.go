package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type instructorTestStore struct {
	instructorsvc.Store
	sessions []domain.SessionRecord
	projects []domain.ProjectRecord
	works    []domain.InstructorWork
}

func (s instructorTestStore) ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error) {
	return s.sessions, nil
}
func (s instructorTestStore) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	return s.sessions, nil
}
func (s instructorTestStore) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	return s.projects, nil
}
func (s instructorTestStore) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	for _, rec := range s.projects {
		if rec.ID == id {
			return rec, true, nil
		}
	}
	return domain.ProjectRecord{}, false, nil
}
func (s instructorTestStore) ListInstructorWorks(context.Context) ([]domain.InstructorWork, error) {
	return s.works, nil
}
func (s instructorTestStore) GetInstructorWork(_ context.Context, id string) (domain.InstructorWork, bool, error) {
	for _, rec := range s.works {
		if rec.ID == id {
			return rec, true, nil
		}
	}
	return domain.InstructorWork{}, false, nil
}
func (s instructorTestStore) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	for _, rec := range s.sessions {
		if rec.ID == id {
			return rec, true, nil
		}
	}
	return domain.SessionRecord{}, false, nil
}

type instructorTestBinder struct {
	target domain.SessionID
	id     string
	err    error
}

func (s *reportDeliverySession) SendGovernedSemantic(ctx context.Context, id domain.SessionID, message, key string) error {
	return s.SendSemantic(ctx, id, message, key)
}

func (b *instructorTestBinder) BindDispatchTarget(_ context.Context, id string, target domain.SessionID) error {
	b.id, b.target = id, target
	return b.err
}

func TestInstructorDeliveryUsesActualProjectOrchestratorAndStableKey(t *testing.T) {
	now := time.Now()
	store := instructorTestStore{works: []domain.InstructorWork{{ID: "work-1", ProjectID: "p"}}, sessions: []domain.SessionRecord{
		{ID: "old", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: now.Add(-time.Hour)},
		{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, CreatedAt: now.Add(time.Hour)},
		{ID: "other", ProjectID: "other", Kind: domain.KindOrchestrator, CreatedAt: now.Add(time.Hour)},
		{ID: "new", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: now, Activity: domain.Activity{State: domain.ActivityWaitingInput}},
	}}
	sender, binder := &reportDeliverySession{}, &instructorTestBinder{}
	delivery := instructorDelivery{store: store, sessions: sender, binder: binder}
	dispatch := domain.InstructorDispatch{ID: "dispatch-1", WorkID: "work-1", ProjectID: "p", Message: "Continue the reviewed scope"}
	if err := delivery.Deliver(context.Background(), dispatch); err != nil {
		t.Fatal(err)
	}
	if sender.sentID != "new" || sender.sentKey != "dispatch-1" || binder.target != "new" || binder.id != "dispatch-1" {
		t.Fatalf("sender=%+v binder=%+v", sender, binder)
	}
	if !strings.Contains(sender.sentMessage, "--kind accepted --dispatch \"dispatch-1\"") || !strings.Contains(sender.sentMessage, "ao spawn --work \"work-1\"") {
		t.Fatalf("no receipt/delegation binding: %s", sender.sentMessage)
	}
	// Once bound, a restart must not silently retarget to the newer controller.
	dispatch.TargetSessionID = "old"
	if err := delivery.Deliver(context.Background(), dispatch); err != nil {
		t.Fatal(err)
	}
	if sender.sentID != "old" {
		t.Fatalf("bound target was changed to %s", sender.sentID)
	}
}

func TestInstructorDeliveryFailsClosedBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rec       domain.SessionRecord
		bindError error
	}{
		{"blocked", domain.SessionRecord{ID: "o", ProjectID: "p", Kind: domain.KindOrchestrator, Activity: domain.Activity{State: domain.ActivityBlocked}}, nil},
		{"terminated", domain.SessionRecord{ID: "o", ProjectID: "p", Kind: domain.KindOrchestrator, IsTerminated: true}, nil},
		{"wrong project", domain.SessionRecord{ID: "o", ProjectID: "other", Kind: domain.KindOrchestrator}, nil},
		{"binding failure", domain.SessionRecord{ID: "o", ProjectID: "p", Kind: domain.KindOrchestrator}, errors.New("durable bind failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := &reportDeliverySession{}
			delivery := instructorDelivery{store: instructorTestStore{works: []domain.InstructorWork{{ID: "w", ProjectID: "p"}}, sessions: []domain.SessionRecord{tc.rec}}, sessions: sender, binder: &instructorTestBinder{err: tc.bindError}}
			err := delivery.Deliver(context.Background(), domain.InstructorDispatch{ID: "d", WorkID: "w", ProjectID: "p", TargetSessionID: "o"})
			if err == nil || sender.sentID != "" {
				t.Fatalf("err=%v send=%+v", err, sender)
			}
		})
	}
}

func TestInstructorDeliveryCancelNeverClaimsNativePaneWasCancelled(t *testing.T) {
	delivery := instructorDelivery{store: instructorTestStore{sessions: []domain.SessionRecord{{ID: "tui", Mode: domain.SessionModeTUI}}}}
	if err := delivery.Cancel(context.Background(), domain.InstructorDispatch{}); err != nil {
		t.Fatalf("unbound dispatch cannot have reached a native target: %v", err)
	}
	if err := delivery.Cancel(context.Background(), domain.InstructorDispatch{ID: "d", TargetSessionID: "tui"}); !errors.Is(err, instructorsvc.ErrDeliveryUncertain) {
		t.Fatalf("pane cancellation was not established: %v", err)
	}
}

func TestInstructorPureProjectOrchestratorCannotChooseAnotherWorkRole(t *testing.T) {
	// Contradictory role input must fail before policy loading or native launch.
	guard := instructorGovernance{}
	for _, role := range []string{"worker", "reviewer", "fixer"} {
		if _, err := guard.PrepareLaunch(context.Background(), ports.SpawnConfig{Kind: domain.KindOrchestrator, WorkRole: role}, domain.ProjectRecord{}); !errors.Is(err, instructorsvc.ErrValidation) {
			t.Fatalf("role %s: %v", role, err)
		}
	}
}

func TestInstructorInventoryPreservesUnassignedRuntimeFacts(t *testing.T) {
	store := instructorTestStore{
		projects: []domain.ProjectRecord{{ID: "p", DisplayName: "Project", Path: "/repo"}},
		sessions: []domain.SessionRecord{{ID: "legacy", ProjectID: "p", Kind: domain.KindWorker, Metadata: domain.SessionMetadata{Branch: "keep-me", WorkspacePath: "/worktree", Prompt: "private prompt"}}, {ID: "managed", ProjectID: "p", Kind: domain.KindWorker}},
		works:    []domain.InstructorWork{{ID: "work", SessionIDs: []domain.SessionID{"managed"}}},
	}
	result, err := (instructorInventory{store: store}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Atomic || result.UnassignedCount != 1 || len(result.Sessions) != 2 {
		t.Fatalf("inventory=%+v", result)
	}
	if result.Sessions[0].Assignment != "unassigned" || result.Sessions[0].Branch != "keep-me" || result.Sessions[0].WorkspacePath != "/worktree" || len(result.Sessions[0].WorkIDs) != 0 {
		t.Fatalf("legacy=%+v", result.Sessions[0])
	}
	if result.Sessions[1].Assignment != "linked" || result.Sessions[1].WorkIDs[0] != "work" {
		t.Fatalf("managed=%+v", result.Sessions[1])
	}
}

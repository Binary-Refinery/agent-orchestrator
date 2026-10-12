package instructor_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestInstructorMembersRetainDeletedInstanceWithoutAdoptingReusedID(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	work := f.register(t, "member-reuse", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, work.ID)
	before, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Members) != 1 || before.Members[0].Association != "current" || before.Members[0].OpenTarget == nil || before.Members[0].Confirmed != nil || before.Members[0].Sent == nil {
		t.Fatalf("initial members=%+v", before)
	}
	oldInstance := before.Members[0].InstanceID
	if deleted, err := f.store.DeleteSession(ctx, sid); err != nil || !deleted {
		t.Fatalf("delete=%v err=%v", deleted, err)
	}
	replacement, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, Harness: domain.HarnessCodex, DisplayName: "Unrelated replacement", CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID != sid {
		t.Fatalf("expected reused display ID: %s != %s", replacement.ID, sid)
	}
	newInstance, found, err := f.store.InstructorSessionInstance(ctx, sid)
	if err != nil || !found || newInstance == oldInstance {
		t.Fatalf("instance reused: %q %q %v", newInstance, oldInstance, err)
	}
	// A normal heartbeat/phase/evidence save used to re-insert every SessionID.
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) { w.Summary = "Unrelated work update" })
	if _, managed, err := f.service.WorkForSession(ctx, sid); err != nil || managed {
		t.Fatalf("replacement silently adopted: %v %v", managed, err)
	}
	after, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Members) != 1 || after.Members[0].Association != "historical" || after.Members[0].InstanceID != oldInstance || after.Members[0].OpenTarget != nil || after.Members[0].Name == replacement.DisplayName {
		t.Fatalf("historical identity leaked replacement: %+v", after)
	}
	if _, err := f.service.Report(ctx, instructorsvc.ReportInput{WorkID: work.ID, SessionID: sid, Kind: "progress", Summary: "unrelated"}); !errors.Is(err, instructorsvc.ErrValidation) {
		t.Fatalf("reused-ID report accepted: %v", err)
	}
	// Explicit operator admission may associate the new incarnation. The old
	// entry survives, and old native selections never transfer to the new agent.
	if err := f.service.AttachSession(ctx, work.ID, sid); err != nil {
		t.Fatal(err)
	}
	admitted, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(admitted.Members) != 2 {
		t.Fatalf("lost historical membership: %+v", admitted)
	}
	current := 0
	for _, member := range admitted.Members {
		if member.Association == "current" {
			current++
			if member.InstanceID != newInstance || member.Requested != nil || member.Sent != nil || member.Confirmed != nil {
				t.Fatalf("native launch proof transferred: %+v", member)
			}
		}
	}
	if current != 1 {
		t.Fatalf("current members=%d: %+v", current, admitted)
	}
}

func TestInstructorMembersShowUnprovedHistoryAndSeparateProjectCoordinator(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	work := f.register(t, "legacy-members", instructorsvc.DefaultPolicy())
	worker, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) { w.SessionIDs = []domain.SessionID{worker.ID} })
	older, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindOrchestrator, DisplayName: "Older coordinator", CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindOrchestrator, DisplayName: "Current coordinator", CreatedAt: f.now.Add(time.Second), UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Atomic || got.WorkRevision <= work.Revision || got.Coordinator.Key != "governed:orchestrator" || got.Coordinator.Current == nil || got.Coordinator.Current.SessionID != newer.ID || got.Coordinator.Current.SessionID == older.ID {
		t.Fatalf("coordinator=%+v", got)
	}
	if len(got.Members) != 1 || got.Members[0].Association != "legacy_unknown" || got.Members[0].OpenTarget != nil {
		t.Fatalf("unproved history autojoined: %+v", got.Members)
	}
	if _, managed, err := f.service.WorkForSession(ctx, newer.ID); err != nil || managed {
		t.Fatalf("reading board adopted project coordinator: %v %v", managed, err)
	}
}

func TestInstructorSessionAdmissionFencesStaleCapturedInstance(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	work := f.register(t, "stale-admission", instructorsvc.DefaultPolicy())
	original, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	old, _, err := f.store.InstructorSessionInstance(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DeleteSession(ctx, original.ID); err != nil {
		t.Fatal(err)
	}
	replacement, err := f.store.CreateSession(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID != original.ID {
		t.Fatal("test requires reused ID")
	}
	proposed := work
	proposed.Revision++
	proposed.SessionIDs = []domain.SessionID{original.ID}
	updated, err := f.store.AttachInstructorSession(ctx, proposed, work.Revision, domain.InstructorEvent{ID: "stale-admission", WorkID: work.ID, At: f.now}, domain.InstructorSessionMembership{SessionID: original.ID, InstanceID: old, ProjectID: work.ProjectID, Provenance: "admitted"})
	if !errors.Is(err, domain.ErrInstructorSessionInstanceChanged) || updated {
		t.Fatalf("stale admitted: updated=%v err=%v", updated, err)
	}
	retained, _, err := f.store.GetInstructorWork(ctx, work.ID)
	if err != nil || retained.Revision != work.Revision || len(retained.SessionIDs) != 0 {
		t.Fatalf("stale admission changed work: %+v %v", retained, err)
	}
}

func TestInstructorMembershipConcurrentAdmissionHasOneOwner(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	a := f.register(t, "owner-a", instructorsvc.DefaultPolicy())
	b := f.register(t, "owner-b", instructorsvc.DefaultPolicy())
	worker, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindWorker, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	// Different service instances do not share a mutex. Ownership is enforced by
	// the SQLite transaction, including rollback of the losing work revision.
	other := instructorsvc.New(instructorsvc.Deps{Store: f.store, Clock: func() time.Time { return f.now }})
	start := make(chan struct{})
	out := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; out <- f.service.AttachSession(ctx, a.ID, worker.ID) }()
	go func() { defer wg.Done(); <-start; out <- other.AttachSession(ctx, b.ID, worker.ID) }()
	close(start)
	wg.Wait()
	close(out)
	success := 0
	for err := range out {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful owners=%d", success)
	}
	bound, managed, err := f.service.WorkForSession(ctx, worker.ID)
	if err != nil || !managed {
		t.Fatalf("owner missing: %v %v", managed, err)
	}
	losing := a
	if bound.ID == a.ID {
		losing = b
	}
	unchanged, _, err := f.store.GetInstructorWork(ctx, losing.ID)
	if err != nil || unchanged.Revision != losing.Revision || len(unchanged.SessionIDs) != 0 {
		t.Fatalf("losing admission partially committed: %+v %v", unchanged, err)
	}
}

func TestInstructorBoundDispatchCannotTargetReusedSession(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	work := f.register(t, "dispatch-instance", instructorsvc.DefaultPolicy())
	target, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: "governed", Kind: domain.KindOrchestrator, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := domain.InstructorDispatch{ID: "instance-dispatch", BatchID: "instance-batch", WorkID: work.ID, ProjectID: work.ProjectID, WorkRevision: work.Revision, State: "pending", CreatedAt: f.now, UpdatedAt: f.now}
	if err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{dispatch}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.BindDispatchTarget(ctx, dispatch.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DeleteSession(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	replaced, err := f.store.CreateSession(ctx, target)
	if err != nil || replaced.ID != target.ID {
		t.Fatalf("replace: %+v %v", replaced, err)
	}
	if err := f.service.BindDispatchTarget(ctx, dispatch.ID, replaced.ID); err == nil {
		t.Fatal("immutable target rebound to new instance")
	}
	if err := f.service.ValidateDispatchAdmission(ctx, replaced.ID, dispatch.ID); !errors.Is(err, instructorsvc.ErrValidation) {
		t.Fatalf("reused target passed queue admission: %v", err)
	}
	if _, err := f.service.Report(ctx, instructorsvc.ReportInput{WorkID: work.ID, SessionID: replaced.ID, Kind: "accepted", DispatchID: dispatch.ID}); !errors.Is(err, instructorsvc.ErrValidation) {
		t.Fatalf("reused target forged receipt: %v", err)
	}
}

func TestInstructorMembersReviewerUsesRetainedReviewIdentity(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	work := f.register(t, "review-members", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, work.ID)
	review := domain.Review{ID: "review-member", SessionID: sid, ProjectID: work.ProjectID, Harness: domain.ReviewerCodex, ReviewerLaunchID: "review-generation", CreatedAt: f.now, UpdatedAt: f.now}
	if err := f.store.UpsertReview(ctx, review); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{ID: "run-member", ReviewID: review.ID, SessionID: sid, Harness: domain.ReviewerCodex, Status: domain.ReviewRunRunning, CreatedAt: f.now}
	if err := f.store.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	selection := domain.InstructorAgentSelection{Harness: "codex", Model: "provider/reviewer", Effort: "high"}
	if err := f.service.RecordLaunch(ctx, work.ID, domain.InstructorLaunch{ReviewID: review.ID, ReviewRunID: run.ID, Role: "reviewer", Requested: selection, Sent: &selection, Status: "launched"}); err != nil {
		t.Fatal(err)
	}
	current, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	var member domain.InstructorMember
	for _, m := range current.Members {
		if m.Kind == "review" {
			member = m
		}
	}
	if member.Association != "current" || member.OpenTarget == nil || member.OpenTarget.Kind != "review" || member.OpenTarget.SessionID != sid || member.OpenTarget.ReviewID != review.ID || member.OpenTarget.ReviewRunID != run.ID || member.Confirmed != nil {
		t.Fatalf("reviewer target=%+v", member)
	}
	if _, err := f.store.UpdateReviewRunResult(ctx, run.ID, domain.ReviewRunComplete, domain.VerdictApproved, "approved", "", true); err != nil {
		t.Fatal(err)
	}
	historical, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range historical.Members {
		if m.Kind == "review" && (m.Association != "historical" || m.OpenTarget == nil) {
			t.Fatalf("completed review history=%+v", m)
		}
	}
	if deleted, err := f.store.DeleteSession(ctx, sid); err != nil || !deleted {
		t.Fatalf("delete reviewer owner: %v %v", deleted, err)
	}
	if _, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: work.ProjectID, Kind: domain.KindWorker, CreatedAt: f.now, UpdatedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	deleted, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range deleted.Members {
		if m.Kind == "review" && (m.Association != "historical" || m.OpenTarget != nil) {
			t.Fatalf("deleted review linked replacement=%+v", m)
		}
	}
}

func TestInstructorMembersDoNotCarryProviderConfirmationAcrossControllerRestore(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	work := f.register(t, "confirmation-generation", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, work.ID)
	rec, _, err := f.store.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	rec.Metadata.RuntimeLaunchID = "old-controller"
	if err := f.store.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	selection := domain.InstructorAgentSelection{Harness: "opencode", Model: "provider/worker", Effort: "high"}
	if err := f.service.RecordLaunch(ctx, work.ID, domain.InstructorLaunch{SessionID: sid, Role: "worker", Requested: selection, Sent: &selection, Confirmed: &selection, Status: "native-config-reported"}); err != nil {
		t.Fatal(err)
	}
	before, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Members[0].Confirmed == nil {
		t.Fatal("current native confirmation missing")
	}
	rec.Metadata.RuntimeLaunchID = "new-controller"
	if err := f.store.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Members[0].RuntimeGeneration != "new-controller" || after.Members[0].Confirmed != nil || after.Members[0].Sent != nil || after.Members[0].Requested == nil {
		t.Fatalf("restored generation inherited old confirmation: %+v", after.Members[0])
	}
}

func TestInstructorMembersSurviveServiceRestart(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	work := f.register(t, "members-restart", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, work.ID)
	before, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.rebuildService()
	after, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Members) != 1 || after.Members[0].ID != before.Members[0].ID || after.Members[0].SessionID != sid || after.Members[0].OpenTarget == nil {
		t.Fatalf("membership lost on restart: %+v", after)
	}
}

func TestInstructorMembersRequireExplicitConfirmationForLegacyUpgradeBinding(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	work := f.register(t, "legacy-live-binding", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, work.ID)
	db, err := sql.Open("sqlite", filepath.Join(f.dataDir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `UPDATE instructor_session_members SET body=json_set(body,'$.provenance','legacy_bound') WHERE work_id=?`, work.ID); err != nil {
		t.Fatal(err)
	}
	legacy, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Members[0].Association != "legacy_unknown" || legacy.Members[0].OpenTarget != nil {
		t.Fatalf("legacy assertion labelled proven: %+v", legacy)
	}
	if bound, managed, err := f.service.WorkForSession(ctx, sid); err != nil || !managed || bound.ID != work.ID {
		t.Fatalf("upgrade removed governance authority: %+v %v %v", bound, managed, err)
	}
	if err := f.service.AttachSession(ctx, work.ID, sid); err != nil {
		t.Fatal(err)
	}
	confirmed, err := f.service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Members[0].Association != "current" || confirmed.Members[0].OpenTarget == nil || confirmed.WorkRevision <= legacy.WorkRevision {
		t.Fatalf("explicit confirmation was not persisted: %+v", confirmed)
	}
}

// reviewerOwnerABAStore deterministically replaces the worker after the caller
// has retained the review/run. The former two-read path returns the old key and
// then the replacement row; the atomic path observes only the replacement pair.
type reviewerOwnerABAStore struct {
	*sqlite.Store
	reviewRunID string
	owner       domain.SessionID
	armed       bool
	replaced    bool
	replace     func()
}

func (s *reviewerOwnerABAStore) GetReviewRun(ctx context.Context, id string) (domain.ReviewRun, bool, error) {
	run, found, err := s.Store.GetReviewRun(ctx, id)
	if id == s.reviewRunID && found && err == nil {
		s.armed = true
	}
	return run, found, err
}

func (s *reviewerOwnerABAStore) replaceIfArmed(id domain.SessionID) {
	if s.armed && !s.replaced && id == s.owner {
		s.replace()
		s.replaced = true
	}
}

func (s *reviewerOwnerABAStore) InstructorSessionInstance(ctx context.Context, id domain.SessionID) (string, bool, error) {
	instance, found, err := s.Store.InstructorSessionInstance(ctx, id)
	// The regression fails on the original path: the old key remains captured
	// while the following ordinary GetSession reads the newly inserted owner.
	s.replaceIfArmed(id)
	return instance, found, err
}

func (s *reviewerOwnerABAStore) InstructorSessionForMembership(ctx context.Context, id domain.SessionID) (domain.SessionRecord, string, bool, error) {
	s.replaceIfArmed(id)
	return s.Store.InstructorSessionForMembership(ctx, id)
}

func TestInstructorMembersReviewerOwnerReplacementCannotMixRowAndInstance(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	work := f.register(t, "review-owner-aba", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, work.ID)
	review := domain.Review{ID: "aba-review", SessionID: sid, ProjectID: work.ProjectID, Harness: domain.ReviewerCodex, ReviewerLaunchID: "aba-review-launch", CreatedAt: f.now, UpdatedAt: f.now}
	if err := f.store.UpsertReview(ctx, review); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{ID: "aba-review-run", ReviewID: review.ID, SessionID: sid, Harness: domain.ReviewerCodex, Status: domain.ReviewRunRunning, CreatedAt: f.now}
	if err := f.store.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	selection := domain.InstructorAgentSelection{Harness: "codex", Model: "provider/reviewer", Effort: "high"}
	if err := f.service.RecordLaunch(ctx, work.ID, domain.InstructorLaunch{ReviewID: review.ID, ReviewRunID: run.ID, Role: "reviewer", Requested: selection, Sent: &selection, Status: "launched"}); err != nil {
		t.Fatal(err)
	}
	before, _, err := f.store.GetInstructorWork(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	owner, oldInstance, found, err := f.store.InstructorSessionForMembership(ctx, sid)
	if err != nil || !found {
		t.Fatalf("owner read: %v %v", found, err)
	}
	store := &reviewerOwnerABAStore{Store: f.store, reviewRunID: run.ID, owner: sid}
	store.replace = func() {
		deleted, err := f.store.DeleteSession(ctx, sid)
		if err != nil || !deleted {
			t.Fatalf("delete original owner: %v %v", deleted, err)
		}
		replacement := owner
		replacement.DisplayName = "Replacement using the same display ID"
		inserted, err := f.store.CreateSession(ctx, replacement)
		if err != nil || inserted.ID != sid {
			t.Fatalf("reinsert owner: %+v %v", inserted, err)
		}
		_, newInstance, found, err := f.store.InstructorSessionForMembership(ctx, sid)
		if err != nil || !found || newInstance == oldInstance {
			t.Fatalf("replacement key: %q old=%q found=%v err=%v", newInstance, oldInstance, found, err)
		}
	}
	service := instructorsvc.New(instructorsvc.Deps{Store: store, Clock: func() time.Time { return f.now }})
	members, err := service.Members(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !store.replaced {
		t.Fatal("deterministic owner replacement did not execute")
	}
	reviewFound := false
	for _, member := range members.Members {
		if member.Kind != "review" || member.ReviewRunID != run.ID {
			continue
		}
		reviewFound = true
		if member.Association != "historical" || member.OpenTarget != nil {
			t.Fatalf("review linked an unrelated replacement owner: %+v", member)
		}
	}
	if !reviewFound {
		t.Fatalf("retained review missing: %+v", members)
	}
	after, _, err := f.store.GetInstructorWork(ctx, work.ID)
	if err != nil || after.Revision != before.Revision {
		t.Fatalf("test did not preserve work revision: before=%d after=%d err=%v", before.Revision, after.Revision, err)
	}
}

package instructor_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

const immutableSHA = "1111111111111111111111111111111111111111"

type deliveryRecorder struct {
	calls           []domain.InstructorDispatch
	reconciliations []domain.InstructorDispatch
	bind            func(context.Context, domain.InstructorDispatch) error
	err             error
}

func (d *deliveryRecorder) Deliver(ctx context.Context, dispatch domain.InstructorDispatch) error {
	d.calls = append(d.calls, dispatch)
	if d.bind != nil {
		if err := d.bind(ctx, dispatch); err != nil {
			return err
		}
	}
	return d.err
}

func (d *deliveryRecorder) Reconcile(_ context.Context, dispatch domain.InstructorDispatch, _ bool, _ string) error {
	d.reconciliations = append(d.reconciliations, dispatch)
	return nil
}

type instructorFixture struct {
	store    *sqlite.Store
	service  *instructorsvc.Service
	delivery *deliveryRecorder
	now      time.Time
	dataDir  string
}

func newInstructorFixture(t *testing.T) *instructorFixture {
	t.Helper()
	f := &instructorFixture{
		delivery: &deliveryRecorder{},
		now:      time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC),
		dataDir:  t.TempDir(),
	}
	var err error
	f.store, err = sqlitetest.Open(f.dataDir)
	if err != nil {
		t.Fatalf("open SQLite fixture: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := f.store.Close(); closeErr != nil {
			t.Errorf("close SQLite fixture: %v", closeErr)
		}
	})
	if err = f.store.UpsertProject(context.Background(), domain.ProjectRecord{
		ID: "governed", Path: t.TempDir(), RegisteredAt: f.now,
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	f.rebuildService()
	return f
}

func (f *instructorFixture) rebuildService() {
	f.service = instructorsvc.New(instructorsvc.Deps{
		Store: f.store, Delivery: f.delivery, Clock: func() time.Time { return f.now },
	})
}

func (f *instructorFixture) register(t *testing.T, sourceRef string, policy domain.InstructorPolicy) domain.InstructorWork {
	t.Helper()
	w, err := f.service.Register(context.Background(), instructorsvc.RegisterInput{
		ProjectID: "governed", Name: sourceRef, Goal: "Complete the requested work", Source: "manual", SourceRef: sourceRef, Policy: policy,
	})
	if err != nil {
		t.Fatalf("register %s: %v", sourceRef, err)
	}
	return w
}

func (f *instructorFixture) attachWorker(t *testing.T, workID string) domain.SessionID {
	t.Helper()
	rec, err := f.store.CreateSession(context.Background(), domain.SessionRecord{
		ProjectID: "governed", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode,
		Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: f.now},
		Metadata:  domain.SessionMetadata{Model: "provider/worker", Effort: "high"},
		CreatedAt: f.now, UpdatedAt: f.now,
	})
	if err != nil {
		t.Fatalf("seed worker session: %v", err)
	}
	if err = f.service.AttachSession(context.Background(), workID, rec.ID); err != nil {
		t.Fatalf("attach worker: %v", err)
	}
	selection := domain.InstructorAgentSelection{Harness: "opencode", Model: "provider/worker", Effort: "high"}
	if err = f.service.RecordLaunch(context.Background(), workID, domain.InstructorLaunch{
		SessionID: rec.ID, Role: "worker", Requested: selection, Sent: &selection, Status: "launched", LaunchedAt: f.now,
	}); err != nil {
		t.Fatalf("record worker launch: %v", err)
	}
	return rec.ID
}

func (f *instructorFixture) snapshot(t *testing.T) domain.InstructorSnapshot {
	t.Helper()
	snapshot, err := f.service.Snapshot(context.Background(), "")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snapshot
}

// This helper represents facts imported by a trusted daemon adapter. It does
// not bypass the public Report boundary in tests checking client evidence.
func (f *instructorFixture) trustedFacts(t *testing.T, id string, update func(*domain.InstructorWork)) domain.InstructorWork {
	t.Helper()
	w, found, err := f.store.GetInstructorWork(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("read work before trusted facts: found=%v err=%v", found, err)
	}
	expected := w.Revision
	update(&w)
	w.Revision++
	w.UpdatedAt = f.now
	changed, err := f.store.SaveInstructorWork(context.Background(), w, expected, domain.InstructorEvent{
		ID: uuid.NewString(), WorkID: id, Kind: "trusted_fixture", At: f.now,
	})
	if err != nil || !changed {
		t.Fatalf("persist trusted facts: changed=%v err=%v", changed, err)
	}
	return w
}

func TestInstructorFrozenCommandAndWorkIdentitySurviveSQLiteRestart(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "durable", instructorsvc.DefaultPolicy())
	snapshot := f.snapshot(t)
	input := instructorsvc.CommandInput{Command: "Weiter", SnapshotID: snapshot.ID, WorkIDs: []string{work.ID}, IdempotencyKey: "durable-command"}
	first, err := f.service.Command(context.Background(), input)
	if err != nil || len(first.Dispatches) != 1 {
		t.Fatalf("enqueue before restart: dispatches=%d err=%v", len(first.Dispatches), err)
	}
	if err = f.store.Close(); err != nil {
		t.Fatalf("close before restart: %v", err)
	}
	f.store, err = sqlite.OpenPreMigrated(f.dataDir)
	if err != nil {
		t.Fatalf("reopen SQLite: %v", err)
	}
	f.rebuildService()
	repeated, err := f.service.Command(context.Background(), input)
	if err != nil || len(repeated.Dispatches) != 1 || repeated.Dispatches[0].ID != first.Dispatches[0].ID {
		t.Fatalf("retry after restart duplicated or lost command: result=%+v err=%v", repeated, err)
	}
	duplicate := f.register(t, "durable", instructorsvc.DefaultPolicy())
	if duplicate.ID != work.ID {
		t.Fatalf("same origin obtained a different identity: %s != %s", duplicate.ID, work.ID)
	}
	if err = f.service.Tick(context.Background()); err != nil {
		t.Fatalf("deliver recovered command: %v", err)
	}
	if len(f.delivery.calls) != 1 || f.delivery.calls[0].WorkID != work.ID {
		t.Fatalf("recovered deliveries = %+v", f.delivery.calls)
	}
}

func TestInstructorRejectsStaleSnapshotBeforeAnyBatchIsEnqueued(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "stale", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	snapshot := f.snapshot(t)
	if _, err := f.service.Report(context.Background(), instructorsvc.ReportInput{
		WorkID: work.ID, SessionID: worker, Kind: "progress", Summary: "New result",
	}); err != nil {
		t.Fatalf("report progress: %v", err)
	}
	_, err := f.service.Command(context.Background(), instructorsvc.CommandInput{
		Command: "Weiter", SnapshotID: snapshot.ID, IdempotencyKey: "stale-command",
	})
	if !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("stale snapshot error = %v, want conflict", err)
	}
	queued, err := f.store.ListInstructorDispatches(context.Background(), "", "")
	if err != nil || len(queued) != 0 {
		t.Fatalf("stale command mutated queue: %+v err=%v", queued, err)
	}
}

func TestInstructorFailedFrozenBatchCancelsRemainingDeliveries(t *testing.T) {
	f := newInstructorFixture(t)
	first := f.register(t, "first", instructorsvc.DefaultPolicy())
	second := f.register(t, "second", instructorsvc.DefaultPolicy())
	snapshot := f.snapshot(t)
	result, err := f.service.Command(context.Background(), instructorsvc.CommandInput{
		Command: "Weiter", SnapshotID: snapshot.ID, WorkIDs: []string{first.ID, second.ID}, IdempotencyKey: "frozen-batch",
	})
	if err != nil || len(result.Dispatches) != 2 {
		t.Fatalf("enqueue batch: %+v err=%v", result, err)
	}
	f.delivery.err = errors.New("orchestrator unavailable")
	_ = f.service.Tick(context.Background())
	if len(f.delivery.calls) != 1 {
		t.Fatalf("batch delivered after first failure: %+v", f.delivery.calls)
	}
	queued, err := f.store.ListInstructorDispatches(context.Background(), result.BatchID, "")
	if err != nil || len(queued) != 2 || queued[0].State != "failed" || queued[1].State != "cancelled" {
		t.Fatalf("failed batch states = %+v err=%v", queued, err)
	}
}

func TestInstructorClientVerifiedFlagDoesNotCreateAcceptedEvidence(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "forged-evidence", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	result, err := f.service.Report(context.Background(), instructorsvc.ReportInput{
		WorkID: work.ID, SessionID: worker, Kind: "evidence", SHA: immutableSHA,
		Evidence: []domain.InstructorEvidence{{Kind: "check", Profile: "push", Role: "worker", Ref: "claimed-success", Verified: true}},
	})
	if err != nil {
		t.Fatalf("record unverified client evidence: %v", err)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].Verified {
		t.Fatalf("client forged verified evidence: %+v", result.Evidence)
	}
	if _, err = f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "done"}); err == nil {
		t.Fatal("client claim alone completed work")
	}
}

func TestInstructorHumanWaitDoesNotSpendNudges(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "human-wait", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	if _, err := f.service.Report(context.Background(), instructorsvc.ReportInput{
		WorkID: work.ID, SessionID: worker, Kind: "needs_input", Gate: &domain.InstructorGate{Kind: "human", Title: "Operator decision"},
	}); err != nil {
		t.Fatalf("open human gate: %v", err)
	}
	f.now = f.now.Add(24 * time.Hour)
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatalf("human wait timer: %v", err)
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.State != "waiting_human" || details.Work.NudgedAt != nil || len(f.delivery.calls) != 0 {
		t.Fatalf("human wait was nudged or escalated: %+v err=%v", details, err)
	}
}

func TestInstructorReceiptIsNotProgress(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "receipt", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	original := work.ProgressAt
	f.now = f.now.Add(time.Minute)
	updated, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "accepted", Summary: "Still alive"})
	if err != nil {
		t.Fatalf("report receipt: %v", err)
	}
	if !updated.ProgressAt.Equal(original) {
		t.Fatalf("receipt hid a stall by advancing ProgressAt: %s -> %s", original, updated.ProgressAt)
	}
}

func TestInstructorHumanTransitionRequiresAnActualGrant(t *testing.T) {
	f := newInstructorFixture(t)
	policy := instructorsvc.DefaultPolicy()
	policy.InitialPhase = "triage"
	work := f.register(t, "human-transition", policy)
	worker := f.attachWorker(t, work.ID)
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.SHA = immutableSHA
		w.Evidence = []domain.InstructorEvidence{{ID: "trusted-triage", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: immutableSHA, Kind: "triage", Role: "orchestrator", Producer: "independent-controller", Ref: "trusted-triage", Verified: true}}
	})
	_, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "progress", Phase: "fix"})
	if err == nil {
		t.Fatal("human-gated transition passed merely because no gate object was open")
	}
	details, readErr := f.service.Details(context.Background(), work.ID)
	if readErr != nil || details.Work.Phase != "triage" {
		t.Fatalf("rejected transition changed durable phase: %+v err=%v", details.Work, readErr)
	}
}

func TestInstructorTransitionRoleCannotBeSpoofedByAttachedWorker(t *testing.T) {
	f := newInstructorFixture(t)
	policy := instructorsvc.DefaultPolicy()
	policy.Transitions = []domain.InstructorTransition{{From: "intake", To: "review", Role: "reviewer"}}
	work := f.register(t, "transition-role", policy)
	worker := f.attachWorker(t, work.ID)
	_, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "progress", Phase: "review"})
	if err == nil {
		t.Fatal("worker performed a transition reserved for an independent reviewer")
	}
}

func TestInstructorRefusesThirdFixRound(t *testing.T) {
	f := newInstructorFixture(t)
	policy := instructorsvc.DefaultPolicy()
	policy.InitialPhase = "triage"
	policy.Transitions = []domain.InstructorTransition{{From: "triage", To: "fix", Role: "worker"}}
	work := f.register(t, "fix-budget", policy)
	worker := f.attachWorker(t, work.ID)
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) { w.FixRounds = 2 })
	_, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "progress", Phase: "fix"})
	if err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("third fix round error = %v, want budget rejection", err)
	}
}

func TestInstructorDispatchReceiptRequiresTheActualTarget(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "receipt-target", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	orchestrator, err := f.store.CreateSession(context.Background(), domain.SessionRecord{
		ProjectID: "governed", Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: f.now}, CreatedAt: f.now, UpdatedAt: f.now,
	})
	if err != nil {
		t.Fatalf("seed actual dispatch target: %v", err)
	}
	snapshot := f.snapshot(t)
	result, err := f.service.Command(context.Background(), instructorsvc.CommandInput{
		Command: "Weiter", SnapshotID: snapshot.ID, WorkIDs: []string{work.ID}, IdempotencyKey: "receipt-target",
	})
	if err != nil {
		t.Fatalf("enqueue directive: %v", err)
	}
	dispatch, found, err := f.store.ClaimInstructorDispatch(context.Background(), f.now)
	if err != nil || !found || dispatch.ID != result.Dispatches[0].ID {
		t.Fatalf("claim directive: %+v found=%v err=%v", dispatch, found, err)
	}
	if err = f.service.BindDispatchTarget(context.Background(), dispatch.ID, orchestrator.ID); err != nil {
		t.Fatalf("bind actual target: %v", err)
	}
	if err = f.store.SetInstructorDispatch(context.Background(), dispatch.ID, "delivered", "", f.now); err != nil {
		t.Fatalf("record transport submission: %v", err)
	}
	_, err = f.service.Report(context.Background(), instructorsvc.ReportInput{
		WorkID: work.ID, SessionID: worker, Kind: "accepted", DispatchID: dispatch.ID,
	})
	if err == nil {
		t.Fatal("attached worker forged receipt of a directive addressed to the project orchestrator")
	}
	updated, err := f.service.Report(context.Background(), instructorsvc.ReportInput{
		WorkID: work.ID, SessionID: orchestrator.ID, Kind: "accepted", DispatchID: dispatch.ID,
	})
	if err != nil || !updated.ProgressAt.Equal(work.ProgressAt) {
		t.Fatalf("actual target receipt failed or became progress: %+v err=%v", updated, err)
	}
	queued, err := f.store.ListInstructorDispatches(context.Background(), result.BatchID, "")
	if err != nil || len(queued) != 1 || queued[0].State != "accepted" {
		t.Fatalf("actual receipt was not durable: %+v err=%v", queued, err)
	}
}

func TestInstructorStopCancelsPreviouslyQueuedWork(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "stop-queue", instructorsvc.DefaultPolicy())
	snapshot := f.snapshot(t)
	first, err := f.service.Command(context.Background(), instructorsvc.CommandInput{
		Command: "Weiter", SnapshotID: snapshot.ID, WorkIDs: []string{work.ID}, IdempotencyKey: "before-stop",
	})
	if err != nil {
		t.Fatalf("enqueue earlier work: %v", err)
	}
	_, err = f.service.Command(context.Background(), instructorsvc.CommandInput{
		Command: "Stopp", SnapshotID: snapshot.ID, WorkIDs: []string{work.ID}, IdempotencyKey: "operator-stop",
	})
	if err != nil {
		t.Fatalf("stop work: %v", err)
	}
	if err = f.service.Tick(context.Background()); err != nil {
		t.Fatalf("deliver stop: %v", err)
	}
	prior, err := f.store.ListInstructorDispatches(context.Background(), first.BatchID, "")
	if err != nil || len(prior) != 1 || prior[0].State != "cancelled" {
		t.Fatalf("earlier directive survived operator stop: %+v err=%v", prior, err)
	}
	if len(f.delivery.calls) != 1 || f.delivery.calls[0].Action != "stopp" {
		t.Fatalf("operator stop delivered unexpected execution: %+v", f.delivery.calls)
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.State != "parked" || details.Work.Phase != "intake" {
		t.Fatalf("stopped work automatically advanced: %+v err=%v", details.Work, err)
	}
	resumeSnapshot := f.snapshot(t)
	resumeInput := instructorsvc.CommandInput{Command: "Weiter", SnapshotID: resumeSnapshot.ID, WorkIDs: []string{work.ID}, IdempotencyKey: "operator-resume"}
	resumed, err := f.service.Command(context.Background(), resumeInput)
	if err != nil || len(resumed.Dispatches) != 1 {
		t.Fatalf("explicit operator resume: %+v err=%v", resumed, err)
	}
	details, err = f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.State != "ready" || details.Work.WaitCondition != "" {
		t.Fatalf("resume did not clear operator pause: %+v err=%v", details.Work, err)
	}
	retried, err := f.service.Command(context.Background(), resumeInput)
	if err != nil || len(retried.Dispatches) != 1 || retried.Dispatches[0].ID != resumed.Dispatches[0].ID {
		t.Fatalf("resume retry duplicated execution: %+v err=%v", retried, err)
	}
}

type fixedWorkspace string

func (w fixedWorkspace) WorkspaceForSession(context.Context, domain.SessionID) (string, error) {
	return string(w), nil
}

func committedWorkspace(t *testing.T) (string, string) {
	t.Helper()
	path := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = path
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "--quiet")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("Independent check fixture\n"), 0o600); err != nil {
		t.Fatalf("write committed fixture: %v", err)
	}
	run("add", "README.md")
	run("-c", "user.name=Independent reviewer", "-c", "user.email=reviewer@example.invalid", "commit", "--quiet", "-m", "Fixture")
	return path, run("rev-parse", "HEAD")
}

func TestInstructorNativeChecksBindRealCleanCommitAndRejectDirtyClaims(t *testing.T) {
	f := newInstructorFixture(t)
	path, sha := committedWorkspace(t)
	policy := instructorsvc.DefaultPolicy()
	workerRole := policy.Roles["worker"]
	workerRole.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = workerRole
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "--verify", "HEAD"}, TimeoutSeconds: 10}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "diff", "--exit-code", "HEAD"}, TimeoutSeconds: 10}
	work := f.register(t, "native-checks", policy)
	worker := f.attachWorker(t, work.ID)
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	updated, err := f.service.RunChecks(context.Background(), work.ID, worker, nil)
	if err != nil || updated.SHA != sha || len(updated.Evidence) != 2 {
		t.Fatalf("actual profiles did not produce exact-commit evidence: %+v err=%v", updated, err)
	}
	for _, evidence := range updated.Evidence {
		if !evidence.Verified || evidence.SHA != sha || evidence.Producer != worker || evidence.Model != "provider/worker" || evidence.Effort != "high" {
			t.Fatalf("check evidence lost actual execution provenance: %+v", evidence)
		}
	}
	if err = os.WriteFile(filepath.Join(path, "README.md"), []byte("Dirty, untested change\n"), 0o600); err != nil {
		t.Fatalf("dirty workspace: %v", err)
	}
	if _, err = f.service.RunChecks(context.Background(), work.ID, worker, []string{"push"}); err == nil {
		t.Fatal("dirty worktree was attested as the committed SHA")
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || len(details.Work.Evidence) != 2 {
		t.Fatalf("rejected dirty claim changed evidence history: %+v err=%v", details.Work.Evidence, err)
	}
}

func TestInstructorFailedNativeProfileNeverCreatesVerifiedEvidence(t *testing.T) {
	f := newInstructorFixture(t)
	path, _ := committedWorkspace(t)
	policy := instructorsvc.DefaultPolicy()
	workerRole := policy.Roles["worker"]
	workerRole.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = workerRole
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "--verify", "definitely-missing-fixture-reference"}, TimeoutSeconds: 10}
	work := f.register(t, "failing-profile", policy)
	worker := f.attachWorker(t, work.ID)
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	if _, err := f.service.RunChecks(context.Background(), work.ID, worker, []string{"push"}); err == nil {
		t.Fatal("failed profile execution was accepted")
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || len(details.Work.Evidence) != 0 || !strings.Contains(details.Work.Summary, "fehlgeschlagen") {
		t.Fatalf("failed execution produced accepted evidence or lost failure: %+v err=%v", details.Work, err)
	}
}

func TestInstructorFailedRerunAtSameCleanCommitRevokesTheEarlierProfilePass(t *testing.T) {
	f := newInstructorFixture(t)
	path, sha := committedWorkspace(t)
	ref := "refs/heads/required-native-check-fixture"
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = path
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("prepare actual rerun condition %v: %v\n%s", args, err, output)
		}
	}
	git("update-ref", ref, sha)
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "--verify", ref}, TimeoutSeconds: 10}
	policy.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "--verify", "HEAD"}, TimeoutSeconds: 10}
	work := f.register(t, "actual-failed-rerun", policy)
	worker := f.attachWorker(t, work.ID)
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	passed, err := f.service.RunChecks(context.Background(), work.ID, worker, nil)
	if err != nil || passed.SHA != sha || len(passed.Evidence) != 2 {
		t.Fatalf("initial real profiles did not pass at immutable commit: %+v err=%v", passed, err)
	}
	git("update-ref", "-d", ref)
	if _, err = f.service.RunChecks(context.Background(), work.ID, worker, []string{"push"}); err == nil {
		t.Fatal("actual failed rerun was accepted as a passing check")
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.SHA != sha || len(details.Work.Evidence) != 2 {
		t.Fatalf("failed rerun lost immutable audit history: %+v err=%v", details.Work, err)
	}
	for _, evidence := range details.Work.Evidence {
		if evidence.Profile == "push" && !evidence.Revoked {
			t.Fatalf("earlier push pass remained accepted after its same-SHA actual rerun failed: %+v", evidence)
		}
		if evidence.Profile == "full" && evidence.Revoked {
			t.Fatalf("failed push rerun revoked an unrelated passing full profile: %+v", evidence)
		}
	}
}

type permissiveNativeValidator struct{}

func (permissiveNativeValidator) ValidateInstructorAgentConfig(context.Context, domain.ProjectID, domain.InstructorRole, string, string, string) error {
	return nil
}

type rejectingNativeValidator struct{}

func (rejectingNativeValidator) ValidateInstructorAgentConfig(context.Context, domain.ProjectID, domain.InstructorRole, string, string, string) error {
	return errors.New("native model does not support requested effort")
}

type cancellingDelivery struct {
	*deliveryRecorder
	cancelErr error
	cancelled []domain.InstructorDispatch
}

func (d *cancellingDelivery) Cancel(_ context.Context, dispatch domain.InstructorDispatch) error {
	d.cancelled = append(d.cancelled, dispatch)
	return d.cancelErr
}

type cachedVerification struct {
	calls             int
	verificationCalls int
	source            string
	discoveryErr      error
}

func (v *cachedVerification) Verify(context.Context, domain.InstructorWork, domain.InstructorEvidence) error {
	v.verificationCalls++
	return errors.New("cache test must not attest an absent native fact")
}

func (v *cachedVerification) Discover(context.Context, domain.InstructorWork) ([]domain.InstructorEvidence, error) {
	v.calls++
	return nil, v.discoveryErr
}

func (v *cachedVerification) VerificationKey(context.Context, domain.InstructorWork) (string, error) {
	return v.source, nil
}

type immutableArtifactVerification struct{}

func (immutableArtifactVerification) Verify(context.Context, domain.InstructorWork, domain.InstructorEvidence) error {
	return nil
}

func (immutableArtifactVerification) Discover(_ context.Context, work domain.InstructorWork) ([]domain.InstructorEvidence, error) {
	return []domain.InstructorEvidence{{WorkID: work.ID, PolicyVersion: work.PolicyVersion, SHA: work.SHA, Kind: "task-packet", Ref: ".ao/task-packets/immutable.md", Producer: work.SessionIDs[0], Role: "worker"}}, nil
}

func (immutableArtifactVerification) VerificationKey(context.Context, domain.InstructorWork) (string, error) {
	return "same-immutable-artifact", nil
}

func TestInstructorReinspectionOfSameArtifactCannotInvalidateItsOwnQueuedPhaseDirective(t *testing.T) {
	f := newInstructorFixture(t)
	verifier := immutableArtifactVerification{}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Verifier: verifier, Clock: func() time.Time { return f.now }})
	work := f.register(t, "autonomous-phase-after-reinspection", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.SHA, w.Phase, w.State = immutableSHA, "implement", "running"
		for _, profile := range []string{"push", "full"} {
			w.Evidence = append(w.Evidence, domain.InstructorEvidence{ID: "trusted-check-" + profile, WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: "check", Profile: profile, Phase: w.Phase, PhaseSequence: w.PhaseSequence, Producer: worker, Role: "worker", Verified: true})
		}
	})
	if err := f.service.VerifyWork(context.Background(), work.ID); err != nil {
		t.Fatalf("inspect immutable artifact before phase transition: %v", err)
	}
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatalf("advance governed phase: %v", err)
	}
	if err := f.service.Shutdown(context.Background()); err != nil {
		t.Fatalf("join harmless original inspection: %v", err)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Verifier: verifier, Clock: func() time.Time { return f.now }})
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.Phase != "review" {
		t.Fatalf("checks did not produce autonomous review phase: %+v err=%v", details.Work, err)
	}
	f.now = f.now.Add(31 * time.Second)
	if err = f.service.VerifyWork(context.Background(), work.ID); err != nil {
		t.Fatalf("reinspect same artifact in next phase: %v", err)
	}
	if err = f.service.Tick(context.Background()); err != nil {
		t.Fatalf("deliver autonomous phase after same-fact inspection: %v", err)
	}
	if err = f.service.Shutdown(context.Background()); err != nil {
		t.Fatalf("join final same-fact inspection: %v", err)
	}
	if len(f.delivery.calls) != 1 || f.delivery.calls[0].Action != "weiter" {
		t.Fatalf("unchanged immutable artifact stranded its own phase directive: deliveries=%+v", f.delivery.calls)
	}
	details, err = f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.Gate != nil || details.Work.State == "waiting_human" {
		t.Fatalf("same-fact inspection inflated a transport gate: %+v err=%v", details.Work, err)
	}
}

func TestInstructorPublishedNativeCIFailureReentersTheHumanApprovedBoundedFixLoop(t *testing.T) {
	for _, rounds := range []int{1, 2} {
		t.Run(fmt.Sprintf("prior-fix-rounds-%d", rounds), func(t *testing.T) {
			f := newInstructorFixture(t)
			work := f.register(t, "published-ci-correction", instructorsvc.DefaultPolicy())
			worker := f.attachWorker(t, work.ID)
			f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
				w.State, w.Phase, w.SHA, w.Branch = "running", "closeout", immutableSHA, "feature/published-ci"
				w.PhaseSequence, w.DeltaPhaseSequence, w.FixPhaseSequence, w.FixRounds = 4, 3, 2, rounds
				w.PublicationGrants = []domain.InstructorPublicationGrant{{GateID: "native-publication", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Branch: w.Branch, SnapshotID: "native-publication-snapshot", WorkRevision: w.Revision, Approval: "PUBLISH-GO " + w.Branch, ApprovedAt: f.now}}
				w.Evidence = append(w.Evidence, domain.InstructorEvidence{ID: "actual-ci-failure", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: "ci-failed", Producer: worker, Role: "worker", Ref: "trusted:native-ci-failure", Verified: true, Outcome: "failed"})
			})
			if _, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "done"}); err == nil {
				t.Fatal("publication authorization concealed a trusted current CI failure")
			}
			if err := f.service.Tick(context.Background()); err != nil {
				t.Fatalf("re-enter CI correction triage: %v", err)
			}
			details, err := f.service.Details(context.Background(), work.ID)
			if err != nil || details.Work.Phase != "triage" || details.Work.FixRounds != rounds {
				t.Fatalf("native CI failure remained trapped in closeout or consumed an unapproved fix: %+v err=%v", details.Work, err)
			}
			if err = f.service.Tick(context.Background()); err != nil {
				t.Fatalf("open human CI correction decision: %v", err)
			}
			details, err = f.service.Details(context.Background(), work.ID)
			if err != nil || details.Work.State != "waiting_human" || details.Work.Gate == nil || details.Work.Gate.TransitionTo != "fix" || details.Work.FixRounds != rounds {
				t.Fatalf("CI correction bypassed its mandatory human decision: %+v err=%v", details.Work, err)
			}
			if _, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{work.ID}, IdempotencyKey: "approve-ci-correction"}); err != nil {
				t.Fatalf("record human CI correction approval: %v", err)
			}
			if err = f.service.Tick(context.Background()); err != nil {
				t.Fatalf("apply bounded approved CI correction: %v", err)
			}
			details, err = f.service.Details(context.Background(), work.ID)
			if err != nil {
				t.Fatal(err)
			}
			if rounds == 1 && (details.Work.Phase != "fix" || details.Work.FixRounds != 2 || details.Work.State != "ready") {
				t.Fatalf("available approved second correction did not progress: %+v", details.Work)
			}
			if rounds == 2 && (details.Work.Phase == "fix" || details.Work.FixRounds != 2 || details.Work.State != "blocked") {
				t.Fatalf("post-publication CI correction exceeded the same governed fix budget: %+v", details.Work)
			}
		})
	}
}

func TestInstructorIncompleteDiscoveryCannotCertifyPreviouslyAcceptedProofForNewSource(t *testing.T) {
	f := newInstructorFixture(t)
	verifier := &cachedVerification{source: "previous-native-source"}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Verifier: verifier, Clock: func() time.Time { return f.now }})
	work := f.register(t, "incomplete-source-inspection", instructorsvc.DefaultPolicy())
	work = f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.SHA = immutableSHA
		w.Evidence = []domain.InstructorEvidence{{ID: "previously-accepted-ci", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: "ci", Ref: "trusted:previous-native-source", Verified: true}}
	})
	if err := f.service.VerifyWork(context.Background(), work.ID); err != nil {
		t.Fatalf("inspect original source: %v", err)
	}
	f.now = f.now.Add(31 * time.Second)
	verifier.source, verifier.discoveryErr = "changed-native-source", context.Canceled
	if err := f.service.VerifyWork(context.Background(), work.ID); err != nil {
		t.Fatalf("persist incomplete inspection outcome: %v", err)
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.Verification == nil || details.Work.Verification.Error == "" {
		t.Fatalf("incomplete native inspection lost its actual error: %+v err=%v", details.Work.Verification, err)
	}
	if err = f.service.CurrentEvidence(context.Background(), details.Work); err == nil {
		t.Fatal("failed discovery certified old proofs against a changed native source without inspecting them")
	}
}

func TestInstructorEmptyDiscoveryRevalidatesAndRevokesVanishedAcceptedSource(t *testing.T) {
	f := newInstructorFixture(t)
	verifier := &cachedVerification{source: "authoritative-source-was-deleted"}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Verifier: verifier, Clock: func() time.Time { return f.now }})
	work := f.register(t, "vanished-authoritative-source", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.SHA = immutableSHA
		w.Evidence = []domain.InstructorEvidence{{ID: "previously-accepted-source", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: "ci", Producer: worker, Role: "worker", Ref: "trusted:source-now-absent", Verified: true}}
	})
	if err := f.service.VerifyWork(context.Background(), work.ID); err != nil {
		t.Fatalf("reinspect deleted source: %v", err)
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.verificationCalls != 1 || len(details.Work.Evidence) != 1 || !details.Work.Evidence[0].Revoked || !strings.Contains(details.Work.Evidence[0].RevocationReason, "absent native fact") {
		t.Fatalf("empty discovery preserved accepted proof of a vanished native source: %+v", details.Work.Evidence)
	}
	if details.Work.Verification == nil || !details.Work.Verification.Inspected || details.Work.Verification.Claims != 1 || details.Work.Verification.Verified != 0 {
		t.Fatalf("deleted source was not fully revalidated and rejected: %+v", details.Work.Verification)
	}
}

func TestInstructorVerificationBudgetSurvivesHeartbeatAndRestartButTracksSourceAndDay(t *testing.T) {
	f := newInstructorFixture(t)
	verifier := &cachedVerification{source: "immutable-native-source-one"}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Verifier: verifier, Clock: func() time.Time { return f.now }})
	work := f.register(t, "durable-verification-budget", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) { w.SHA = immutableSHA })
	verify := func() {
		t.Helper()
		if err := f.service.VerifyWork(context.Background(), work.ID); err != nil {
			t.Fatalf("native verification pass: %v", err)
		}
	}
	verify()
	verify()
	if verifier.calls != 1 {
		t.Fatalf("verification ignored bounded retry delay: calls=%d", verifier.calls)
	}
	f.now = f.now.Add(31 * time.Second)
	verify()
	f.now = f.now.Add(31 * time.Second)
	if _, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "heartbeat", Summary: "Still connected"}); err != nil {
		t.Fatalf("heartbeat before cached verification: %v", err)
	}
	verify()
	if verifier.calls != 2 {
		t.Fatalf("unchanged source spent more than two daily deep passes: calls=%d", verifier.calls)
	}
	before, err := f.service.Details(context.Background(), work.ID)
	if err != nil || before.Work.Verification == nil || before.Work.Verification.Passes != 2 || before.Work.Verification.MaxPasses != 2 {
		t.Fatalf("verification budget unavailable to status: %+v err=%v", before.Work.Verification, err)
	}
	if err = f.store.Close(); err != nil {
		t.Fatalf("close verification cache: %v", err)
	}
	f.store, err = sqlite.OpenPreMigrated(f.dataDir)
	if err != nil {
		t.Fatalf("restart verification cache: %v", err)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Verifier: verifier, Clock: func() time.Time { return f.now }})
	verify()
	if verifier.calls != 2 {
		t.Fatalf("daemon restart reset durable verification budget: calls=%d", verifier.calls)
	}
	verifier.source = "immutable-native-source-two"
	verify()
	changed, err := f.service.Details(context.Background(), work.ID)
	if err != nil || verifier.calls != 3 || changed.Work.Verification == nil || changed.Work.Verification.Passes != 1 || changed.Work.Verification.Key == before.Work.Verification.Key {
		t.Fatalf("changed actual source did not create a distinct bounded inspection: %+v calls=%d err=%v", changed.Work.Verification, verifier.calls, err)
	}
	f.now = f.now.Add(24 * time.Hour)
	verify()
	nextDay, err := f.service.Details(context.Background(), work.ID)
	if err != nil || verifier.calls != 4 || nextDay.Work.Verification == nil || nextDay.Work.Verification.Passes != 1 || nextDay.Work.Verification.Key == changed.Work.Verification.Key {
		t.Fatalf("daily inspection did not renew its bounded source key: %+v calls=%d err=%v", nextDay.Work.Verification, verifier.calls, err)
	}
}

func TestInstructorLateVerificationAttemptCannotCompleteANewerSQLiteLease(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "verification-lease-fence", instructorsvc.DefaultPolicy())
	first, claimed, err := f.store.ClaimInstructorVerification(context.Background(), work.ID, "same-source-key", f.now)
	if err != nil || !claimed || first.Passes != 1 {
		t.Fatalf("claim first attempt: %+v claimed=%v err=%v", first, claimed, err)
	}
	f.now = f.now.Add(91 * time.Second)
	second, claimed, err := f.store.ClaimInstructorVerification(context.Background(), work.ID, "same-source-key", f.now)
	if err != nil || !claimed || second.Passes != 2 {
		t.Fatalf("claim recovery attempt: %+v claimed=%v err=%v", second, claimed, err)
	}
	if err = f.store.FinishInstructorVerification(context.Background(), work.ID, first.Key, first.Passes, "complete", "late first response", 1, 1, f.now, true); err != nil {
		t.Fatalf("finish stale first response: %v", err)
	}
	current, found, err := f.store.GetInstructorVerification(context.Background(), work.ID)
	if err != nil || !found || current.Passes != 2 || current.Status != "running" {
		t.Fatalf("late first response completed a different lease: %+v found=%v err=%v", current, found, err)
	}
	if err = f.store.FinishInstructorVerification(context.Background(), work.ID, second.Key, second.Passes, "failed", "current provider failure", 1, 0, f.now, true); err != nil {
		t.Fatalf("finish current recovery attempt: %v", err)
	}
	current, found, err = f.store.GetInstructorVerification(context.Background(), work.ID)
	if err != nil || !found || current.Status != "failed" || current.Error != "current provider failure" {
		t.Fatalf("current failure was overwritten or lost: %+v found=%v err=%v", current, found, err)
	}
	f.now = f.now.Add(time.Minute)
	if _, claimed, err = f.store.ClaimInstructorVerification(context.Background(), work.ID, second.Key, f.now); err != nil || claimed {
		t.Fatalf("lease recovery exceeded the two-pass source budget: claimed=%v err=%v", claimed, err)
	}
}

func TestInstructorHeartbeatIsAvailabilityWithoutProgressOrSnapshotMutation(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "heartbeat-boundary", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	oldProgress, oldNudge := f.now.Add(-20*time.Minute), f.now.Add(-15*time.Minute)
	before := f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.State, w.Phase, w.ProgressAt, w.NudgedAt = "running", "implement", oldProgress, &oldNudge
		w.Summary, w.Next = "Work result awaiting review", "review"
	})
	snapshot := f.snapshot(t)
	f.now = f.now.Add(2 * time.Second)
	updated, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "heartbeat", Summary: "Agent remains connected"})
	if err != nil || updated.Revision != before.Revision || !updated.ProgressAt.Equal(before.ProgressAt) || updated.NudgedAt == nil || !updated.NudgedAt.Equal(oldNudge) || updated.Summary != before.Summary || updated.Next != before.Next {
		t.Fatalf("availability report changed work progress or revision: before=%+v after=%+v err=%v", before, updated, err)
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.Heartbeat == nil || details.Work.Heartbeat.SessionID != worker || !details.Work.Heartbeat.At.Equal(f.now) || details.Work.Revision != before.Revision {
		t.Fatalf("heartbeat was not independently persisted: %+v err=%v", details.Work, err)
	}
	delta, err := f.service.Snapshot(context.Background(), snapshot.ID)
	if err != nil || len(delta.Works) != 0 {
		t.Fatalf("heartbeat invalidated the frozen work revision: %+v err=%v", delta, err)
	}
	if _, err = f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "heartbeat", Phase: "review"}); err == nil {
		t.Fatal("heartbeat payload changed the business phase")
	}
}

func TestInstructorNamesRemainUniqueWhileStableOriginsRetry(t *testing.T) {
	f := newInstructorFixture(t)
	first := f.register(t, "Distinct name", instructorsvc.DefaultPolicy())
	retry, err := f.service.Register(context.Background(), instructorsvc.RegisterInput{ProjectID: "governed", Name: "New display name", Goal: "Retry the same origin", Source: "manual", SourceRef: "Distinct name"})
	if err != nil || retry.ID != first.ID || retry.Name != first.Name {
		t.Fatalf("origin retry changed durable identity/name: %+v err=%v", retry, err)
	}
	if _, err = f.service.Register(context.Background(), instructorsvc.RegisterInput{ProjectID: "governed", Name: " distinct NAME ", Goal: "Different work", Source: "manual", SourceRef: "different-origin"}); err == nil {
		t.Fatal("new origin reused another work's case-insensitive name")
	}
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
	if _, err = f.service.SetPolicy(context.Background(), policy); err != nil {
		t.Fatalf("set manual launch mapping: %v", err)
	}
	ids := []string{}
	names := map[string]bool{}
	for range 2 {
		id, spawnErr := f.service.PrepareSpawn(context.Background(), instructorsvc.SpawnInput{ProjectID: "governed", Role: "worker", Harness: "opencode", Model: "provider/worker", Effort: "high"})
		if spawnErr != nil {
			t.Fatalf("unnamed manual admission failed: %v", spawnErr)
		}
		details, readErr := f.service.Details(context.Background(), id)
		if readErr != nil || details.Work.Name == "" || names[details.Work.Name] {
			t.Fatalf("manual admissions lack distinct visible names: %+v err=%v", details.Work, readErr)
		}
		ids = append(ids, id)
		names[details.Work.Name] = true
	}
	if ids[0] == ids[1] {
		t.Fatal("independent manual admissions collapsed into one work")
	}
}

func TestInstructorStopSettlesEveryAlreadyQueuedNativeAttemptBeforeResume(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprintf("uncertain-%v", uncertain), func(t *testing.T) {
			f := newInstructorFixture(t)
			delivery := &cancellingDelivery{deliveryRecorder: f.delivery}
			if uncertain {
				delivery.cancelErr = instructorsvc.ErrDeliveryUncertain
			}
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: delivery, Clock: func() time.Time { return f.now }})
			work := f.register(t, "native-stop", instructorsvc.DefaultPolicy())
			target, err := f.store.CreateSession(context.Background(), domain.SessionRecord{ProjectID: "governed", Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, CreatedAt: f.now, UpdatedAt: f.now})
			if err != nil {
				t.Fatalf("seed native target: %v", err)
			}
			f.delivery.err = instructorsvc.ErrDeliveryQueued
			f.delivery.bind = func(ctx context.Context, dispatch domain.InstructorDispatch) error {
				return f.service.BindDispatchTarget(ctx, dispatch.ID, target.ID)
			}
			for i, command := range []string{"Weiter", "Pruefen"} {
				if _, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: command, SnapshotID: f.snapshot(t).ID, WorkIDs: []string{work.ID}, IdempotencyKey: fmt.Sprintf("queued-native-%d", i)}); err != nil {
					t.Fatalf("enqueue native attempt %d: %v", i, err)
				}
				if err = f.service.Tick(context.Background()); err != nil {
					t.Fatalf("persist native queued attempt %d: %v", i, err)
				}
			}
			if _, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Stopp", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{work.ID}, IdempotencyKey: "native-stop"}); err != nil {
				t.Fatalf("stop already queued attempts: %v", err)
			}
			details, err := f.service.Details(context.Background(), work.ID)
			if err != nil || details.Work.State != "parked" || len(delivery.cancelled) != 2 {
				t.Fatalf("Stop missed a native pending attempt: work=%+v cancelled=%+v err=%v", details.Work, delivery.cancelled, err)
			}
			if !uncertain {
				if details.Work.Gate != nil || len(details.Work.UncertainDispatchIDs) != 0 {
					t.Fatalf("confirmed cancellations left an unnecessary uncertainty: %+v", details.Work)
				}
				return
			}
			if details.Work.Gate == nil || len(details.Work.UncertainDispatchIDs) != 2 {
				t.Fatalf("uncertain cancellations lost native attempts: %+v", details.Work)
			}
			for i := range 2 {
				dispatchID := details.Work.Gate.DispatchID
				if _, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Weiter", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{work.ID}, IdempotencyKey: fmt.Sprintf("premature-resume-%d", i)}); err == nil {
					t.Fatal("parked work resumed before every uncertain native attempt was settled")
				}
				settled, reconcileErr := f.service.ReconcileDelivery(context.Background(), instructorsvc.ReconcileInput{SnapshotID: f.snapshot(t).ID, WorkID: work.ID, DispatchID: dispatchID, Accepted: false, Note: "Operator confirmed this attempt was not accepted"})
				if reconcileErr != nil || settled.State != "parked" || len(settled.UncertainDispatchIDs) != 1-i {
					t.Fatalf("reconcile %d changed pause or lost remaining uncertainty: %+v err=%v", i, settled, reconcileErr)
				}
				details.Work = settled
				if i == 0 && (settled.Gate == nil || settled.Gate.DispatchID == dispatchID) {
					t.Fatalf("first receipt failed to preserve the other pending native attempt: %+v", settled)
				}
			}
			if details.Work.Gate != nil {
				t.Fatalf("all reconciled native attempts left an uncertainty: %+v", details.Work)
			}
		})
	}
}

func TestInstructorExceptionCannotRewriteAHistoricallyPublishedWorkPolicy(t *testing.T) {
	for _, currentSHA := range []string{immutableSHA, "2222222222222222222222222222222222222222"} {
		t.Run(currentSHA[:8], func(t *testing.T) {
			f := newInstructorFixture(t)
			work := f.register(t, "published-policy", instructorsvc.DefaultPolicy())
			f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
				w.SHA, w.Branch = currentSHA, "feature/published"
				w.PublicationGrants = []domain.InstructorPublicationGrant{{GateID: "historical-publication", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: immutableSHA, Branch: w.Branch, SnapshotID: "historical-snapshot", WorkRevision: w.Revision, Approval: "PUBLISH-GO " + w.Branch, ApprovedAt: f.now.Add(-time.Hour)}}
			})
			budget := 3
			input := instructorsvc.ExceptionInput{SnapshotID: f.snapshot(t).ID, WorkID: work.ID, MaxFixRounds: &budget, Reason: "Attempt to weaken a published policy", ExpiresAt: f.now.Add(time.Hour)}
			if _, err := f.service.PreviewException(context.Background(), input); err == nil {
				t.Fatal("published work policy received a new exception preview")
			}
			if _, err := f.service.ApplyException(context.Background(), input); err == nil {
				t.Fatal("published work policy received a new exception grant")
			}
		})
	}
}

func TestInstructorExceptionNeedsExactApprovalAndCurrentSnapshot(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "exception-approval", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	snapshot := f.snapshot(t)
	budget := 3
	input := instructorsvc.ExceptionInput{SnapshotID: snapshot.ID, WorkID: work.ID, MaxFixRounds: &budget, Reason: "One additional bounded fix for this work", ExpiresAt: f.now.Add(time.Hour)}
	preview, err := f.service.PreviewException(context.Background(), input)
	if err != nil || preview.ExactApproval == "" || preview.ProposalHash == "" {
		t.Fatalf("preview scoped exception: %+v err=%v", preview, err)
	}
	before, err := f.service.Details(context.Background(), work.ID)
	if err != nil || before.Work.Policy.MaxFixRounds != 2 || len(before.Work.Exceptions) != 0 {
		t.Fatalf("preview mutated work policy: %+v err=%v", before.Work, err)
	}
	input.Approval = "Go"
	if _, err = f.service.ApplyException(context.Background(), input); err == nil {
		t.Fatal("generic Go granted a scoped exception")
	}
	input.Approval = preview.ExactApproval
	if _, err = f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "progress", Summary: "Work changed after exception preview"}); err != nil {
		t.Fatalf("change snapshot-bound work: %v", err)
	}
	if _, err = f.service.ApplyException(context.Background(), input); !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("stale exception preview error = %v, want conflict", err)
	}
	after, err := f.service.Details(context.Background(), work.ID)
	if err != nil || after.Work.Policy.MaxFixRounds != 2 || len(after.Work.Exceptions) != 0 {
		t.Fatalf("rejected approvals changed work: %+v err=%v", after.Work, err)
	}
}

func TestInstructorExceptionIsScopedAndPersistsWithoutWeakeningGlobalPolicy(t *testing.T) {
	f := newInstructorFixture(t)
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
	policy := instructorsvc.DefaultPolicy()
	workerRole := policy.Roles["worker"]
	workerRole.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = workerRole
	work := f.register(t, "exception-selected", policy)
	other := f.register(t, "exception-unselected", policy)
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.SHA = immutableSHA
		w.ApprovedTransitions = []string{"fix"}
		w.Evidence = []domain.InstructorEvidence{{ID: "old-proof", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: immutableSHA, Kind: "push", Profile: "push", Verified: true}}
	})
	selection := domain.InstructorAgentSelection{Harness: "codex", Model: "codex/exception-worker", Effort: "low"}
	budget := 3
	input := instructorsvc.ExceptionInput{SnapshotID: f.snapshot(t).ID, WorkID: work.ID, Role: "worker", Selection: &selection, MaxFixRounds: &budget, Reason: "Temporary provider fallback and one additional fix", ExpiresAt: f.now.Add(time.Hour)}
	preview, err := f.service.PreviewException(context.Background(), input)
	if err != nil {
		t.Fatalf("preview work exception: %v", err)
	}
	input.Approval = preview.ExactApproval
	granted, err := f.service.ApplyException(context.Background(), input)
	if err != nil || granted.PolicyVersion == work.PolicyVersion || len(granted.Exceptions) != 2 || granted.Policy.MaxFixRounds != 3 || len(granted.ApprovedTransitions) != 0 {
		t.Fatalf("grant did not pin new scoped policy and clear old approvals: %+v err=%v", granted, err)
	}
	if len(granted.Evidence) != 1 || granted.Evidence[0].PolicyVersion == granted.PolicyVersion {
		t.Fatalf("exception relabeled prior-policy evidence: %+v", granted.Evidence)
	}
	for _, grant := range granted.Exceptions {
		if grant.SnapshotID != input.SnapshotID || grant.ProposalHash != preview.ProposalHash || grant.Approval != input.Approval || grant.SHA != immutableSHA || grant.BeforePolicyVersion == grant.AfterPolicyVersion || grant.Reason != input.Reason {
			t.Fatalf("exception lost immutable audit bindings: %+v", grant)
		}
	}
	if err = f.store.Close(); err != nil {
		t.Fatalf("close granted exception fixture: %v", err)
	}
	f.store, err = sqlite.OpenPreMigrated(f.dataDir)
	if err != nil {
		t.Fatalf("restart granted exception fixture: %v", err)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
	spawn := instructorsvc.SpawnInput{ProjectID: "governed", WorkID: work.ID, Role: "worker", Harness: selection.Harness, Model: selection.Model, Effort: selection.Effort}
	if _, err = f.service.PrepareSpawn(context.Background(), spawn); err != nil {
		t.Fatalf("persisted exact work exception rejected approved selection: %v", err)
	}
	spawn.WorkID = other.ID
	if _, err = f.service.PrepareSpawn(context.Background(), spawn); err == nil {
		t.Fatal("work exception changed an unselected work's launch policy")
	}
	global, err := f.service.GetPolicy(context.Background())
	if err != nil || global.MaxFixRounds != 2 || global.ExceptionVersion != "" || global.Roles["worker"].Harness != "opencode" {
		t.Fatalf("scoped exception changed global policy: %+v err=%v", global, err)
	}
	weakened := instructorsvc.DefaultPolicy()
	weakened.MaxFixRounds, weakened.Version = 3, ""
	if _, err = f.service.SetPolicy(context.Background(), weakened); err == nil {
		t.Fatal("global policy accepted the scoped exception fix budget")
	}
	if _, err = f.service.Register(context.Background(), instructorsvc.RegisterInput{ProjectID: "governed", Name: "Forged exception", Goal: "Forged exception", Source: "manual", SourceRef: "forged-exception", Policy: granted.Policy}); err == nil {
		t.Fatal("registration treated exception policy as an unrestricted repository policy")
	}
}

func TestInstructorExceptionRetainsReviewerIndependenceAndChecksNativeCapabilities(t *testing.T) {
	for _, rejectNative := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject-native-%v", rejectNative), func(t *testing.T) {
			f := newInstructorFixture(t)
			var validator instructorsvc.RoleValidator = permissiveNativeValidator{}
			if rejectNative {
				validator = rejectingNativeValidator{}
			}
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: validator, Clock: func() time.Time { return f.now }})
			work := f.register(t, "reviewer-exception", instructorsvc.DefaultPolicy())
			selection := domain.InstructorAgentSelection{Harness: "codex", Model: "codex/reviewer", Effort: "high"}
			input := instructorsvc.ExceptionInput{SnapshotID: f.snapshot(t).ID, WorkID: work.ID, Role: "reviewer", Selection: &selection, Reason: "Use this explicitly supported reviewer effort", ExpiresAt: f.now.Add(time.Hour)}
			preview, err := f.service.PreviewException(context.Background(), input)
			if rejectNative {
				if err == nil {
					t.Fatal("exception skipped native model capability validation")
				}
				return
			}
			if err != nil || !preview.EffectivePolicy.Roles["reviewer"].Independent {
				t.Fatalf("reviewer exception weakened independence: %+v err=%v", preview, err)
			}
			input.Approval = preview.ExactApproval
			granted, err := f.service.ApplyException(context.Background(), input)
			if err != nil || !granted.Policy.Roles["reviewer"].Independent {
				t.Fatalf("granted reviewer exception weakened independence: %+v err=%v", granted, err)
			}
		})
	}
}

func TestInstructorExpiredExceptionBlocksEveryExecutionBoundary(t *testing.T) {
	f := newInstructorFixture(t)
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
	work := f.register(t, "expired-exception", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	budget := 3
	input := instructorsvc.ExceptionInput{SnapshotID: f.snapshot(t).ID, WorkID: work.ID, MaxFixRounds: &budget, Reason: "One short-lived additional fix", ExpiresAt: f.now.Add(time.Minute)}
	preview, err := f.service.PreviewException(context.Background(), input)
	if err != nil {
		t.Fatalf("preview expiring grant: %v", err)
	}
	input.Approval = preview.ExactApproval
	if _, err = f.service.ApplyException(context.Background(), input); err != nil {
		t.Fatalf("apply expiring grant: %v", err)
	}
	f.now = input.ExpiresAt
	assertExpired := func(boundary string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "expired") {
			t.Fatalf("%s did not enforce exception expiry: %v", boundary, err)
		}
	}
	_, err = f.service.PrepareSpawn(context.Background(), instructorsvc.SpawnInput{ProjectID: "governed", WorkID: work.ID, Role: "worker", Harness: "opencode", Model: "provider/worker", Effort: "high"})
	assertExpired("launch", err)
	assertExpired("resume", f.service.ValidateResume(context.Background(), worker))
	_, err = f.service.RunChecks(context.Background(), work.ID, worker, []string{"push"})
	assertExpired("native check", err)
	_, err = f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "progress", Phase: "implement"})
	assertExpired("phase transition", err)
	_, err = f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "done"})
	assertExpired("closeout", err)
	_, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Weiter", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{work.ID}, IdempotencyKey: "expired-weiter"})
	if err == nil {
		t.Fatal("operator continuation accepted work with an expired exception")
	}
	queued, err := f.store.ListInstructorDispatches(context.Background(), "", "")
	if err != nil || len(queued) != 0 {
		t.Fatalf("expired exception queued native execution: %+v err=%v", queued, err)
	}
}

func TestInstructorQueuedContinuationCannotOutliveScopedException(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "queued-expired-exception", instructorsvc.DefaultPolicy())
	budget := 3
	input := instructorsvc.ExceptionInput{SnapshotID: f.snapshot(t).ID, WorkID: work.ID, MaxFixRounds: &budget, Reason: "Brief additional fix budget", ExpiresAt: f.now.Add(time.Minute)}
	preview, err := f.service.PreviewException(context.Background(), input)
	if err != nil {
		t.Fatalf("preview queued exception: %v", err)
	}
	input.Approval = preview.ExactApproval
	if _, err = f.service.ApplyException(context.Background(), input); err != nil {
		t.Fatalf("apply queued exception: %v", err)
	}
	if _, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Weiter", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{work.ID}, IdempotencyKey: "queued-before-expiry"}); err != nil {
		t.Fatalf("queue continuation while grant is valid: %v", err)
	}
	f.now = input.ExpiresAt
	if err = f.service.Tick(context.Background()); err != nil {
		t.Fatalf("tick expired queued continuation: %v", err)
	}
	if len(f.delivery.calls) != 0 {
		t.Fatalf("queued continuation executed after its exception expired: %+v", f.delivery.calls)
	}
}

func TestInstructorRepeatedOriginCannotRelaunchAClosedOrGatedWork(t *testing.T) {
	for _, state := range []string{"done", "blocked", "waiting_human", "parked", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			f := newInstructorFixture(t)
			policy := instructorsvc.DefaultPolicy()
			workerRole := policy.Roles["worker"]
			workerRole.EffortByModel = map[string]string{"provider/worker": "high"}
			policy.Roles["worker"] = workerRole
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
			if _, err := f.service.SetPolicy(context.Background(), policy); err != nil {
				t.Fatalf("approve native maximum mapping: %v", err)
			}
			work, err := f.service.Register(context.Background(), instructorsvc.RegisterInput{
				ProjectID: "governed", Source: "issue", SourceRef: "same-stable-issue", Name: "Issue work", Goal: "Original issue work",
			})
			if err != nil {
				t.Fatalf("register stable issue work: %v", err)
			}
			f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) { w.State = state })
			_, err = f.service.PrepareSpawn(context.Background(), instructorsvc.SpawnInput{
				ProjectID: "governed", Source: "issue", SourceRef: "same-stable-issue", Name: "Repeated launch", Goal: "Original issue work",
				Role: "worker", Harness: "opencode", Model: "provider/worker", Effort: "high",
			})
			if err == nil {
				t.Fatalf("same stable origin reopened %s work without an explicit permitted transition", state)
			}
		})
	}
}

func TestInstructorRecurringAutomationNamesRemainDistinctAcrossOccurrences(t *testing.T) {
	for _, name := range []string{"Nightly issue check", "🔬🔬" + strings.Repeat("夜", 38)} {
		t.Run(name, func(t *testing.T) {
			f := newInstructorFixture(t)
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
			policy := instructorsvc.DefaultPolicy()
			role := policy.Roles["worker"]
			role.EffortByModel = map[string]string{"provider/worker": "high"}
			policy.Roles["worker"] = role
			if _, err := f.service.SetPolicy(context.Background(), policy); err != nil {
				t.Fatal(err)
			}
			input := instructorsvc.SpawnInput{ProjectID: "governed", Source: "automation", SourceRef: "scheduled-occurrence-one", Name: name, Goal: "Check the same issue every night", Role: "worker", Harness: "opencode", Model: "provider/worker", Effort: "high"}
			firstID, err := f.service.PrepareSpawn(context.Background(), input)
			if err != nil {
				t.Fatalf("first scheduled occurrence: %v", err)
			}
			retryID, err := f.service.PrepareSpawn(context.Background(), input)
			if err != nil || retryID != firstID {
				t.Fatalf("same occurrence lost its stable identity: first=%s retry=%s err=%v", firstID, retryID, err)
			}
			f.trustedFacts(t, firstID, func(w *domain.InstructorWork) { w.State = "done" })
			input.SourceRef = "scheduled-occurrence-two"
			secondID, err := f.service.PrepareSpawn(context.Background(), input)
			if err != nil || secondID == firstID {
				t.Fatalf("next occurrence of a fixed automation definition cannot create independent work: first=%s second=%s err=%v", firstID, secondID, err)
			}
			secondRetryID, err := f.service.PrepareSpawn(context.Background(), input)
			if err != nil || secondRetryID != secondID {
				t.Fatalf("next occurrence retry lost its stable identity: first=%s retry=%s err=%v", secondID, secondRetryID, err)
			}
			input.SourceRef = "scheduled-occurrence-one"
			closedRetryID, err := f.service.PrepareSpawn(context.Background(), input)
			if err == nil || closedRetryID != firstID {
				t.Fatalf("retry of a closed earlier occurrence relaunched or changed its identity: want=%s got=%s err=%v", firstID, closedRetryID, err)
			}
			first, err := f.service.Details(context.Background(), firstID)
			if err != nil {
				t.Fatal(err)
			}
			second, err := f.service.Details(context.Background(), secondID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.EqualFold(first.Work.Name, second.Work.Name) || first.Work.SourceRef != "scheduled-occurrence-one" || second.Work.SourceRef != "scheduled-occurrence-two" {
				t.Fatalf("occurrences are not independently named and bound: first=%+v second=%+v", first.Work, second.Work)
			}
			for _, work := range []domain.InstructorWork{first.Work, second.Work} {
				if len(work.Name) > 120 || !utf8.ValidString(work.Name) {
					t.Fatalf("automatic work name exceeds its byte limit or splits a Unicode character: %q", work.Name)
				}
			}
		})
	}
}

func TestInstructorCorrectiveWorkerAdmissionCannotBeatTheHumanFixGate(t *testing.T) {
	for _, scenario := range []string{"ready-without-grant", "running-without-grant", "granted-with-budget", "granted-budget-exhausted"} {
		t.Run(scenario, func(t *testing.T) {
			f := newInstructorFixture(t)
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
			policy := instructorsvc.DefaultPolicy()
			role := policy.Roles["worker"]
			role.EffortByModel = map[string]string{"provider/worker": "high"}
			policy.Roles["worker"] = role
			work := f.register(t, "corrective-native-admission", policy)
			worker := f.attachWorker(t, work.ID)
			f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
				w.Phase, w.State, w.FixRounds = "triage", "ready", 1
				w.SHA = immutableSHA
				if strings.HasPrefix(scenario, "running") {
					w.State = "running"
				}
				if strings.HasPrefix(scenario, "granted") {
					w.ApprovedTransitions = []string{"triage->fix"}
					w.Evidence = append(w.Evidence, domain.InstructorEvidence{ID: "actual-human-triage", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: "triage", Producer: "operator", Role: "instructor", Ref: "gate:approved-human-fix", Verified: true})
				}
				if scenario == "granted-budget-exhausted" {
					w.FixRounds = 2
				}
				// No Tick has yet materialized the gate: admission must enforce
				// the declared contract, independently of the displayed gate.
				w.Gate = nil
			})
			_, spawnErr := f.service.PrepareSpawn(context.Background(), instructorsvc.SpawnInput{ProjectID: "governed", WorkID: work.ID, Role: "worker", Harness: "opencode", Model: "provider/worker", Effort: "high"})
			resumeErr := f.service.ValidateResume(context.Background(), worker)
			queuedErr := f.service.ValidateDispatchAdmission(context.Background(), worker, "queued-native-corrective-worker-turn")
			if scenario == "granted-with-budget" {
				if spawnErr != nil || resumeErr != nil || queuedErr != nil {
					t.Fatalf("exact approved bounded correction was blocked: spawn=%v resume=%v queued=%v", spawnErr, resumeErr, queuedErr)
				}
				return
			}
			if spawnErr == nil || resumeErr == nil || queuedErr == nil {
				t.Fatalf("worker execution bypassed required human correction or fix budget before Tick: spawn=%v resume=%v queued=%v", spawnErr, resumeErr, queuedErr)
			}
		})
	}
}

func TestInstructorProgressCannotReleaseAnOperatorPauseOrHumanGate(t *testing.T) {
	for _, state := range []string{"parked", "waiting_human", "blocked"} {
		t.Run(state, func(t *testing.T) {
			f := newInstructorFixture(t)
			work := f.register(t, "paused-progress", instructorsvc.DefaultPolicy())
			worker := f.attachWorker(t, work.ID)
			f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
				w.State = state
				w.WaitCondition = "Operator decision"
				if state == "waiting_human" {
					w.Gate = &domain.InstructorGate{ID: "human-gate", Kind: "transition", Title: "Human decision", TransitionTo: "implement", OpenedAt: f.now}
				}
			})
			updated, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "progress", Summary: "Old process completed an operation"})
			if err != nil {
				t.Fatalf("record final observation from already running process: %v", err)
			}
			if updated.State != state || updated.WaitCondition != "Operator decision" || (state == "waiting_human" && updated.Gate == nil) {
				t.Fatalf("progress assertion released operator control: %+v", updated)
			}
		})
	}
}

func TestInstructorWorkerCannotEraseCriticalFindings(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "critical-findings", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) { w.OpenCriticalFindings = 1 })
	zero := 0
	for _, input := range []instructorsvc.ReportInput{
		{WorkID: work.ID, SessionID: worker, Kind: "progress"},
		{WorkID: work.ID, SessionID: worker, Kind: "progress", OpenCriticalFindings: &zero},
	} {
		updated, err := f.service.Report(context.Background(), input)
		if err != nil || updated.OpenCriticalFindings != 1 {
			t.Fatalf("worker reset independent review findings: %+v err=%v", updated, err)
		}
	}
}

func TestInstructorDoneRequiresCloseoutPhaseCurrentEvidenceAndApprovedDelta(t *testing.T) {
	for _, fault := range []string{"unfinished-phase", "critical-finding", "old-commit", "negative-delta", "open-gate", "no-publication-grant", "complete"} {
		t.Run(fault, func(t *testing.T) {
			f := newInstructorFixture(t)
			work := f.register(t, "closeout-"+fault, instructorsvc.DefaultPolicy())
			worker := f.attachWorker(t, work.ID)
			f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
				w.SHA = immutableSHA
				w.Branch = "feature/closeout"
				w.Phase = "closeout"
				w.PhaseSequence, w.FixPhaseSequence, w.DeltaPhaseSequence = 4, 2, 3
				if fault != "no-publication-grant" {
					w.PublicationGrants = []domain.InstructorPublicationGrant{{GateID: "approved-publication", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Branch: w.Branch, SnapshotID: "trusted-publication-snapshot", WorkRevision: w.Revision, Approval: "PUBLISH-GO " + w.Branch, ApprovedAt: f.now}}
				}
				for _, required := range w.Policy.CloseoutRequires {
					evidence := domain.InstructorEvidence{ID: "trusted-" + required, WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: required, Phase: "delta", PhaseSequence: 3, Producer: "independent-reviewer", Role: "reviewer", Ref: "trusted:" + required, Verified: true}
					if required == "push" || required == "full" {
						evidence.Kind, evidence.Profile, evidence.Role, evidence.Producer = "check", required, "worker", worker
					}
					if required == "delta" {
						evidence.Outcome = "approved"
					}
					w.Evidence = append(w.Evidence, evidence)
				}
				switch fault {
				case "unfinished-phase":
					w.Phase = "implement"
				case "critical-finding":
					w.OpenCriticalFindings = 1
				case "old-commit":
					w.SHA = strings.Repeat("2", 40)
				case "negative-delta":
					for i := range w.Evidence {
						if w.Evidence[i].Kind == "delta" {
							w.Evidence[i].Outcome = "changes_requested"
						}
					}
				case "open-gate":
					w.Gate = &domain.InstructorGate{ID: "unapproved-publication", Kind: "publication", ExactApproval: "PUBLISH-GO branch", OpenedAt: f.now}
				}
			})
			updated, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "done"})
			if fault == "complete" {
				if err != nil || updated.State != "done" {
					t.Fatalf("complete closeout refused: %+v err=%v", updated, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("closeout ignored %s", fault)
			}
			details, readErr := f.service.Details(context.Background(), work.ID)
			if readErr != nil || details.Work.State == "done" {
				t.Fatalf("rejected closeout persisted done: %+v err=%v", details.Work, readErr)
			}
		})
	}
}

func TestInstructorChecksRequireSuccessfulPinnedWorkerLaunch(t *testing.T) {
	for _, fault := range []string{"absent", "requested-only", "sent-mismatch", "minimum-effort", "wrong-harness"} {
		t.Run(fault, func(t *testing.T) {
			f := newInstructorFixture(t)
			path, _ := committedWorkspace(t)
			policy := instructorsvc.DefaultPolicy()
			workerRole := policy.Roles["worker"]
			workerRole.EffortByModel = map[string]string{"provider/worker": "high"}
			policy.Roles["worker"] = workerRole
			policy.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "rev-parse", "--verify", "HEAD"}, TimeoutSeconds: 10}
			work := f.register(t, "invalid-provenance-"+fault, policy)
			worker := f.attachWorker(t, work.ID)
			f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
				switch fault {
				case "absent":
					w.Launches = nil
				case "requested-only":
					w.Launches[0].Status, w.Launches[0].Sent = "requested", nil
				case "sent-mismatch":
					selection := *w.Launches[0].Sent
					selection.Effort = "minimal"
					w.Launches[0].Sent = &selection
				case "minimum-effort":
					selection := w.Launches[0].Requested
					selection.Effort = "minimal"
					w.Launches[0].Requested, w.Launches[0].Sent = selection, &selection
				case "wrong-harness":
					selection := w.Launches[0].Requested
					selection.Harness = "codex"
					w.Launches[0].Requested, w.Launches[0].Sent = selection, &selection
				}
			})
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
			if _, err := f.service.RunChecks(context.Background(), work.ID, worker, []string{"push"}); err == nil {
				t.Fatalf("%s producer generated trusted worker check evidence", fault)
			}
			details, err := f.service.Details(context.Background(), work.ID)
			if err != nil || len(details.Work.Evidence) != 0 {
				t.Fatalf("rejected provenance added evidence: %+v err=%v", details.Work.Evidence, err)
			}
		})
	}
}

func TestInstructorConcurrentRetriesReturnOneDurableCommand(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "concurrent-command", instructorsvc.DefaultPolicy())
	snapshot := f.snapshot(t)
	for round := 0; round < 8; round++ {
		input := instructorsvc.CommandInput{Command: "Weiter", SnapshotID: snapshot.ID, WorkIDs: []string{work.ID}, IdempotencyKey: fmt.Sprintf("concurrent-retry-%d", round)}
		start := make(chan struct{})
		var wait sync.WaitGroup
		type result struct {
			command domain.InstructorCommandResult
			err     error
		}
		results := make([]result, 8)
		for index := range results {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				<-start
				results[index].command, results[index].err = f.service.Command(context.Background(), input)
			}(index)
		}
		close(start)
		wait.Wait()
		dispatchID, batchID := "", ""
		for index, result := range results {
			if result.err != nil || len(result.command.Dispatches) != 1 {
				t.Fatalf("concurrent retry %d in round %d failed: %+v err=%v", index, round, result.command, result.err)
			}
			if index == 0 {
				dispatchID, batchID = result.command.Dispatches[0].ID, result.command.BatchID
			} else if result.command.Dispatches[0].ID != dispatchID {
				t.Fatalf("concurrent retry created duplicate dispatch: %s != %s", result.command.Dispatches[0].ID, dispatchID)
			}
		}
		durable, err := f.store.ListInstructorDispatches(context.Background(), batchID, "")
		if err != nil || len(durable) != 1 {
			t.Fatalf("concurrent retry produced duplicate durable command: %+v err=%v", durable, err)
		}
	}
}

func TestInstructorNegativeDeltaAllowsASecondHumanApprovedFix(t *testing.T) {
	f := newInstructorFixture(t)
	policy := instructorsvc.DefaultPolicy()
	policy.InitialPhase = "delta"
	work := f.register(t, "second-fix", policy)
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.SHA = immutableSHA
		w.FixRounds = 1
		w.PhaseSequence, w.FixPhaseSequence, w.DeltaPhaseSequence = 3, 2, 3
		w.OpenCriticalFindings = 1
		w.Evidence = []domain.InstructorEvidence{{ID: "negative-delta-pass", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: "review-report", Phase: "delta", PhaseSequence: 3, Producer: "independent-reviewer", Role: "reviewer", Ref: "negative-delta-pass", Verified: true, Outcome: "changes_requested", CriticalFindings: 1}}
	})
	humanApproved := false
	for step := 0; step < 6; step++ {
		if err := f.service.Tick(context.Background()); err != nil {
			t.Fatalf("drive corrective workflow: %v", err)
		}
		details, err := f.service.Details(context.Background(), work.ID)
		if err != nil {
			t.Fatalf("read corrective phase: %v", err)
		}
		if details.Work.Phase == "fix" {
			if !humanApproved || details.Work.FixRounds != 2 || details.Work.OpenCriticalFindings != 1 {
				t.Fatalf("second fix lost human approval, budget or blocker: %+v", details.Work)
			}
			return
		}
		if details.Work.State == "waiting_human" {
			if details.Work.Gate == nil {
				t.Fatal("corrective wait has no concrete human gate")
			}
			snapshot := f.snapshot(t)
			if _, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: snapshot.ID, WorkIDs: []string{work.ID}, IdempotencyKey: "approve-second-fix"}); err != nil {
				t.Fatalf("approve second correction: %v", err)
			}
			humanApproved = true
		}
	}
	details, err := f.service.Details(context.Background(), work.ID)
	t.Fatalf("negative delta never reached permitted second correction: %+v err=%v", details.Work, err)
}

func TestInstructorCommandNamesRejectAmbiguousAndEmptySelections(t *testing.T) {
	f := newInstructorFixture(t)
	first := f.register(t, "name-one", instructorsvc.DefaultPolicy())
	second := f.register(t, "name-two", instructorsvc.DefaultPolicy())
	first = f.trustedFacts(t, first.ID, func(w *domain.InstructorWork) { w.Name = "Shared work one" })
	second = f.trustedFacts(t, second.ID, func(w *domain.InstructorWork) { w.Name = "Shared work two" })
	duplicateName := second
	duplicateName.Name = first.Name
	duplicateName.Revision++
	if changed, err := f.store.SaveInstructorWork(context.Background(), duplicateName, second.Revision, domain.InstructorEvent{ID: uuid.NewString(), WorkID: second.ID, Kind: "duplicate-name", At: f.now}); err == nil || changed {
		t.Fatalf("SQLite accepted a duplicate work name: changed=%v err=%v", changed, err)
	}
	snapshot := f.snapshot(t)
	for _, command := range []string{"Weiter Shared work", "Weiter []"} {
		_, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: command, SnapshotID: snapshot.ID, IdempotencyKey: "invalid-" + command})
		if err == nil {
			t.Fatalf("ambiguous or empty selector %q silently authorized a work cohort", command)
		}
	}
	queued, err := f.store.ListInstructorDispatches(context.Background(), "", "")
	if err != nil || len(queued) != 0 {
		t.Fatalf("invalid work selector changed execution queue: %+v err=%v", queued, err)
	}
	result, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Weiter " + first.ID, SnapshotID: snapshot.ID, IdempotencyKey: "explicit-work-id"})
	if err != nil || len(result.Dispatches) != 1 || result.Dispatches[0].WorkID != first.ID {
		t.Fatalf("explicit durable identity did not disambiguate work: %+v err=%v", result, err)
	}
}

func TestInstructorTransportUncertaintyCannotErasePublicationGate(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "publication-with-uncertain-query", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	_, err := f.service.Report(context.Background(), instructorsvc.ReportInput{
		WorkID: work.ID, SessionID: worker, Kind: "needs_input", Branch: "feature/needs-publication",
		Gate: &domain.InstructorGate{ID: "publication-approval", Kind: "publication", Title: "Publication requires operator approval"},
	})
	if err != nil {
		t.Fatalf("open publication gate: %v", err)
	}
	orchestrator, err := f.store.CreateSession(context.Background(), domain.SessionRecord{
		ProjectID: "governed", Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: f.now}, CreatedAt: f.now, UpdatedAt: f.now,
	})
	if err != nil {
		t.Fatalf("seed dispatch target: %v", err)
	}
	f.delivery.bind = func(ctx context.Context, dispatch domain.InstructorDispatch) error {
		return f.service.BindDispatchTarget(ctx, dispatch.ID, orchestrator.ID)
	}
	f.delivery.err = instructorsvc.ErrDeliveryUncertain
	snapshot := f.snapshot(t)
	query, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Prüfen", SnapshotID: snapshot.ID, WorkIDs: []string{work.ID}, IdempotencyKey: "uncertain-query"})
	if err != nil || len(query.Dispatches) != 1 {
		t.Fatalf("query while publication is gated: %+v err=%v", query, err)
	}
	_ = f.service.Tick(context.Background())
	uncertain := f.snapshot(t)
	_, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: uncertain.ID, WorkIDs: []string{work.ID}, IdempotencyKey: "generic-uncertainty-go"})
	if err == nil {
		t.Fatal("generic Go bypassed unknown native submission")
	}
	_, err = f.service.ReconcileDelivery(context.Background(), instructorsvc.ReconcileInput{SnapshotID: uncertain.ID, WorkID: work.ID, DispatchID: query.Dispatches[0].ID, Accepted: true, Note: "Operator observed this exact query in the native conversation"})
	if err != nil {
		t.Fatalf("reconcile actual query receipt: %v", err)
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.State != "waiting_human" || details.Work.Gate == nil || details.Work.Gate.ID != "publication-approval" || details.Work.Gate.ExactApproval != "PUBLISH-GO feature/needs-publication" {
		t.Fatalf("query receipt erased unapproved publication decision: %+v err=%v", details.Work, err)
	}
	if len(f.delivery.calls) != 1 || len(f.delivery.reconciliations) != 1 {
		t.Fatalf("reconciliation resubmitted execution: deliveries=%+v reconciliations=%+v", f.delivery.calls, f.delivery.reconciliations)
	}
}

func TestInstructorConflictingRepositoryCommandsCannotGrantWrongAuthority(t *testing.T) {
	for _, collision := range []string{"name", "alias"} {
		t.Run(collision, func(t *testing.T) {
			f := newInstructorFixture(t)
			sequence := 0
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Clock: func() time.Time { return f.now }, NewID: func() string {
				sequence++
				return fmt.Sprintf("command-collision-%04d", sequence)
			}})
			firstPolicy, secondPolicy := instructorsvc.DefaultPolicy(), instructorsvc.DefaultPolicy()
			firstName, secondName := "Release", "Release"
			aliases := []string{}
			if collision == "alias" {
				firstName, secondName, aliases = "Allow Release", "Inspect Release", []string{"release"}
			}
			firstPolicy.Commands = []domain.InstructorCommandSpec{{Name: firstName, Aliases: aliases, Action: "go", Mutates: true, SnapshotRequired: true}}
			secondPolicy.Commands = []domain.InstructorCommandSpec{{Name: secondName, Aliases: aliases, Action: "pruefen", Mutates: true, SnapshotRequired: true}}
			f.register(t, "first-command-policy", firstPolicy)
			second := f.register(t, "second-command-policy", secondPolicy)
			worker := f.attachWorker(t, second.ID)
			if _, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: second.ID, SessionID: worker, Kind: "needs_input", Gate: &domain.InstructorGate{ID: "must-not-grant", Kind: "human", Title: "Operator decision"}}); err != nil {
				t.Fatalf("open target human gate: %v", err)
			}
			snapshot, err := f.service.Snapshot(context.Background(), "")
			result := domain.InstructorCommandResult{}
			if err == nil {
				result, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Release " + second.ID, SnapshotID: snapshot.ID, IdempotencyKey: "conflicting-command-" + collision})
			}
			if err == nil && (len(result.Dispatches) != 1 || result.Dispatches[0].WorkID != second.ID || result.Dispatches[0].Action != "pruefen") {
				t.Fatalf("conflicting %s selected another repository's gate-granting action", collision)
			}
			details, readErr := f.service.Details(context.Background(), second.ID)
			if readErr != nil || details.Work.Gate == nil || details.Work.Gate.ID != "must-not-grant" || details.Work.State != "waiting_human" {
				t.Fatalf("command collision changed target authority: %+v err=%v", details.Work, readErr)
			}
			queued, readErr := f.store.ListInstructorDispatches(context.Background(), "", "")
			if readErr != nil || len(queued) > 1 || len(queued) == 1 && (queued[0].WorkID != second.ID || queued[0].Action != "pruefen") {
				t.Fatalf("conflicting command queued execution: %+v err=%v", queued, readErr)
			}
		})
	}
}

package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// Only the provider boundary is simulated. The real review engine, SQLite
// uniqueness constraint and daemon launch/evidence callbacks own the test facts.
type instructorScopeLauncher struct {
	governance reviewcore.ReviewerGovernance
	spawns     []reviewcore.LaunchSpec
	restores   []reviewcore.LaunchSpec
	notifies   int
	probes     int
	alive      bool
}

func (l *instructorScopeLauncher) SetInstructorGovernance(g reviewcore.ReviewerGovernance) {
	l.governance = g
}
func (*instructorScopeLauncher) InterfaceMode(domain.ReviewerHarness) domain.ReviewerInterfaceMode {
	return domain.ReviewerInterfaceTUI
}
func (*instructorScopeLauncher) Preflight(context.Context, domain.ReviewerHarness, string) error {
	return nil
}
func (l *instructorScopeLauncher) launch(ctx context.Context, spec reviewcore.LaunchSpec) (reviewcore.LaunchResult, error) {
	if l.governance != nil {
		if err := l.governance.ValidateReviewerLaunch(ctx, spec); err != nil {
			return reviewcore.LaunchResult{}, err
		}
		if err := l.governance.RecordReviewerLaunch(ctx, spec); err != nil {
			return reviewcore.LaunchResult{}, err
		}
	}
	return reviewcore.LaunchResult{HandleID: "scope-reviewer", AgentSessionID: "native-" + spec.LaunchID, LaunchID: spec.LaunchID, NativeResumed: true}, nil
}
func (l *instructorScopeLauncher) Spawn(ctx context.Context, spec reviewcore.LaunchSpec) (reviewcore.LaunchResult, error) {
	result, err := l.launch(ctx, spec)
	if err == nil {
		l.spawns = append(l.spawns, spec)
		l.alive = true
	}
	return result, err
}
func (l *instructorScopeLauncher) RestoreTerminal(ctx context.Context, spec reviewcore.LaunchSpec) (reviewcore.LaunchResult, error) {
	result, err := l.launch(ctx, spec)
	if err == nil {
		l.restores = append(l.restores, spec)
		l.alive = true
	}
	return result, err
}
func (l *instructorScopeLauncher) Notify(ctx context.Context, _ string, spec reviewcore.LaunchSpec) error {
	if reuse, ok := l.governance.(interface {
		ValidateReviewerReuse(context.Context, reviewcore.LaunchSpec) error
	}); ok {
		if err := reuse.ValidateReviewerReuse(ctx, spec); err != nil {
			return err
		}
	}
	_, err := l.launch(ctx, spec)
	if err == nil {
		l.notifies++
	}
	return err
}
func (l *instructorScopeLauncher) Alive(context.Context, string) (bool, error) {
	l.probes++
	return l.alive, nil
}
func (*instructorScopeLauncher) Reusable(domain.ReviewerHarness) bool { return true }
func (*instructorScopeLauncher) Cancel(context.Context, string, domain.ReviewerHarness) error {
	return nil
}
func (l *instructorScopeLauncher) Destroy(context.Context, string) error {
	l.alive = false
	return nil
}

type instructorScopeValidator struct{}

func (instructorScopeValidator) ValidateInstructorAgentConfig(_ context.Context, _ domain.ProjectID, role domain.InstructorRole, harness, model, effort string) error {
	if harness != role.Harness || effort != "high" || role.EffortByModel[model] != effort {
		return errors.New("native test catalogue does not support the pinned selection")
	}
	return nil
}

type instructorScopeFixture struct {
	store    *sqlite.Store
	service  *instructorsvc.Service
	guard    instructorGovernance
	engine   *reviewcore.Engine
	launcher *instructorScopeLauncher
	worker   domain.SessionRecord
	work     domain.InstructorWork
	legacy   domain.ReviewRun
	now      time.Time
}

func newInstructorScopeFixture(t *testing.T) *instructorScopeFixture {
	t.Helper()
	ctx := context.Background()
	f := &instructorScopeFixture{store: sqlitetest.MustOpen(t), launcher: &instructorScopeLauncher{}, now: time.Unix(1700000000, 0).UTC()}
	config := domain.AgentConfig{Model: "provider/reviewer", Effort: "high"}
	project := domain.ProjectRecord{ID: "scope-project", Path: t.TempDir(), RegisteredAt: f.now, Config: domain.ProjectConfig{Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex, AgentConfig: config}}}}
	if err := f.store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	var err error
	f.worker, err = f.store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: domain.ProjectID(project.ID), Kind: domain.KindWorker, Harness: domain.HarnessOpenCode,
		Metadata:          domain.SessionMetadata{WorkspacePath: project.Path, Model: "provider/worker", Effort: "high"},
		Activity:          domain.Activity{State: domain.ActivityIdle, LastActivityAt: f.now.Add(-time.Minute)},
		AutoReviewEnabled: true, CreatedAt: f.now, UpdatedAt: f.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	if err = f.store.WriteSCMObservation(ctx, domain.PullRequest{SessionID: f.worker.ID, URL: "https://github.com/fork/repo/pull/1", Number: 1, HeadSHA: sha, UpdatedAt: f.now}, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	f.engine = reviewcore.New(reviewcore.Deps{Store: f.store, Sessions: f.store, PRs: f.store, Projects: f.store, Launcher: f.launcher, Clock: func() time.Time { return f.now }})
	legacy, err := f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
	if err != nil || !legacy.Created || legacy.Run.ScopeKey != "" {
		t.Fatalf("unmanaged review must use the legacy scope: %+v err=%v", legacy, err)
	}
	f.complete(t, legacy.Run, "historical approval", "historical-github-review")
	f.legacy, _, err = f.store.GetReviewRun(ctx, legacy.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	policy := instructorsvc.DefaultPolicy()
	policy.InitialPhase = "review"
	for role, model := range map[string]string{"worker": "provider/worker", "reviewer": "provider/reviewer"} {
		selection := policy.Roles[role]
		selection.Model, selection.EffortByModel = model, map[string]string{model: "high"}
		policy.Roles[role] = selection
	}
	validator := instructorScopeValidator{}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, RoleValidator: validator, Clock: func() time.Time { return f.now }})
	f.work, err = f.service.Register(ctx, instructorsvc.RegisterInput{ProjectID: f.worker.ProjectID, Name: "Adopted review", Goal: "Review the adopted worker without certifying historical execution", Source: "manual", SourceRef: "adopted-review", Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.AttachSession(ctx, f.work.ID, f.worker.ID); err != nil {
		t.Fatal(err)
	}
	f.work, err = f.service.Report(ctx, instructorsvc.ReportInput{WorkID: f.work.ID, SessionID: f.worker.ID, Kind: "checkpoint", SHA: sha})
	if err != nil {
		t.Fatal(err)
	}
	f.guard = instructorGovernance{svc: f.service, store: f.store, validator: validator}
	f.engine.SetInstructorGovernance(f.guard)
	return f
}

func (f *instructorScopeFixture) complete(t *testing.T, run domain.ReviewRun, body, githubID string) {
	t.Helper()
	updated, err := f.store.UpdateReviewRunResult(context.Background(), run.ID, domain.ReviewRunComplete, domain.VerdictApproved, body, githubID, false)
	if err != nil || !updated {
		t.Fatalf("complete native review: updated=%t err=%v", updated, err)
	}
}

func (f *instructorScopeFixture) phase(t *testing.T, phase string, sequence, deltaSequence int64) {
	t.Helper()
	w, found, err := f.store.GetInstructorWork(context.Background(), f.work.ID)
	if err != nil || !found {
		t.Fatalf("read work: found=%t err=%v", found, err)
	}
	previous := w.Revision
	w.Revision++
	w.Phase, w.PhaseSequence, w.DeltaPhaseSequence = phase, sequence, deltaSequence
	f.now = f.now.Add(time.Second)
	w.UpdatedAt = f.now
	updated, err := f.store.SaveInstructorWork(context.Background(), w, previous, domain.InstructorEvent{ID: fmt.Sprintf("scope-phase-%d", sequence), WorkID: w.ID, Kind: "test_phase", At: f.now})
	if err != nil || !updated {
		t.Fatalf("record next durable phase: updated=%t err=%v", updated, err)
	}
	f.work = w
}

func TestInstructorAdoptedApprovedSameSHAStartsFreshManagedInitialReview(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	verifier := instructorEvidenceVerifier{store: f.store}
	legacyEvidence := domain.InstructorEvidence{WorkID: f.work.ID, PolicyVersion: f.work.PolicyVersion, SHA: f.work.SHA, Kind: "review-report", Ref: f.legacy.ID, Producer: domain.SessionID(f.legacy.ReviewID), Role: "reviewer", Harness: "codex", Model: "provider/reviewer", Effort: "high"}
	if err := verifier.Verify(ctx, f.work, legacyEvidence); err == nil || !strings.Contains(err.Error(), "no matching successful managed launch provenance") {
		t.Fatalf("correctly bound historical review must fail managed launch provenance: %v", err)
	}
	first, err := f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
	expected := fmt.Sprintf("%s/review/%d", f.work.ID, f.work.PhaseSequence)
	if err != nil || !first.Created || first.Run.ID == f.legacy.ID || first.Run.ScopeKey != expected || first.Run.TargetSHA != f.legacy.TargetSHA {
		t.Fatalf("adopted same-SHA approval suppressed fresh managed review: %+v err=%v", first, err)
	}
	if len(f.launcher.spawns) != 2 || f.launcher.notifies != 0 || f.launcher.spawns[1].AgentSessionID != "" || f.launcher.spawns[1].ScopeKey != expected {
		t.Fatalf("initial managed pass reused historical native reviewer: %+v", f.launcher)
	}
	w, _, err := f.store.GetInstructorWork(ctx, f.work.ID)
	if err != nil || len(w.Launches) != 1 || w.Launches[0].ReviewRunID != first.Run.ID || w.Launches[0].Phase != "review" || w.Launches[0].PhaseSequence != w.PhaseSequence || w.Launches[0].Confirmed != nil {
		t.Fatalf("actual launch provenance missing or fabricated: launches=%+v err=%v", w.Launches, err)
	}
	if err = verifier.Verify(ctx, w, legacyEvidence); err == nil || !strings.Contains(err.Error(), "no matching successful managed launch provenance") {
		t.Fatalf("new managed launch relabeled the historical pass: %v", err)
	}
	f.complete(t, first.Run, "managed approval", "managed-github-review")
	claims, err := verifier.Discover(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	verified := false
	for _, claim := range claims {
		if claim.Kind == "review-report" && claim.Ref == first.Run.ID {
			verified = verifier.Verify(ctx, w, claim) == nil
		}
		if claim.Ref == f.legacy.ID {
			t.Fatal("discovery relabeled the historical ungoverned pass")
		}
	}
	if !verified {
		t.Fatal("fresh native managed review did not produce verifiable work evidence")
	}
	for _, automatic := range []bool{false, true} {
		var repeated reviewcore.TriggerResult
		if automatic {
			repeated, err = f.engine.TriggerWithSource(ctx, f.worker.ID, "", domain.AgentConfig{}, domain.ReviewTriggerAuto)
		} else {
			repeated, err = f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
		}
		if err != nil || repeated.Created || repeated.Run.ID != first.Run.ID || len(f.launcher.spawns) != 2 || f.launcher.notifies != 0 {
			t.Fatalf("same managed phase lost idempotency: automatic=%t result=%+v err=%v", automatic, repeated, err)
		}
	}
	old, found, err := f.store.GetReviewRun(ctx, f.legacy.ID)
	if err != nil || !found || old != f.legacy {
		t.Fatalf("historical approval was rewritten: old=%+v expected=%+v err=%v", old, f.legacy, err)
	}
}

func TestInstructorUnassignedReviewKeepsLegacyScope(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	worker, err := f.store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: f.worker.ProjectID, Kind: domain.KindWorker, Harness: domain.HarnessOpenCode,
		Metadata: f.worker.Metadata, CreatedAt: f.now, UpdatedAt: f.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.WriteSCMObservation(ctx, domain.PullRequest{SessionID: worker.ID, URL: "https://github.com/fork/repo/pull/2", Number: 2, HeadSHA: f.legacy.TargetSHA, UpdatedAt: f.now}, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	first, err := f.engine.Trigger(ctx, worker.ID, "", domain.AgentConfig{})
	if err != nil || !first.Created || first.Run.ScopeKey != "" {
		t.Fatalf("enabled Instructor changed unassigned review behavior: %+v err=%v", first, err)
	}
	f.complete(t, first.Run, "unassigned approval", "")
	repeated, err := f.engine.Trigger(ctx, worker.ID, "", domain.AgentConfig{})
	if err != nil || repeated.Created || repeated.Run.ID != first.Run.ID || repeated.Run.ScopeKey != "" {
		t.Fatalf("legacy review idempotency changed: %+v err=%v", repeated, err)
	}
	w, _, err := f.store.GetInstructorWork(ctx, f.work.ID)
	if err != nil || len(w.Launches) != 0 {
		t.Fatalf("unassigned review fabricated a managed launch: launches=%+v err=%v", w.Launches, err)
	}
}

func TestInstructorInitialScopeChangesWithDurablePhaseAndKeepsDeltaContract(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	first, err := f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
	if err != nil || !first.Created {
		t.Fatalf("first managed phase: %+v err=%v", first, err)
	}
	f.complete(t, first.Run, "first managed phase", "")
	f.phase(t, "review", 2, 0)
	second, err := f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
	if err != nil || !second.Created || second.Run.ID == first.Run.ID || second.Run.ScopeKey != f.work.ID+"/review/2" {
		t.Fatalf("a distinct same-SHA review phase reused its predecessor: %+v err=%v", second, err)
	}
	f.complete(t, second.Run, "next managed phase", "")
	f.phase(t, "delta", 3, 3)
	delta, err := f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
	if err != nil || !delta.Created || delta.Run.ScopeKey != f.work.ID+"/delta/3" || delta.Run.TargetSHA != first.Run.TargetSHA || delta.Run.ID == second.Run.ID {
		t.Fatalf("delta contract changed or reused initial approval: %+v err=%v", delta, err)
	}
	f.complete(t, delta.Run, "delta approval", "")
	repeated, err := f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
	if err != nil || repeated.Created || repeated.Run.ID != delta.Run.ID || len(f.launcher.spawns) != 4 || f.launcher.notifies != 0 {
		t.Fatalf("delta retry lost phase idempotency: %+v err=%v launcher=%+v", repeated, err, f.launcher)
	}
	w, _, err := f.store.GetInstructorWork(ctx, f.work.ID)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := (instructorEvidenceVerifier{store: f.store}).Discover(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	verified := false
	for _, claim := range claims {
		if claim.Kind == "delta" && claim.Ref == delta.Run.ID {
			verified = (instructorEvidenceVerifier{store: f.store}).Verify(ctx, w, claim) == nil
		}
	}
	if !verified {
		t.Fatal("unchanged exact delta scope failed native evidence verification")
	}
}

func TestInstructorInitialScopeRejectsLegacyRestoreAndPreservesCurrentRecovery(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	probes := f.launcher.probes
	if _, err := f.engine.RestoreReviewer(ctx, f.worker.ID); err == nil || !strings.Contains(err.Error(), "earlier governance phase") || f.launcher.probes != probes || len(f.launcher.restores) != 0 {
		t.Fatalf("adoption restored an ungoverned native reviewer: err=%v launcher=%+v", err, f.launcher)
	}
	first, err := f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
	if err != nil || !first.Created {
		t.Fatalf("fresh initial review after refused legacy restore: %+v err=%v", first, err)
	}
	f.complete(t, first.Run, "managed approval", "")
	f.launcher.alive = false
	restored, err := f.engine.RestoreReviewer(ctx, f.worker.ID)
	if err != nil || !restored.Restored || len(f.launcher.restores) != 1 || f.launcher.restores[0].ScopeKey != first.Run.ScopeKey {
		t.Fatalf("current managed native recovery failed: %+v err=%v launcher=%+v", restored, err, f.launcher)
	}
	oldSpec := f.launcher.spawns[1]
	f.phase(t, "delta", 2, 2)
	before, _, err := f.store.GetInstructorWork(ctx, f.work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.guard.ValidateReviewerLaunch(ctx, oldSpec); !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("native launch accepted stale initial scope: %v", err)
	}
	if err = f.guard.ValidateReviewerReuse(ctx, oldSpec); !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("native reuse accepted stale initial scope: %v", err)
	}
	if err = f.guard.RecordReviewerLaunch(ctx, oldSpec); !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("native confirmation relabeled stale initial scope: %v", err)
	}
	probes = f.launcher.probes
	if _, err = f.engine.RestoreReviewer(ctx, f.worker.ID); err == nil || !strings.Contains(err.Error(), "earlier governance phase") || f.launcher.probes != probes || len(f.launcher.restores) != 1 {
		t.Fatalf("delta recovery relabeled the initial native reviewer: err=%v launcher=%+v", err, f.launcher)
	}
	after, _, err := f.store.GetInstructorWork(ctx, f.work.ID)
	if err != nil || len(after.Launches) != len(before.Launches) {
		t.Fatalf("rejected stale scope fabricated provenance: launches=%+v err=%v", after.Launches, err)
	}
}

func TestInstructorMembersObserveExactNativeReviewerLaunchGeneration(t *testing.T) {
	f := newInstructorScopeFixture(t)
	ctx := context.Background()
	triggered, err := f.engine.Trigger(ctx, f.worker.ID, "", domain.AgentConfig{})
	if err != nil {
		t.Fatal(err)
	}
	members, err := f.service.Members(ctx, f.work.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, member := range members.Members {
		if member.ReviewRunID != triggered.Run.ID {
			continue
		}
		found = true
		if member.Association != "current" || member.RuntimeGeneration == "" || member.RuntimeGeneration != f.launcher.spawns[len(f.launcher.spawns)-1].LaunchID || member.OpenTarget == nil || member.OpenTarget.Kind != "review" {
			t.Fatalf("native launch generation not associated with active review: %+v", member)
		}
	}
	if !found {
		t.Fatalf("active native review absent: %+v", members)
	}
}

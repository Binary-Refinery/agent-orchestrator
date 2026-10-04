package trackerintake

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func v2WireCfg() domain.TrackerIntakeConfig {
	return domain.TrackerIntakeConfig{
		Enabled:  true,
		Assignee: "alice",
		AutomationV2: &domain.AutomationV2Config{
			Enabled:              true,
			Label:                "ao-ready",
			MaxParallel:          1,
			ReviewTimeoutSeconds: 3600,
		},
	}
}

func TestV2TakeCallerGateOff(t *testing.T) {
	off := domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}
	head := V2Take{IssueID: "github:a#1"}
	if d := V2TakeCaller(context.Background(), []V2Take{head}, head, nil, off, V2Audit{}); d.Allow || d.Reason != V2ReasonDisabled {
		t.Fatalf("gate off = %+v, want disabled deny", d)
	}
}

func TestV2TakeCallerWIPFull(t *testing.T) {
	cfg := v2WireCfg()
	head := V2Take{IssueID: "github:a#1"}
	if d := V2TakeCaller(context.Background(), []V2Take{head}, head, []V2ActiveTake{{}}, cfg, V2Audit{}); d.Allow || d.Reason != V2ReasonWIPFull {
		t.Fatalf("wip full = %+v, want wip_full", d)
	}
}

func TestV2TakeCallerStallTimeout(t *testing.T) {
	cfg := v2WireCfg()
	head := V2Take{IssueID: "github:a#1"}
	stalled := []V2ActiveTake{{Stalled: true, StalledFor: 2 * time.Hour}}
	if d := V2TakeCaller(context.Background(), []V2Take{head}, head, stalled, cfg, V2Audit{}); d.Allow || d.Reason != V2ReasonReviewStalled {
		t.Fatalf("stall = %+v, want review_stalled", d)
	}
}

func TestV2PreflightCallerOverlapSuspect(t *testing.T) {
	cfg := v2WireCfg()
	probe := V2PreflightProbe{
		BaseResolvable: true, HarnessReady: true, RepoClean: true, BudgetRemaining: 2,
		CandidateFiles:    []string{"backend/foo.go"},
		ActiveBranchFiles: map[string][]string{"worker-1": {"backend/foo.go"}},
	}
	if d := V2PreflightCaller(context.Background(), probe, cfg, V2Audit{}); d.Allow || d.Reason != V2ReasonFileOverlap {
		t.Fatalf("overlap = %+v, want file_overlap suspect deferral", d)
	}
	probe.ActiveBranchFiles = map[string][]string{"worker-1": {"backend/other.go"}}
	if d := V2PreflightCaller(context.Background(), probe, cfg, V2Audit{}); !d.Allow {
		t.Fatalf("no overlap should allow: %+v", d)
	}
}

func TestV2PollFeedEmptyLabel(t *testing.T) {
	cfg := v2WireCfg()
	cfg.AutomationV2.Label = ""
	q := &V2Queue{}
	issues := []domain.Issue{{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "a#1"}, State: domain.IssueOpen, Labels: []string{"ao-ready"}}}
	res := q.V2PollFeed(context.Background(), issues, cfg, V2Audit{})
	if res.Admitted != 0 || len(q.Takes()) != 0 {
		t.Fatalf("empty label must admit nothing: %+v queue=%v", res, q.Takes())
	}
}

func TestV2PollFeedLabelRemovalDisqualifies(t *testing.T) {
	cfg := v2WireCfg()
	q := &V2Queue{}
	mk := func(labels []string) domain.Issue {
		return domain.Issue{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "a#1"}, State: domain.IssueOpen, Labels: labels}
	}
	if res := q.V2PollFeed(context.Background(), []domain.Issue{mk([]string{"ao-ready"})}, cfg, V2Audit{}); res.Admitted != 1 {
		t.Fatalf("admit = %+v", res)
	}
	res := q.V2PollFeed(context.Background(), []domain.Issue{mk([]string{"other"})}, cfg, V2Audit{})
	if res.Disqualified != 1 || len(q.Takes()) != 0 {
		t.Fatalf("label removal must disqualify: %+v queue=%v", res, q.Takes())
	}
}

func TestV2PollFeedFIFOOrder(t *testing.T) {
	cfg := v2WireCfg()
	q := &V2Queue{}
	issues := []domain.Issue{
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "a#1"}, State: domain.IssueOpen, Labels: []string{"ao-ready"}},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "a#2"}, State: domain.IssueOpen, Labels: []string{"ao-ready"}},
	}
	q.V2PollFeed(context.Background(), issues, cfg, V2Audit{})
	takes := q.Takes()
	if len(takes) != 2 || takes[0].IssueID != "github:a#1" || takes[1].IssueID != "github:a#2" {
		t.Fatalf("fifo order broken: %+v", takes)
	}
	// Non-head take must be denied by gate.
	if d := V2TakeCaller(context.Background(), takes, takes[1], nil, cfg, V2Audit{}); d.Allow || d.Reason != V2ReasonNotFirst {
		t.Fatalf("non-head = %+v, want not_first", d)
	}
}

func v2ObserverProject() domain.ProjectRecord {
	return domain.ProjectRecord{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
			Enabled:  true,
			Assignee: "alice",
			AutomationV2: &domain.AutomationV2Config{
				Enabled:              true,
				Label:                "ao-ready",
				MaxParallel:          1,
				ReviewTimeoutSeconds: 3600,
			},
		}},
	}
}

func v2LabeledIssue(native string) domain.Issue {
	return domain.Issue{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: native},
		State:     domain.IssueOpen,
		Labels:    []string{"ao-ready"},
		Assignees: []string{"alice"},
	}
}

// v2FactStore wraps fakeStore with the V2 fact surface. Nil facts means
// facts are not established (fail-closed deny expected).
type v2FactStore struct {
	*fakeStore
	facts    *V2PreflightFacts
	factsErr error
}

func (s *v2FactStore) V2PreflightFacts(context.Context, string) (V2PreflightFacts, bool, error) {
	if s.factsErr != nil {
		return V2PreflightFacts{}, false, s.factsErr
	}
	if s.facts == nil {
		return V2PreflightFacts{}, false, nil
	}
	return *s.facts, true, nil
}

func passingFacts() *V2PreflightFacts {
	return &V2PreflightFacts{HarnessReady: true, RepoClean: true, BudgetRemaining: 1}
}

// Regression P1: full WIP must block the spawn.
func TestV2ObserverFullWIPBlocksSpawn(t *testing.T) {
	store := &v2FactStore{fakeStore: &fakeStore{
		projects: []domain.ProjectRecord{v2ObserverProject()},
		sessions: []domain.SessionRecord{{
			ID: "demo-active", ProjectID: "demo",
			IssueID: "github:acme/demo#9", Kind: domain.KindWorker,
		}},
	}, facts: passingFacts()}
	tracker := &fakeTracker{issues: []domain.Issue{v2LabeledIssue("acme/demo#1")}}
	spawner := &fakeSpawner{}
	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0 (WIP full at maxParallel=1)", len(spawner.calls))
	}
}

// Regression P1: empty WIP admits the head take.
func TestV2ObserverHeadTakeSpawns(t *testing.T) {
	store := &v2FactStore{fakeStore: &fakeStore{projects: []domain.ProjectRecord{v2ObserverProject()}}, facts: passingFacts()}
	tracker := &fakeTracker{issues: []domain.Issue{v2LabeledIssue("acme/demo#1")}}
	spawner := &fakeSpawner{}
	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#1" {
		t.Fatalf("spawn calls = %+v, want exactly the head take", spawner.calls)
	}
}

// Regression P1-A: without established preflight facts the take path must
// deny rather than pass on hardcoded assumptions.
func TestV2ObserverMissingFactsDenySpawn(t *testing.T) {
	for name, store := range map[string]Store{
		"no fact surface": &fakeStore{projects: []domain.ProjectRecord{v2ObserverProject()}},
		"facts unestablished": &v2FactStore{
			fakeStore: &fakeStore{projects: []domain.ProjectRecord{v2ObserverProject()}},
		},
	} {
		tracker := &fakeTracker{issues: []domain.Issue{v2LabeledIssue("acme/demo#1")}}
		spawner := &fakeSpawner{}
		if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
			t.Fatalf("%s: Poll() error = %v", name, err)
		}
		if len(spawner.calls) != 0 {
			t.Fatalf("%s: spawn calls = %+v, want 0 (fail-closed without facts)", name, spawner.calls)
		}
	}
}

// Regression P1-B: a stalled take blocks new spawns via the stall timeout,
// not via WIP-full. MaxParallel=2 with one active session leaves WIP room,
// so only a stall-specific deny can block the spawn.
func TestV2ObserverStalledTakeBlocksSpawn(t *testing.T) {
	clock := time.Now().UTC()
	project := v2ObserverProject()
	project.Config.TrackerIntake.AutomationV2.MaxParallel = 2
	stalled := domain.SessionRecord{
		ID: "demo-stalled", ProjectID: "demo",
		IssueID: "github:acme/demo#9", Kind: domain.KindWorker,
		Activity: domain.Activity{State: domain.ActivityBlocked, LastActivityAt: clock.Add(-2 * time.Hour)},
	}
	store := &v2FactStore{fakeStore: &fakeStore{
		projects: []domain.ProjectRecord{project},
		sessions: []domain.SessionRecord{stalled},
	}, facts: passingFacts()}
	tracker := &fakeTracker{issues: []domain.Issue{v2LabeledIssue("acme/demo#1")}}
	spawner := &fakeSpawner{}
	var logs bytes.Buffer
	o := New(singleResolver(tracker), store, spawner, Config{
		Clock:  func() time.Time { return clock },
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err := o.Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %+v, want 0 (WIP has room at 1/2, only stall blocks)", spawner.calls)
	}
	// Pin the stall-specific deny on the same durable facts.
	active := v2ActiveTakes("demo", []domain.SessionRecord{stalled}, clock)
	if len(active) != 1 || !active[0].Stalled {
		t.Fatalf("active takes = %+v, want one stalled take", active)
	}
	head := V2Take{IssueID: "github:acme/demo#1"}
	if d := V2WIPGate([]V2Take{head}, head, active, project.Config.TrackerIntake); d.Allow || d.Reason != V2ReasonReviewStalled {
		t.Fatalf("gate = %+v, want review_stalled deny", d)
	}
	if !strings.Contains(logs.String(), string(V2ReasonReviewStalled)) {
		t.Fatalf("audit log missing %q:\n%s", V2ReasonReviewStalled, logs.String())
	}
}

// Unstalled control: a recently-blocked take (inside the stall timeout) with
// WIP room allows the head spawn.
func TestV2ObserverFreshStallAllowsSpawn(t *testing.T) {
	clock := time.Now().UTC()
	project := v2ObserverProject()
	project.Config.TrackerIntake.AutomationV2.MaxParallel = 2
	fresh := domain.SessionRecord{
		ID: "demo-fresh", ProjectID: "demo",
		IssueID: "github:acme/demo#9", Kind: domain.KindWorker,
		Activity: domain.Activity{State: domain.ActivityBlocked, LastActivityAt: clock.Add(-time.Minute)},
	}
	store := &v2FactStore{fakeStore: &fakeStore{
		projects: []domain.ProjectRecord{project},
		sessions: []domain.SessionRecord{fresh},
	}, facts: passingFacts()}
	tracker := &fakeTracker{issues: []domain.Issue{v2LabeledIssue("acme/demo#1")}}
	spawner := &fakeSpawner{}
	o := New(singleResolver(tracker), store, spawner, Config{
		Clock:  func() time.Time { return clock },
		Logger: discardLogger(),
	})
	if err := o.Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#1" {
		t.Fatalf("spawn calls = %+v, want exactly the head take (stall not timed out, WIP has room)", spawner.calls)
	}
}

// Regression P2: successive polls with reordered tracker results preserve
// FIFO, and label removal disqualifies before any take.
func TestV2ObserverFIFOAcrossReorderedPolls(t *testing.T) {
	store := &v2FactStore{fakeStore: &fakeStore{
		projects: []domain.ProjectRecord{v2ObserverProject()},
		sessions: []domain.SessionRecord{{
			ID: "demo-active", ProjectID: "demo",
			IssueID: "github:acme/demo#9", Kind: domain.KindWorker,
		}},
	}, facts: passingFacts()}
	tracker := &fakeTracker{issues: []domain.Issue{v2LabeledIssue("acme/demo#1"), v2LabeledIssue("acme/demo#2")}}
	spawner := &fakeSpawner{}
	o := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()})
	ctx := context.Background()
	if err := o.Poll(ctx); err != nil {
		t.Fatalf("Poll 1: %v", err)
	}
	// Reordered results must not reorder the queue; WIP still full.
	tracker.issues = []domain.Issue{v2LabeledIssue("acme/demo#2"), v2LabeledIssue("acme/demo#1")}
	if err := o.Poll(ctx); err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0 while WIP full", len(spawner.calls))
	}
	// Head loses its label: disqualified; with WIP still full nothing spawns.
	unlabeled := v2LabeledIssue("acme/demo#1")
	unlabeled.Labels = []string{"other"}
	tracker.issues = []domain.Issue{unlabeled, v2LabeledIssue("acme/demo#2")}
	if err := o.Poll(ctx); err != nil {
		t.Fatalf("Poll 3: %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %+v, want 0 (head disqualified, WIP full)", spawner.calls)
	}
	// WIP drains: the surviving queued take (#2) is now head and spawns.
	store.sessions = nil
	if err := o.Poll(ctx); err != nil {
		t.Fatalf("Poll 4: %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#2" {
		t.Fatalf("spawn calls = %+v, want exactly github:acme/demo#2", spawner.calls)
	}
}

// Regression P2: clearing the configured label denies the feed, drops the
// stale queue, and prevents any later spawn from it — even across successive
// polls with reordered results.
func TestV2ObserverClearedLabelDropsQueue(t *testing.T) {
	inner := &fakeStore{
		projects: []domain.ProjectRecord{v2ObserverProject()},
		sessions: []domain.SessionRecord{{
			ID: "demo-active", ProjectID: "demo",
			IssueID: "github:acme/demo#9", Kind: domain.KindWorker,
		}},
	}
	store := &v2FactStore{fakeStore: inner, facts: passingFacts()}
	tracker := &fakeTracker{issues: []domain.Issue{v2LabeledIssue("acme/demo#1"), v2LabeledIssue("acme/demo#2")}}
	spawner := &fakeSpawner{}
	o := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()})
	ctx := context.Background()
	if err := o.Poll(ctx); err != nil {
		t.Fatalf("Poll 1: %v", err)
	}
	// Configured label cleared: feed denies, stale queue must drop.
	inner.projects[0].Config.TrackerIntake.AutomationV2.Label = ""
	tracker.issues = []domain.Issue{v2LabeledIssue("acme/demo#2"), v2LabeledIssue("acme/demo#1")}
	if err := o.Poll(ctx); err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %+v, want 0 after label cleared", spawner.calls)
	}
	if takes := o.v2queues["demo"].Takes(); len(takes) != 0 {
		t.Fatalf("queue = %+v, want empty after denied feed", takes)
	}
	// WIP drains but the label is still cleared: still nothing spawns.
	inner.sessions = nil
	if err := o.Poll(ctx); err != nil {
		t.Fatalf("Poll 3: %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %+v, want 0 while label cleared", spawner.calls)
	}
}

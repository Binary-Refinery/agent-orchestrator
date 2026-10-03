package automation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// The spawn gate is automation-scheduler scoped: with the gate on, a legacy
// definition (no naming convention, no issue link) must not spawn and must
// not be poisoned as failed — the claim is released with an audit message.
func TestPolicyGateReleasesInsteadOfFailing(t *testing.T) {
	now := time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC)
	store := newSchedulerStore()
	store.automations["automation-legacy"] = domain.Automation{ID: "automation-legacy", ProjectID: "p", DisplayName: "Legacy name", Prompt: "Do work", Kind: domain.KindWorker, RRuleText: "DTSTART:20260825T140000Z\nRRULE:FREQ=HOURLY", Timezone: "UTC", Enabled: true, NextRunAt: now}
	spawner := &recordingSpawner{store: store}
	policy := domain.DefaultAutomationPolicy()
	policy.SpawnGate = true
	svc := New(Deps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }, Policy: policy})
	if err := svc.Tick(context.Background(), now); err == nil || !strings.Contains(err.Error(), "display name must match") {
		t.Fatalf("expected policy rejection, got %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatal("rejected dispatch must not spawn")
	}
	run := store.runs["run-legacy"]
	if len(store.runs) == 0 {
		t.Fatal("expected materialized runs")
	}
	audited := false
	for _, run := range store.runs {
		if run.Status == domain.AutomationRunFailed {
			t.Fatalf("rejected run must not fail, got %s failed", run.ID)
		}
		if strings.Contains(run.ErrorMessage, "AUTOMATION_POLICY_REJECTED") {
			audited = true
		}
	}
	_ = run
	if !audited {
		t.Fatal("release must carry an AUTOMATION_POLICY_REJECTED audit message")
	}
}

// A linked definition (valid name + issue ID) passes the gate and the
// issue link is forwarded into the spawn.
func TestPolicyGatePassesLinkedDefinition(t *testing.T) {
	now := time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC)
	store := newSchedulerStore()
	store.automations["automation-linked"] = domain.Automation{ID: "automation-linked", ProjectID: "p", IssueID: "github:acme/demo#123", DisplayName: "[ci] #123 Fix flake", Prompt: "Do work", Kind: domain.KindWorker, RRuleText: "DTSTART:20260825T140000Z\nRRULE:FREQ=HOURLY", Timezone: "UTC", Enabled: true, NextRunAt: now}
	spawner := &recordingSpawner{store: store}
	policy := domain.DefaultAutomationPolicy()
	policy.SpawnGate = true
	svc := New(Deps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }, Policy: policy})
	if err := svc.Tick(context.Background(), now); err != nil {
		t.Fatalf("linked definition must dispatch, got %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("expected one spawn, got %d", len(spawner.calls))
	}
	if spawner.calls[0].IssueID != "github:acme/demo#123" {
		t.Fatalf("spawn must carry the definition issue link, got %q", spawner.calls[0].IssueID)
	}
}

func TestPolicyWiringStatus(t *testing.T) {
	svc := New(Deps{Store: newFakeStore(), Policy: domain.DefaultAutomationPolicy()})
	for cap, state := range svc.WiringStatus() {
		if state != "off" {
			t.Fatalf("default capability %s must be off, got %s", cap, state)
		}
	}
	policy := domain.DefaultAutomationPolicy()
	policy.SpawnGate, policy.ReviewerArchive, policy.DraftOnlyReports, policy.AutoReviewer = true, true, true, true
	svc = New(Deps{Store: newFakeStore(), Policy: policy})
	status := svc.WiringStatus()
	if status["spawnGate"] != "active" {
		t.Fatalf("spawnGate must be active in dispatch, got %s", status["spawnGate"])
	}
	for _, cap := range []string{"reviewerArchive", "draftReports", "autoReviewer"} {
		if status[cap] != "staged" {
			t.Fatalf("capability %s must be staged, got %s", cap, status[cap])
		}
	}
}
func TestPolicyServiceCallSurface(t *testing.T) {
	now := time.Now()
	policy := domain.DefaultAutomationPolicy()
	policy.ReviewerArchive, policy.DraftOnlyReports, policy.AutoReviewer = true, true, true
	svc := New(Deps{Store: newFakeStore(), Clock: func() time.Time { return now }, Policy: policy})
	decision := svc.ArchiveReviewer(true, true, true, true, time.Hour, "s1")
	if !decision.Archive || decision.Delete {
		t.Fatal("service archive decision must archive, never delete")
	}
	draft, posted := svc.DraftReport("status", "body")
	if posted || !strings.HasPrefix(draft, "DRAFT") {
		t.Fatal("service drafts must stay DRAFT and never post")
	}
	ok, _ := svc.AssignAutoReviewer(true, false, 0)
	if !ok {
		t.Fatal("service must grant exactly one reviewer")
	}
	if ok, _ := svc.AssignAutoReviewer(true, true, 0); ok {
		t.Fatal("service must honor explicit opt-out")
	}
}

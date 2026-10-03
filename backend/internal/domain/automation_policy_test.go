package domain

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultPolicyAllOff(t *testing.T) {
	p := DefaultAutomationPolicy()
	if p.SpawnGate || p.ReviewerArchive || p.DraftOnlyReports || p.AutoReviewer {
		t.Fatal("default policy must be all off")
	}
	if got := p.ValidateSpawnGate("anything", ""); got != nil {
		t.Fatal("gate off must accept everything")
	}
}

func TestSpawnGate(t *testing.T) {
	p := DefaultAutomationPolicy()
	p.SpawnGate = true
	if got := p.ValidateSpawnGate("[ci] #123 Fix flake", "issue-1"); got != nil {
		t.Fatalf("valid spawn rejected: %v", got)
	}
	if got := p.ValidateSpawnGate("bad name", "issue-1"); got == nil || got.Code != "AUTOMATION_POLICY_REJECTED" {
		t.Fatal("bad name must be 409 AUTOMATION_POLICY_REJECTED")
	}
	if got := p.ValidateSpawnGate("[ci] #123 Fix flake", ""); got == nil {
		t.Fatal("missing issueId must be rejected")
	}
	if got := p.ValidateSpawnGate("[ci] #123 Fix flake", "  "); got == nil {
		t.Fatal("blank issueId must be rejected")
	}
}

func TestArchiveReviewer(t *testing.T) {
	p := DefaultAutomationPolicy()
	p.ReviewerArchive = true
	now := time.Now()
	d := p.ShouldArchiveReviewer(true, true, true, true, time.Hour, now, "s1")
	if !d.Archive || d.Delete {
		t.Fatal("idle+report+triage must archive, never delete")
	}
	d = p.ShouldArchiveReviewer(true, false, false, false, 8*24*time.Hour, now, "s2")
	if !d.Archive || d.Delete {
		t.Fatal("7d must archive, never delete")
	}
	d = p.ShouldArchiveReviewer(true, true, false, false, time.Hour, now, "s3")
	if d.Archive || d.Delete {
		t.Fatal("conditions not met must not archive")
	}
	if d.Audit.Action == "" {
		t.Fatal("every evaluation must be audit-logged")
	}
	off := DefaultAutomationPolicy()
	if d := off.ShouldArchiveReviewer(true, true, true, true, 30*24*time.Hour, now, "s4"); d.Archive {
		t.Fatal("policy off must never archive")
	}
}

func TestDraftOnlyReports(t *testing.T) {
	p := DefaultAutomationPolicy()
	draft, posted := p.DraftReport("status", "hello")
	if posted {
		t.Fatal("must never auto-post")
	}
	if !strings.HasPrefix(draft, "DRAFT") {
		t.Fatal("must always be DRAFT")
	}
}

func TestAutoReviewer(t *testing.T) {
	p := DefaultAutomationPolicy()
	p.AutoReviewer = true
	ok, sug := p.AssignAutoReviewer(true, false, 0)
	if !ok || !strings.Contains(sug, "no auto-fix") {
		t.Fatal("must assign exactly one with triage-only suggestion")
	}
	if ok, _ := p.AssignAutoReviewer(true, true, 0); ok {
		t.Fatal("explicit opt-out must win")
	}
	if ok, _ := p.AssignAutoReviewer(true, false, 1); ok {
		t.Fatal("budget cap max 1 must hold")
	}
	if ok, _ := p.AssignAutoReviewer(false, false, 0); ok {
		t.Fatal("no worker-done means no reviewer")
	}
	off := DefaultAutomationPolicy()
	if ok, _ := off.AssignAutoReviewer(true, false, 0); ok {
		t.Fatal("policy off means no auto-reviewer")
	}
}

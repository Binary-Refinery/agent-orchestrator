package daemon

import (
	"testing"
)

func TestAutomationPolicyFromEnvDefaultOff(t *testing.T) {
	t.Setenv("AO_AUTOMATION_SPAWN_GATE", "")
	t.Setenv("AO_AUTOMATION_REVIEWER_ARCHIVE", "")
	t.Setenv("AO_AUTOMATION_DRAFT_ONLY_REPORTS", "")
	t.Setenv("AO_AUTOMATION_AUTO_REVIEWER", "")
	policy := automationPolicyFromEnv()
	if policy.SpawnGate || policy.ReviewerArchive || policy.DraftOnlyReports || policy.AutoReviewer {
		t.Fatalf("env default must be all off, got %+v", policy)
	}
}

func TestAutomationPolicyFromEnvOptIn(t *testing.T) {
	t.Setenv("AO_AUTOMATION_SPAWN_GATE", "1")
	t.Setenv("AO_AUTOMATION_REVIEWER_ARCHIVE", "true")
	t.Setenv("AO_AUTOMATION_DRAFT_ONLY_REPORTS", "on")
	t.Setenv("AO_AUTOMATION_AUTO_REVIEWER", "yes")
	policy := automationPolicyFromEnv()
	if !policy.SpawnGate || !policy.ReviewerArchive || !policy.DraftOnlyReports || !policy.AutoReviewer {
		t.Fatalf("opt-in env must enable all switches, got %+v", policy)
	}
	if got := policy.EffectiveReviewerBudget(); got != 1 {
		t.Fatalf("budget must stay capped at 1, got %d", got)
	}
}

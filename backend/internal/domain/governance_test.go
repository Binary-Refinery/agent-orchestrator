package domain

import (
	"context"
	"testing"
)

func TestGovernanceManagedRejectsNativeAutomation(t *testing.T) {
	for _, cfg := range []ProjectConfig{
		{GovernanceManaged: true, AutoReview: true},
		{GovernanceManaged: true, TrackerIntake: TrackerIntakeConfig{Enabled: true, Assignee: "test"}},
	} {
		if cfg.Validate() == nil {
			t.Fatal("managed native automation was accepted")
		}
	}
	if err := (ProjectConfig{GovernanceManaged: true}).Validate(); err != nil {
		t.Fatal(err)
	}
	if NativeHistoryRequired(context.Background()) {
		t.Fatal("requirement must be explicit")
	}
	if !NativeHistoryRequired(WithNativeHistoryRequired(context.Background())) {
		t.Fatal("requirement lost")
	}
}

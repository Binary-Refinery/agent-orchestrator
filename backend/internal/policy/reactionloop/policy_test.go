package reactionloop

import (
	"testing"
)

func TestReactionDisabledByDefault(t *testing.T) {
	if EnabledFromEnv("") {
		t.Fatal("empty env must stay disabled")
	}
	for _, v := range []string{"0", "false", "off", "no", "random"} {
		if EnabledFromEnv(v) {
			t.Fatalf("env %q must stay disabled", v)
		}
	}
	for _, v := range []string{"1", "true", "yes", "on", " TRUE "} {
		if !EnabledFromEnv(v) {
			t.Fatalf("env %q must enable", v)
		}
	}
}

func TestPlanPollGates(t *testing.T) {
	budget := DefaultBudget()
	if d := PlanPoll(false, budget, Usage{}); d.Allow || d.Reason != ReasonDisabled {
		t.Fatalf("disabled poll = %+v, want disabled deny", d)
	}
	if d := PlanPoll(true, budget, Usage{}); !d.Allow {
		t.Fatalf("fresh budget denied: %+v", d)
	}
	if d := PlanPoll(true, budget, Usage{PollsPR: budget.MaxPerPR}); d.Allow || d.Reason != ReasonBudgetPR {
		t.Fatalf("exhausted per-PR budget = %+v, want budget_pr deny", d)
	}
	if d := PlanPoll(true, budget, Usage{PollsDay: budget.MaxPerDay}); d.Allow || d.Reason != ReasonBudgetDay {
		t.Fatalf("exhausted daily budget = %+v, want budget_day deny", d)
	}
	if d := PlanPoll(true, budget, Usage{PollsPR: budget.MaxPerPR - 1, PollsDay: budget.MaxPerDay}); d.Allow || d.Reason != ReasonBudgetDay {
		t.Fatalf("daily cap must bind first when both bind: %+v", d)
	}
	if d := PlanPoll(true, Budget{}, Usage{}); d.Allow {
		t.Fatalf("zero budget must deny: %+v", d)
	}
}

func TestRedLinesAlwaysDenied(t *testing.T) {
	for _, kind := range RedLines {
		if d := AttemptRedLine(kind); d.Allow || d.Reason != ReasonRedLine {
			t.Fatalf("red line %q = %+v, want red_line deny", kind, d)
		}
	}
	if len(RedLines) != 6 {
		t.Fatalf("want 6 enumerated red lines, got %d", len(RedLines))
	}
}

package trackerintake

import (
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func v2Cfg() domain.TrackerIntakeConfig {
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

func v2Issue(labels ...string) domain.Issue {
	return domain.Issue{
		ID:     domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#1"},
		State:  domain.IssueOpen,
		Labels: labels,
	}
}

func TestV2DisabledDeniesEverything(t *testing.T) {
	off := domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}
	if d := V2IssueAdmitted(v2Issue("ao-ready"), off); d.Allow || d.Reason != V2ReasonDisabled {
		t.Fatalf("issue = %+v, want disabled deny", d)
	}
	if d := V2WIPGate([]V2Take{{IssueID: "x"}}, V2Take{IssueID: "x"}, nil, off); d.Allow {
		t.Fatalf("wip = %+v, want deny when off", d)
	}
	if d := V2Preflight(V2PreflightInput{BaseResolvable: true, HarnessReady: true, RepoClean: true, BudgetRemaining: 1}, off); d.Allow {
		t.Fatalf("preflight = %+v, want deny when off", d)
	}
}

func TestV2IntakeRequiresLabel(t *testing.T) {
	cfg := v2Cfg()
	if d := V2IssueAdmitted(v2Issue("ao-ready"), cfg); !d.Allow {
		t.Fatalf("labeled issue denied: %+v", d)
	}
	if d := V2IssueAdmitted(v2Issue("other"), cfg); d.Allow || d.Reason != V2ReasonNoLabel {
		t.Fatalf("unlabeled issue = %+v, want no_label deny", d)
	}
	noLabel := v2Cfg()
	noLabel.AutomationV2.Label = ""
	if d := V2IssueAdmitted(v2Issue("ao-ready"), noLabel); d.Allow {
		t.Fatalf("empty required label must admit nothing, got %+v", d)
	}
}

func TestV2WIPGateFIFOAndLimit(t *testing.T) {
	cfg := v2Cfg()
	head := V2Take{IssueNumber: 1, IssueID: "github:acme/demo#1"}
	second := V2Take{IssueNumber: 2, IssueID: "github:acme/demo#2"}
	queue := []V2Take{head, second}
	if d := V2WIPGate(queue, second, nil, cfg); d.Allow || d.Reason != V2ReasonNotFirst {
		t.Fatalf("non-head take = %+v, want FIFO deny", d)
	}
	if d := V2WIPGate(queue, head, nil, cfg); !d.Allow {
		t.Fatalf("head take denied: %+v", d)
	}
	full := []V2ActiveTake{{}}
	if d := V2WIPGate(queue, head, full, cfg); d.Allow || d.Reason != V2ReasonWIPFull {
		t.Fatalf("full WIP = %+v, want wip_full deny", d)
	}
}

func TestV2WIPGateStalledReviewBlocks(t *testing.T) {
	cfg := v2Cfg()
	head := V2Take{IssueNumber: 1, IssueID: "github:acme/demo#1"}
	stalled := []V2ActiveTake{{Stalled: true, StalledFor: 2 * time.Hour}}
	if d := V2WIPGate([]V2Take{head}, head, stalled, cfg); d.Allow || d.Reason != V2ReasonReviewStalled {
		t.Fatalf("stalled = %+v, want review_stalled deny", d)
	}
	cfg.AutomationV2.MaxParallel = 2
	fresh := []V2ActiveTake{{Stalled: true, StalledFor: time.Minute}}
	if d := V2WIPGate([]V2Take{head}, head, fresh, cfg); !d.Allow {
		t.Fatalf("fresh stall should not block yet: %+v", d)
	}
}

func TestV2PreflightDefersOnOverlap(t *testing.T) {
	cfg := v2Cfg()
	ok := V2PreflightInput{BaseResolvable: true, HarnessReady: true, RepoClean: true, BudgetRemaining: 2}
	if d := V2Preflight(ok, cfg); !d.Allow {
		t.Fatalf("clean preflight denied: %+v", d)
	}
	overlap := ok
	overlap.OverlapSuspects = []string{"backend/foo.go"}
	if d := V2Preflight(overlap, cfg); d.Allow || d.Reason != V2ReasonFileOverlap {
		t.Fatalf("overlap = %+v, want defer with reason", d)
	}
	for name, mut := range map[string]func(*V2PreflightInput){
		"base":    func(i *V2PreflightInput) { i.BaseResolvable = false },
		"harness": func(i *V2PreflightInput) { i.HarnessReady = false },
		"dirty":   func(i *V2PreflightInput) { i.RepoClean = false },
		"budget":  func(i *V2PreflightInput) { i.BudgetRemaining = 0 },
	} {
		bad := ok
		mut(&bad)
		if d := V2Preflight(bad, cfg); d.Allow {
			t.Fatalf("%s preflight should deny, got %+v", name, d)
		}
	}
}

func TestV2DefaultsResolve(t *testing.T) {
	c := &domain.AutomationV2Config{Enabled: true, Label: "ao-ready"}
	if c.ResolvedMaxParallel() != domain.DefaultV2MaxParallel {
		t.Fatalf("maxParallel = %d, want default %d", c.ResolvedMaxParallel(), domain.DefaultV2MaxParallel)
	}
	if c.ResolvedReviewTimeoutSeconds() != domain.DefaultV2ReviewTimeoutSeconds {
		t.Fatalf("timeout = %d, want default", c.ResolvedReviewTimeoutSeconds())
	}
}

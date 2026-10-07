package supervision

import (
	"errors"
	"testing"
	"time"
)

func TestSupervisionDisabledByDefault(t *testing.T) {
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

func TestWakeupMandatesTurn(t *testing.T) {
	for _, k := range []ReportKind{ReportDone, ReportCheckpoint} {
		p := PlanWakeup(true, k)
		if !p.Decision.Allow || p.Attempts != 1 {
			t.Fatalf("kind %q = %+v, want mandated turn", k, p)
		}
	}
	if p := PlanWakeup(false, ReportDone); p.Decision.Allow || p.Decision.Reason != ReasonDisabled {
		t.Fatalf("disabled wakeup = %+v, want disabled deny", p)
	}
	if p := PlanWakeup(true, "bogus"); p.Decision.Allow || p.Decision.Reason != ReasonWakeupInvalidKind {
		t.Fatalf("unknown kind = %+v, want invalid-kind deny", p)
	}
}

func TestWakeupRetryAndAudit(t *testing.T) {
	calls := 0
	flaky := func() error {
		calls++
		if calls < 3 {
			return errors.New("daemon busy")
		}
		return nil
	}
	p := DeliverWakeup(true, ReportDone, flaky)
	if !p.Decision.Allow || p.Attempts != 3 || p.Audit != "" {
		t.Fatalf("flaky deliver = %+v, want success on 3rd try without audit", p)
	}
	alwaysFail := func() error { return errors.New("down") }
	p = DeliverWakeup(true, ReportCheckpoint, alwaysFail)
	if p.Decision.Allow || p.Decision.Reason != ReasonWakeupFailed {
		t.Fatalf("failing deliver = %+v, want failed-audit deny", p)
	}
	if p.Audit == "" || p.Attempts != MaxWakeupAttempts {
		t.Fatalf("failing deliver must carry audit + max attempts: %+v", p)
	}
	p = DeliverWakeup(true, ReportDone, nil)
	if p.Decision.Allow || p.Decision.Reason != ReasonWakeupFailed || p.Audit == "" {
		t.Fatalf("nil deliver = %+v, want fail-closed audit deny", p)
	}
	p = DeliverWakeup(false, ReportDone, flaky)
	if p.Decision.Allow || p.Decision.Reason != ReasonDisabled {
		t.Fatalf("disabled deliver = %+v, want disabled deny", p)
	}
}

func TestWatchdogNudgeThenEscalate(t *testing.T) {
	cfg := DefaultWatchdogConfig()
	// Simulated standstill: quiet -> nudge -> escalation.
	if c := CheckStall(true, cfg, cfg.NudgeAfter-time.Minute); !c.Decision.Allow || c.Outcome != OutcomeQuiet {
		t.Fatalf("recent progress = %+v, want quiet", c)
	}
	if c := CheckStall(true, cfg, cfg.NudgeAfter); !c.Decision.Allow || c.Outcome != OutcomeNudge {
		t.Fatalf("quiet window elapsed = %+v, want nudge first", c)
	}
	if c := CheckStall(true, cfg, cfg.EscalateAfter); !c.Decision.Allow || c.Outcome != OutcomeEscalate {
		t.Fatalf("escalation window elapsed = %+v, want escalation", c)
	}
	if c := CheckStall(true, cfg, cfg.EscalateAfter+time.Hour); c.Outcome != OutcomeEscalate {
		t.Fatalf("long stall = %+v, must stay escalated, never silent idle", c)
	}
}

func TestWatchdogNegative(t *testing.T) {
	cfg := DefaultWatchdogConfig()
	if c := CheckStall(false, cfg, time.Hour); c.Decision.Allow || c.Decision.Reason != ReasonDisabled {
		t.Fatalf("disabled watchdog = %+v, want disabled deny", c)
	}
	for _, bad := range []WatchdogConfig{{}, {NudgeAfter: -time.Minute, EscalateAfter: time.Minute}, {NudgeAfter: time.Hour, EscalateAfter: time.Minute}} {
		if c := CheckStall(true, bad, time.Hour); c.Decision.Allow || c.Decision.Reason != ReasonStallInvalid {
			t.Fatalf("bad windows %+v = %+v, want invalid deny", bad, c)
		}
	}
	if c := CheckStall(true, cfg, -time.Second); c.Decision.Allow || c.Decision.Reason != ReasonStallInvalid {
		t.Fatalf("negative elapsed = %+v, want invalid deny", c)
	}
}

func TestChainDriverStepsAndTimeouts(t *testing.T) {
	to := DefaultChainTimeouts()
	// Full chain walk, role-agnostic: only steps, no role names.
	cur := StepWorker
	for _, want := range []ChainStep{StepReview, StepTriage} {
		m := AdvanceChain(true, to, cur, time.Minute, true)
		if !m.Decision.Allow || m.Next != want {
			t.Fatalf("advance %q = %+v, want %q", cur, m, want)
		}
		cur = m.Next
	}
	// Triage->fix is human-gated regardless of role.
	m := AdvanceChain(true, to, StepTriage, time.Minute, false)
	if m.Decision.Allow || m.Decision.Reason != ReasonNeedsHuman || m.Next != StepTriage {
		t.Fatalf("ungated triage->fix = %+v, want needs-human hold", m)
	}
	m = AdvanceChain(true, to, StepTriage, time.Minute, true)
	if !m.Decision.Allow || m.Next != StepFix {
		t.Fatalf("gated triage->fix = %+v, want fix", m)
	}
	// Per-step timeout holds the step for escalation.
	m = AdvanceChain(true, to, StepFix, 24*time.Hour, true)
	if m.Decision.Allow || m.Decision.Reason != ReasonChainTimeout || m.Next != StepFix {
		t.Fatalf("timed-out fix = %+v, want timeout hold", m)
	}
	// Terminal holds.
	m = AdvanceChain(true, to, StepClosed, 0, true)
	if m.Decision.Allow || m.Decision.Reason != ReasonChainTerminal {
		t.Fatalf("terminal advance = %+v, want terminal deny", m)
	}
	// Unknown step + disabled deny closed.
	m = AdvanceChain(true, to, "auditor", 0, true)
	if m.Decision.Allow || m.Decision.Reason != ReasonChainInvalid {
		t.Fatalf("unknown step = %+v, want invalid deny", m)
	}
	m = AdvanceChain(false, to, StepWorker, 0, true)
	if m.Decision.Allow || m.Decision.Reason != ReasonDisabled {
		t.Fatalf("disabled chain = %+v, want disabled deny", m)
	}
	// Missing timeout config denies fail-closed.
	m = AdvanceChain(true, ChainTimeouts{}, StepWorker, 0, true)
	if m.Decision.Allow || m.Decision.Reason != ReasonChainTimeout {
		t.Fatalf("missing timeouts = %+v, want timeout deny", m)
	}
}

func TestChainRoleIndependence(t *testing.T) {
	to := DefaultChainTimeouts()
	// Any present or future role name must not change the machine:
	// the driver takes no role input at all, so the same step from
	// two different role occupants advances identically.
	for _, step := range []ChainStep{StepWorker, StepReview, StepFix, StepDelta} {
		a := AdvanceChain(true, to, step, time.Minute, true)
		b := AdvanceChain(true, to, step, time.Minute, true)
		if a.Next != b.Next || a.Decision.Allow != b.Decision.Allow {
			t.Fatalf("step %q not role-independent: %+v vs %+v", step, a, b)
		}
	}
	if len(ChainSteps) != 6 {
		t.Fatalf("want 6 chain steps, got %d", len(ChainSteps))
	}
}

func TestBudgetCaps(t *testing.T) {
	b := DefaultBudget()
	if d := PlanStep(true, b, Usage{}, false, false); !d.Allow {
		t.Fatalf("fresh budget denied: %+v", d)
	}
	if d := PlanStep(false, b, Usage{}, false, false); d.Allow || d.Reason != ReasonDisabled {
		t.Fatalf("disabled budget = %+v, want disabled deny", d)
	}
	if d := PlanStep(true, b, Usage{Rounds: b.MaxRounds}, false, false); d.Allow || d.Reason != ReasonBudgetRound {
		t.Fatalf("exhausted rounds = %+v, want round deny", d)
	}
	if d := PlanStep(true, b, Usage{SessionSteps: b.MaxSessionSteps}, false, false); d.Allow || d.Reason != ReasonBudgetSession {
		t.Fatalf("exhausted session = %+v, want session deny", d)
	}
	// Third fix round impossible without explicit human exception.
	third := Usage{FixRounds: 2}
	if d := PlanStep(true, b, third, true, false); d.Allow || d.Reason != ReasonBudgetFixCap {
		t.Fatalf("third fix without exception = %+v, want fix-cap deny", d)
	}
	if d := PlanStep(true, b, third, true, true); !d.Allow {
		t.Fatalf("third fix with human exception denied: %+v", d)
	}
	// Fix cap binds even with round/session budget remaining; a
	// non-fix step with the same counters still allows.
	if d := PlanStep(true, b, third, false, false); !d.Allow {
		t.Fatalf("non-fix step under fix-heavy usage denied: %+v", d)
	}
	if d := PlanStep(true, Budget{}, Usage{}, false, false); d.Allow {
		t.Fatalf("zero budget must deny")
	}
	// Negative caller-supplied counters fail closed, one per counter.
	for _, bad := range []Usage{{Rounds: -1}, {SessionSteps: -1}, {FixRounds: -1}} {
		if d := PlanStep(true, b, bad, false, false); d.Allow || d.Reason != ReasonBudgetInvalid {
			t.Fatalf("negative usage %+v = %+v, want invalid-usage deny", bad, d)
		}
	}
	// A negative fix counter denies even with an explicit human
	// exception: the exception authorizes a third round, never corrupt
	// input.
	if d := PlanStep(true, b, Usage{FixRounds: -1}, true, true); d.Allow || d.Reason != ReasonBudgetInvalid {
		t.Fatalf("negative fix counter with exception = %+v, want invalid-usage deny", d)
	}
}

func TestRedLinesNeverAllowed(t *testing.T) {
	for _, kind := range RedLines {
		if d := AttemptRedLine(kind); d.Allow || d.Reason != ReasonRedLine {
			t.Fatalf("red line %q = %+v, want red_line deny", kind, d)
		}
		// Also denied when policy is off: no path through this package.
		if d := AttemptRedLine(kind); d.Allow {
			t.Fatalf("red line %q allowed while off", kind)
		}
	}
	if len(RedLines) != 6 {
		t.Fatalf("want 6 enumerated red lines, got %d", len(RedLines))
	}
}

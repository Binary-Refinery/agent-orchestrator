package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/supervision"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type fakeNotifySink struct {
	intents []ports.NotificationIntent
	err     error
}

func (f *fakeNotifySink) Notify(_ context.Context, intent ports.NotificationIntent) error {
	if f.err != nil {
		return f.err
	}
	f.intents = append(f.intents, intent)
	return nil
}

func (f *fakeNotifySink) Resolve(_ context.Context, _ ports.NotificationResolution) error {
	return nil
}

func enabledSupervision() SupervisionConfig {
	return SupervisionConfig{
		Enabled:  true,
		Watchdog: supervision.DefaultWatchdogConfig(),
		Chain:    supervision.DefaultChainTimeouts(),
		Budget:   supervision.DefaultBudget(),
	}
}

func TestSupervisionDisabledIsNoOp(t *testing.T) {
	m, _, msg := newManager()
	id := domain.SessionID("sess-disabled")
	if plan := m.SupervisedReportTurn(ctx, id, supervision.ReportDone); plan.Decision.Allow {
		t.Fatalf("disabled report turn allowed: %+v", plan)
	}
	if check := m.SupervisionStallCheck(ctx, id, "mer", time.Hour); check.Decision.Allow {
		t.Fatalf("disabled stall check allowed: %+v", check)
	}
	if d := m.SupervisionPlanStep(supervision.Usage{}, false, false); d.Allow {
		t.Fatalf("disabled plan step allowed: %+v", d)
	}
	if mv := m.SupervisionAdvance(supervision.StepWorker, 0, true); mv.Decision.Allow {
		t.Fatalf("disabled advance allowed: %+v", mv)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("disabled supervision sent %d messages", len(msg.msgs))
	}
	if len(m.SupervisionAudits()) != 0 {
		t.Fatalf("disabled supervision recorded audits")
	}
}

func TestSupervisedReportTurnDeliversOrchestratorTurn(t *testing.T) {
	m, st, msg := newManager(WithSupervision(enabledSupervision()))
	id := domain.SessionID("sess-wakeup")
	st.sessions[id] = working(id)
	plan := m.SupervisedReportTurn(ctx, id, supervision.ReportCheckpoint)
	if !plan.Decision.Allow || plan.Audit != "" {
		t.Fatalf("enabled report turn = %+v, want delivered turn without audit", plan)
	}
	if len(msg.msgs) != 1 {
		t.Fatalf("want 1 orchestrator-turn message, got %d", len(msg.msgs))
	}
}

func TestSupervisedReportTurnFailureAuditsDurably(t *testing.T) {
	sink := &fakeNotifySink{}
	m, st, msg := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(sink))
	msg.err = errors.New("messenger down")
	id := domain.SessionID("sess-wakeup-fail")
	st.sessions[id] = working(id)
	plan := m.SupervisedReportTurn(ctx, id, supervision.ReportDone)
	if plan.Decision.Allow || plan.Audit == "" {
		t.Fatalf("failed report turn = %+v, want fail-closed audit deny", plan)
	}
	if len(sink.intents) == 0 {
		t.Fatal("wake-up failure must persist a durable human-visible notification")
	}
	if got := sink.intents[0]; got.Type != domain.NotificationNeedsInput || got.SessionID != id {
		t.Fatalf("audit notification = %+v, want needs_input for the session", got)
	}
	if len(m.SupervisionAudits()) == 0 {
		t.Fatal("wake-up failure must append to the manager audit trail")
	}
}

func TestSupervisionStallNudgeThenEscalate(t *testing.T) {
	sink := &fakeNotifySink{}
	m, st, msg := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(sink))
	id := domain.SessionID("sess-stall")
	st.sessions[id] = working(id)
	cfg := supervision.DefaultWatchdogConfig()

	// Simulated standstill on the wired path: quiet sends nothing,
	// nudge pings the worker, escalation pings the human durably.
	if check := m.SupervisionStallCheck(ctx, id, "mer", cfg.NudgeAfter-time.Minute); !check.Decision.Allow || check.Outcome != supervision.OutcomeQuiet {
		t.Fatalf("recent progress = %+v, want quiet", check)
	}
	if len(msg.msgs) != 0 || len(sink.intents) != 0 {
		t.Fatal("quiet must send nothing")
	}
	if check := m.SupervisionStallCheck(ctx, id, "mer", cfg.NudgeAfter); check.Outcome != supervision.OutcomeNudge {
		t.Fatalf("quiet window elapsed = %+v, want nudge first", check)
	}
	if len(msg.msgs) != 1 {
		t.Fatalf("nudge must send exactly one worker message, got %d", len(msg.msgs))
	}
	if check := m.SupervisionStallCheck(ctx, id, "mer", cfg.EscalateAfter); check.Outcome != supervision.OutcomeEscalate {
		t.Fatalf("escalation window elapsed = %+v, want escalation", check)
	}
	if len(sink.intents) == 0 || sink.intents[len(sink.intents)-1].Type != domain.NotificationNeedsInput {
		t.Fatalf("escalation must persist a durable needs_input notification: %+v", sink.intents)
	}
}

func TestSupervisionBudgetAndChainGatesWired(t *testing.T) {
	m, _, _ := newManager(WithSupervision(enabledSupervision()))
	if d := m.SupervisionPlanStep(supervision.Usage{}, false, false); !d.Allow {
		t.Fatalf("fresh wired budget denied: %+v", d)
	}
	// Third fix round impossible without an explicit human exception,
	// even on the wired path with round/session budget remaining.
	third := supervision.Usage{FixRounds: 2}
	if d := m.SupervisionPlanStep(third, true, false); d.Allow {
		t.Fatalf("wired third fix without exception allowed: %+v", d)
	}
	if d := m.SupervisionPlanStep(third, true, true); !d.Allow {
		t.Fatalf("wired third fix with human exception denied: %+v", d)
	}
	// Negative counters fail closed on the wired path too.
	for _, bad := range []supervision.Usage{{Rounds: -1}, {SessionSteps: -1}, {FixRounds: -1}} {
		if d := m.SupervisionPlanStep(bad, false, false); d.Allow {
			t.Fatalf("wired negative usage %+v allowed: %+v", bad, d)
		}
	}
	mv := m.SupervisionAdvance(supervision.StepTriage, time.Minute, false)
	if mv.Decision.Allow || mv.Next != supervision.StepTriage {
		t.Fatalf("wired ungated triage->fix = %+v, want human-gate hold", mv)
	}
}

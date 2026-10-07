package lifecycle

import (
	"context"
	"errors"
	"strings"
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

func withState(rec domain.SessionRecord, state domain.ActivityState) domain.SessionRecord {
	rec.Activity.State = state
	return rec
}

func withKind(rec domain.SessionRecord, kind domain.SessionKind) domain.SessionRecord {
	rec.Kind = kind
	return rec
}

func TestSupervisedReportTurnForWorkerTargetsActiveOrchestrator(t *testing.T) {
	sink := &fakeNotifySink{}
	m, st, msg := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(sink))
	worker := domain.SessionID("sess-worker")
	orch := domain.SessionID("sess-orchestrator")
	stale := domain.SessionID("sess-orch-stale")
	st.sessions[worker] = withKind(working(worker), domain.KindWorker)
	st.sessions[orch] = withKind(working(orch), domain.KindOrchestrator)
	st.sessions[stale] = withKind(withState(working(stale), domain.ActivityExited), domain.KindOrchestrator)

	plan := m.SupervisedReportTurnForWorker(ctx, worker, "mer", supervision.ReportDone)
	if !plan.Decision.Allow || plan.Audit != "" {
		t.Fatalf("worker report wake-up = %+v, want delivered orchestrator turn", plan)
	}
	// Exactly one delivery, and it targets the active orchestrator —
	// never the reporting worker, never the exited orchestrator.
	if len(msg.ids) != 1 || msg.ids[0] != orch {
		t.Fatalf("wake-up deliveries = %q, want exactly [%q]", msg.ids, orch)
	}
}

func TestSupervisedReportTurnForWorkerWithoutOrchestratorDeniesAndAudits(t *testing.T) {
	sink := &fakeNotifySink{}
	m, st, msg := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(sink))
	worker := domain.SessionID("sess-lone-worker")
	st.sessions[worker] = withKind(working(worker), domain.KindWorker)

	plan := m.SupervisedReportTurnForWorker(ctx, worker, "mer", supervision.ReportCheckpoint)
	if plan.Decision.Allow || plan.Audit == "" {
		t.Fatalf("orchestrator-less wake-up = %+v, want fail-closed audit deny", plan)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("orchestrator-less wake-up wrote %d pane messages, want none", len(msg.msgs))
	}
	if len(sink.intents) != 1 {
		t.Fatalf("orchestrator-less wake-up must persist exactly one audit: %+v", sink.intents)
	}
}

func TestSupervisionRepeatedEscalationIsIdempotent(t *testing.T) {
	sink := &fakeNotifySink{}
	m, st, _ := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(sink))
	id := domain.SessionID("sess-repeat")
	st.sessions[id] = working(id)
	cfg := supervision.DefaultWatchdogConfig()

	// The same stall escalated twice persists exactly one durable
	// audit and one notification — repeated polls never duplicate.
	for i := 0; i < 2; i++ {
		if check := m.SupervisionStallCheck(ctx, id, "mer", cfg.EscalateAfter); check.Outcome != supervision.OutcomeEscalate {
			t.Fatalf("poll %d = %+v, want escalation", i, check)
		}
	}
	if audits := m.SupervisionAudits(); len(audits) != 1 {
		t.Fatalf("repeated escalation audits = %q, want exactly one", audits)
	}
	if len(sink.intents) != 1 {
		t.Fatalf("repeated escalation intents = %d, want exactly one", len(sink.intents))
	}
	// The durable readback carries the session, project, and type the
	// notification store persists across restarts.
	got := sink.intents[0]
	if got.Type != domain.NotificationNeedsInput || got.SessionID != id || got.ProjectID != domain.ProjectID("mer") || got.CreatedAt.IsZero() {
		t.Fatalf("durable escalation readback = %+v, want needs_input with session/project/timestamp", got)
	}
}

func TestSupervisedReportTurnBlockedWritesNothingAndAudits(t *testing.T) {
	sink := &fakeNotifySink{}
	m, st, msg := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(sink))
	id := domain.SessionID("sess-blocked")
	st.sessions[id] = withState(working(id), domain.ActivityBlocked)
	plan := m.SupervisedReportTurn(ctx, id, supervision.ReportDone)
	if plan.Decision.Allow || plan.Audit == "" {
		t.Fatalf("blocked wake-up = %+v, want fail-closed audit deny", plan)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("blocked wake-up wrote %d pane messages, want none", len(msg.msgs))
	}
	if len(sink.intents) != 1 || sink.intents[0].Type != domain.NotificationNeedsInput {
		t.Fatalf("blocked wake-up must persist exactly one needs_input audit: %+v", sink.intents)
	}
}

func TestSupervisedReportTurnMissingWritesNothingAndAudits(t *testing.T) {
	sink := &fakeNotifySink{}
	m, _, msg := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(sink))
	plan := m.SupervisedReportTurn(ctx, "sess-missing", supervision.ReportCheckpoint)
	if plan.Decision.Allow || plan.Audit == "" {
		t.Fatalf("missing-session wake-up = %+v, want fail-closed audit deny", plan)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("missing-session wake-up wrote %d pane messages, want none", len(msg.msgs))
	}
	if audits := m.SupervisionAudits(); len(audits) != 1 {
		t.Fatalf("missing-session wake-up audits = %q, want exactly one", audits)
	}
}

func TestSupervisionStallNudgeAtWaitingInputWritesNothingAndEscalates(t *testing.T) {
	sink := &fakeNotifySink{}
	m, st, msg := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(sink))
	id := domain.SessionID("sess-waiting")
	st.sessions[id] = withState(working(id), domain.ActivityWaitingInput)
	cfg := supervision.DefaultWatchdogConfig()
	check := m.SupervisionStallCheck(ctx, id, "mer", cfg.NudgeAfter)
	if check.Outcome != supervision.OutcomeEscalate {
		t.Fatalf("refused stall nudge = %+v, want escalation", check)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("refused stall nudge wrote %d pane messages, want none", len(msg.msgs))
	}
	if len(sink.intents) != 1 || sink.intents[0].Type != domain.NotificationNeedsInput {
		t.Fatalf("refused stall nudge must persist exactly one needs_input escalation: %+v", sink.intents)
	}
	if audits := m.SupervisionAudits(); len(audits) != 1 {
		t.Fatalf("refused stall nudge audits = %q, want exactly one", audits)
	}
}

func TestSupervisionAuditExactlyOnceAndSurvivesNotifierFailure(t *testing.T) {
	sink := &fakeNotifySink{}
	m, st, _ := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(sink))
	id := domain.SessionID("sess-audit-once")
	st.sessions[id] = withState(working(id), domain.ActivityBlocked)
	plan := m.SupervisedReportTurn(ctx, id, supervision.ReportDone)
	if plan.Audit == "" {
		t.Fatalf("want audit, got %+v", plan)
	}
	audits := m.SupervisionAudits()
	if len(audits) != 1 {
		t.Fatalf("audits = %q, want exactly one entry", audits)
	}
	wantPrefix := "session=" + string(id) + " "
	if len(audits[0]) < len(wantPrefix) || audits[0][:len(wantPrefix)] != wantPrefix {
		t.Fatalf("audit %q must start with %q", audits[0], wantPrefix)
	}

	// Notifier failure: the audit still exists exactly once, now
	// carrying the persistence error instead of dropping it.
	failing := &fakeNotifySink{err: errors.New("notification store down")}
	m2, _, msg2 := newManager(WithSupervision(enabledSupervision()), WithNotificationSink(failing))
	msg2.err = errors.New("messenger down")
	plan2 := m2.SupervisedReportTurn(ctx, "sess-audit-fail", supervision.ReportDone)
	if plan2.Decision.Allow || plan2.Audit == "" {
		t.Fatalf("failed wake-up = %+v, want fail-closed audit deny", plan2)
	}
	audits2 := m2.SupervisionAudits()
	if len(audits2) != 1 {
		t.Fatalf("notifier-failure audits = %q, want exactly one entry", audits2)
	}
	if !strings.Contains(audits2[0], "notify_err=notification store down") {
		t.Fatalf("notifier-failure audit %q must carry the persistence error", audits2[0])
	}
}

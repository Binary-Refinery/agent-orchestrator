// Supervision wiring for slice af-ao-supervision-v1: opt-in,
// default-off, fail-closed enforcement of the supervision policy
// (see internal/policy/supervision) on the lifecycle session paths.
//
// When disabled (default), every entry point below is a no-op that
// denies closed with supervision_disabled and performs no send,
// no notification, and no state change. When enabled:
//
//   - SupervisedReportTurn mandates one orchestrator turn per
//     done/checkpoint report, delivered through the session guard with
//     retry; terminal delivery failure is persisted as a durable
//     needs-input notification (human-visible audit) and returned in
//     the plan audit.
//   - SupervisionStallCheck evaluates time-since-progress: nudge goes
//     through the guard messenger, escalation goes to the human as a
//     durable needs-input notification. There is no silent-idle path.
//   - SupervisionPlanStep / SupervisionAdvance enforce the budget and
//     chain gates before a session step starts.
//
// Red lines: this file never auto-pushes, auto-merges, auto-terminates
// a worker, auto-confirms a finding, automates publish-go, or bypasses
// a human gate. Human-gated moves still require the human flag.
package lifecycle

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/supervision"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// SupervisionConfig carries the opt-in supervision enforcement wired
// onto the lifecycle manager. Zero value is disabled.
type SupervisionConfig struct {
	// Enabled turns enforcement on. Default off; production wires it
	// from AO_SUPERVISION via SupervisionConfigFromEnv.
	Enabled bool
	// Watchdog bounds the stall nudge/escalation windows.
	Watchdog supervision.WatchdogConfig
	// Chain bounds the per-step chain deadlines.
	Chain supervision.ChainTimeouts
	// Budget caps chain rounds and session steps.
	Budget supervision.Budget
}

// SupervisionConfigFromEnv builds the production config: opt-in via
// AO_SUPERVISION, conservative defaults otherwise.
func SupervisionConfigFromEnv() SupervisionConfig {
	return SupervisionConfig{
		Enabled:  supervision.Enabled(),
		Watchdog: supervision.DefaultWatchdogConfig(),
		Chain:    supervision.DefaultChainTimeouts(),
		Budget:   supervision.DefaultBudget(),
	}
}

// WithSupervision wires opt-in supervision enforcement onto the manager.
func WithSupervision(cfg SupervisionConfig) Option {
	return func(m *Manager) { m.supervision = cfg }
}

// SupervisionAudits returns the durable audit trail recorded by the
// supervision wiring (wake-up failures, escalations) for this manager.
func (m *Manager) SupervisionAudits() []string {
	m.supMu.Lock()
	defer m.supMu.Unlock()
	return append([]string(nil), m.supAudits...)
}

func (m *Manager) recordSupAudit(entry string) {
	m.supMu.Lock()
	defer m.supMu.Unlock()
	m.supAudits = append(m.supAudits, entry)
}

// supervised reports whether enforcement applies. Fail-closed: an
// unset manager or a disabled config never enforces.
func (m *Manager) supervised() bool {
	if m == nil {
		return false
	}
	return m.supervision.Enabled
}

// SupervisedReportTurn mandates one orchestrator turn for a
// done/checkpoint worker report on the session. Disabled denies closed
// with no side effects. Enabled delivers through the session guard
// with retry; terminal failure records a durable needs-input
// notification (human-visible audit) and returns the audit in the plan.
func (m *Manager) SupervisedReportTurn(ctx context.Context, id domain.SessionID, kind supervision.ReportKind) supervision.WakeupPlan {
	if !m.supervised() {
		return supervision.WakeupPlan{Decision: denyClosed()}
	}
	if m.guard == nil {
		plan := supervision.DeliverWakeup(true, kind, nil)
		m.persistSupAudit(ctx, id, plan.Audit)
		return plan
	}
	guard := m.guard
	plan := supervision.DeliverWakeup(true, kind, func() error {
		return guard.Send(ctx, id, "supervision: orchestrator turn for "+string(kind))
	})
	if plan.Audit != "" {
		m.persistSupAudit(ctx, id, plan.Audit)
	}
	return plan
}

// SupervisionStallCheck evaluates time-since-progress for the session.
// Disabled denies closed with no side effects. Enabled: quiet allows,
// nudge sends one worker nudge through the guard, escalation records a
// durable needs-input notification for the human.
func (m *Manager) SupervisionStallCheck(ctx context.Context, id domain.SessionID, project domain.ProjectID, sinceProgress time.Duration) supervision.StallCheck {
	if !m.supervised() {
		return supervision.StallCheck{Decision: denyClosed()}
	}
	check := supervision.CheckStall(true, m.supervision.Watchdog, sinceProgress)
	if !check.Decision.Allow {
		return check
	}
	switch check.Outcome {
	case supervision.OutcomeNudge:
		if m.guard != nil {
			_ = m.guard.Send(ctx, id, "supervision: stall nudge, report progress")
		}
	case supervision.OutcomeEscalate:
		m.emitSupEscalation(ctx, id, project, "supervision: stall escalation, no progress")
	}
	return check
}

// SupervisionPlanStep enforces the round/session/fix-round budget gates
// before a session step starts. Disabled denies closed.
func (m *Manager) SupervisionPlanStep(usage supervision.Usage, isFixRound bool, humanException bool) supervision.Decision {
	if !m.supervised() {
		return denyClosed()
	}
	return supervision.PlanStep(true, m.supervision.Budget, usage, isFixRound, humanException)
}

// SupervisionAdvance enforces the chain step gate (timeouts plus the
// triage->fix human gate) before a session step starts. Disabled
// denies closed and holds the step.
func (m *Manager) SupervisionAdvance(cur supervision.ChainStep, elapsed time.Duration, human bool) supervision.ChainMove {
	if !m.supervised() {
		return supervision.ChainMove{Next: cur, Decision: denyClosed()}
	}
	return supervision.AdvanceChain(true, m.supervision.Chain, cur, elapsed, human)
}

// persistSupAudit records the wake-up failure audit durably: in the
// manager audit trail plus, when a notification sink is wired, as a
// human-visible needs-input notification.
func (m *Manager) persistSupAudit(ctx context.Context, id domain.SessionID, audit string) {
	if audit == "" {
		return
	}
	entry := "session=" + string(id) + " " + audit
	m.recordSupAudit(entry)
	m.emitSupEscalation(ctx, id, "", entry)
}

// emitSupEscalation delivers a stall/wake-up escalation to the human as
// a durable needs-input notification. Without a sink the audit trail
// above is the record; nothing is silently dropped.
func (m *Manager) emitSupEscalation(ctx context.Context, id domain.SessionID, project domain.ProjectID, detail string) {
	m.recordSupAudit("session=" + string(id) + " " + detail)
	if m.notifications == nil {
		return
	}
	_ = m.notifications.Notify(ctx, ports.NotificationIntent{
		Type:      domain.NotificationNeedsInput,
		SessionID: id,
		ProjectID: project,
		CreatedAt: m.supClock(),
	})
}

func (m *Manager) supClock() time.Time {
	if m != nil && m.clock != nil {
		return m.clock()
	}
	return time.Now().UTC()
}

func denyClosed() supervision.Decision {
	return supervision.PlanStep(false, supervision.Budget{}, supervision.Usage{}, false, false)
}

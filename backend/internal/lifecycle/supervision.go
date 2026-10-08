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
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/supervision"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/sessionguard"
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

// SupervisionAudits returns the manager-local audit trail recorded by
// the supervision wiring (one entry per wake-up failure or escalation).
// The durable counterpart of each entry is the needs-input notification
// persisted through the notification sink; this accessor is the
// repository-durable readback for the in-process trail.
func (m *Manager) SupervisionAudits() []string {
	m.supMu.Lock()
	defer m.supMu.Unlock()
	return append([]string(nil), m.supAudits...)
}

// claimSupAudit claims the event key for recording: the first claim
// wins, later duplicates of the identical event are skipped entirely
// (no trail append, no re-notify).
func (m *Manager) claimSupAudit(key string) bool {
	m.supMu.Lock()
	defer m.supMu.Unlock()
	if m.supEmitted == nil {
		m.supEmitted = map[string]struct{}{}
	}
	if _, seen := m.supEmitted[key]; seen {
		return false
	}
	m.supEmitted[key] = struct{}{}
	return true
}

func (m *Manager) appendSupAudit(entry string) {
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
// with no side effects. Enabled delivers through the guard's Deliver
// boundary (not Send: a suppressed-but-nil-error outcome must read as
// failure, never as a delivered turn) with retry; terminal failure
// records exactly one durable audit and returns it in the plan.
// A suppressed write (blocked, terminated, missing, exited, gated)
// performs no pane write: the turn is denied and audited instead.
func (m *Manager) SupervisedReportTurn(ctx context.Context, id domain.SessionID, kind supervision.ReportKind) supervision.WakeupPlan {
	if !m.supervised() {
		return supervision.WakeupPlan{Decision: denyClosed()}
	}
	if m.guard == nil {
		plan := supervision.DeliverWakeup(true, kind, nil)
		m.persistSupAudit(ctx, id, "", plan.Audit)
		return plan
	}
	guard := m.guard
	plan := supervision.DeliverWakeup(true, kind, func() error {
		outcome, err := guard.Deliver(ctx, id, "supervision: orchestrator turn for "+string(kind))
		if err != nil {
			return err
		}
		if outcome != sessionguard.Sent {
			return errors.New("supervision: wake-up suppressed (" + outcome.String() + ")")
		}
		return nil
	})
	if plan.Audit != "" {
		m.persistSupAudit(ctx, id, "", plan.Audit)
	}
	return plan
}

// SupervisionStallCheck evaluates time-since-progress for the session.
// Disabled denies closed with no side effects. Enabled: quiet allows,
// nudge sends one worker nudge through the guard's Nudge boundary
// (which refuses at waiting_input/blocked, unlike Deliver), escalation
// records a durable needs-input notification for the human. A refused
// or failed nudge writes nothing and escalates instead: the refusal is
// audited durably, so the stall is never silently idle.
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
		if m.guard == nil {
			return escalateDeniedNudge(ctx, m, id, project, "no delivery guard wired")
		}
		outcome, err := m.guard.Nudge(ctx, id, "supervision: stall nudge, report progress")
		if err != nil || outcome != sessionguard.Sent {
			reason := "nudge refused (" + outcome.String() + ")"
			if err != nil {
				reason = "nudge failed (" + err.Error() + ")"
			}
			return escalateDeniedNudge(ctx, m, id, project, reason)
		}
	case supervision.OutcomeEscalate:
		m.persistSupAudit(ctx, id, project, "supervision: stall escalation, no progress")
	}
	return check
}

// escalateDeniedNudge converts a refused/failed stall nudge into a
// durable human escalation: no pane write happened, so the human owns
// the stall. The returned check carries the escalate outcome.
func escalateDeniedNudge(ctx context.Context, m *Manager, id domain.SessionID, project domain.ProjectID, reason string) supervision.StallCheck {
	m.persistSupAudit(ctx, id, project, "supervision: stall nudge denied, escalated ("+reason+")")
	// Canonical escalate decision from the policy: the nudge window was
	// reached and the write was refused, so the human owns the stall.
	escalated := supervision.CheckStall(true, m.supervision.Watchdog, m.supervision.Watchdog.EscalateAfter)
	if !escalated.Decision.Allow || escalated.Outcome != supervision.OutcomeEscalate {
		escalated.Outcome = supervision.OutcomeEscalate
	}
	return escalated
}

// SupervisedReportTurnForWorker mandates one orchestrator turn for a
// done/checkpoint worker report. The worker session only originates the
// report: the turn always targets the project's active orchestrator,
// resolved from durable session facts with the same selection the
// report coordinator uses (newest live orchestrator). Disabled denies
// closed with no side effects. With no resolvable orchestrator the
// turn is denied and audited durably against the worker session —
// never delivered to the worker itself.
func (m *Manager) SupervisedReportTurnForWorker(ctx context.Context, worker domain.SessionID, project domain.ProjectID, kind supervision.ReportKind) supervision.WakeupPlan {
	if !m.supervised() {
		return supervision.WakeupPlan{Decision: denyClosed()}
	}
	target, ok, err := m.activeOrchestrator(ctx, project)
	if err != nil {
		plan := supervision.WakeupPlan{Decision: denyWakeup("orchestrator resolution failed: " + err.Error())}
		plan.Audit = "wakeup kind=" + string(kind) + " worker=" + string(worker) + " err=" + err.Error()
		m.persistSupAudit(ctx, worker, project, plan.Audit)
		return plan
	}
	if !ok {
		plan := supervision.WakeupPlan{Decision: denyWakeup("no active orchestrator")}
		plan.Audit = "wakeup kind=" + string(kind) + " worker=" + string(worker) + " err=no-active-orchestrator"
		m.persistSupAudit(ctx, worker, project, plan.Audit)
		return plan
	}
	return m.SupervisedReportTurn(ctx, target, kind)
}

// activeOrchestrator resolves the newest live orchestrator session for
// the project from durable facts, mirroring the report coordinator's
// selection: orchestrator kind, not terminated, agent not exited.
func (m *Manager) activeOrchestrator(ctx context.Context, project domain.ProjectID) (domain.SessionID, bool, error) {
	recs, err := m.store.ListSessions(ctx, project)
	if err != nil {
		return "", false, err
	}
	var selected domain.SessionRecord
	for _, rec := range recs {
		if rec.Kind != domain.KindOrchestrator || rec.IsTerminated || rec.Activity.State == domain.ActivityExited {
			continue
		}
		if selected.ID == "" || rec.CreatedAt.After(selected.CreatedAt) {
			selected = rec
		}
	}
	return selected.ID, selected.ID != "", nil
}

// SupervisionPlanStep enforces the round/session/fix-round budget gates
// before a session step starts. Disabled denies closed.
func (m *Manager) SupervisionPlanStep(usage supervision.Usage, isFixRound, humanException bool) supervision.Decision {
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

// persistSupAudit records exactly one audit entry per event: the
// manager audit trail entry plus, when a notification sink is wired, a
// human-visible needs-input notification carrying the same detail (the
// durable record that survives a daemon restart via the notification
// store). A notification persistence failure is folded into the same
// single entry as a notify_err suffix, never dropped. Repeated
// identical events are idempotent: the first recording wins and later
// duplicates neither append nor re-notify.
func (m *Manager) persistSupAudit(ctx context.Context, id domain.SessionID, project domain.ProjectID, detail string) {
	if detail == "" {
		return
	}
	key := "session=" + string(id) + " " + detail
	// Idempotency first: a repeated identical event neither appends
	// nor re-notifies, so the durable record stays exactly one.
	if !m.claimSupAudit(key) {
		return
	}
	entry := key
	if m.notifications != nil {
		if err := m.notifications.Notify(ctx, ports.NotificationIntent{
			Type:      domain.NotificationNeedsInput,
			SessionID: id,
			ProjectID: project,
			CreatedAt: m.supClock(),
		}); err != nil {
			entry += " notify_err=" + err.Error()
		}
	}
	m.appendSupAudit(entry)
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

// denyWakeup builds a fail-closed wake-up denial for resolution
// failures that never reach delivery.
func denyWakeup(detail string) supervision.Decision {
	return supervision.Decision{Reason: supervision.ReasonWakeupFailed, Detail: detail}
}

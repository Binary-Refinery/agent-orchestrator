package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/supervision"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// supWiringStore is the minimal session-store fake the lifecycle
// manager needs for the supervision wiring regression: sessions plus
// the notification-signature surface the guard/store touch.
type supWiringStore struct {
	sessions map[domain.SessionID]domain.SessionRecord
}

func (s *supWiringStore) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	rec, ok := s.sessions[id]
	return rec, ok, nil
}

func (s *supWiringStore) UpdateSession(_ context.Context, rec domain.SessionRecord) error {
	s.sessions[rec.ID] = rec
	return nil
}

func (s *supWiringStore) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	return domain.ProjectRecord{ID: id}, true, nil
}

func (s *supWiringStore) UpdateSessionFromActivitySignal(_ context.Context, rec domain.SessionRecord, _ int64) (bool, error) {
	s.sessions[rec.ID] = rec
	return true, nil
}

func (s *supWiringStore) ListSessions(_ context.Context, _ domain.ProjectID) ([]domain.SessionRecord, error) {
	out := make([]domain.SessionRecord, 0, len(s.sessions))
	for _, rec := range s.sessions {
		out = append(out, rec)
	}
	return out, nil
}

func (s *supWiringStore) ListPRsBySession(_ context.Context, _ domain.SessionID) ([]domain.PullRequest, error) {
	return nil, nil
}

func (s *supWiringStore) GetPR(_ context.Context, prURL string) (domain.PullRequest, bool, error) {
	return domain.PullRequest{URL: prURL}, true, nil
}

func (s *supWiringStore) ListPRReviews(_ context.Context, _ string) ([]domain.PullRequestReview, error) {
	return nil, nil
}

func (s *supWiringStore) ListPRComments(_ context.Context, _ string) ([]domain.PullRequestComment, error) {
	return nil, nil
}

func (s *supWiringStore) GetPRLastNudgeSignature(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *supWiringStore) UpdatePRLastNudgeSignature(_ context.Context, _, _ string) error {
	return nil
}

type supWiringMessenger struct {
	sends int
	ids   []domain.SessionID
	err   error
}

func (m *supWiringMessenger) Send(_ context.Context, id domain.SessionID, _ string) error {
	if m.err != nil {
		return m.err
	}
	m.sends++
	m.ids = append(m.ids, id)
	return nil
}

type supWiringNotifier struct {
	intents []ports.NotificationIntent
}

func (n *supWiringNotifier) Notify(_ context.Context, intent ports.NotificationIntent) error {
	n.intents = append(n.intents, intent)
	return nil
}

func (n *supWiringNotifier) Resolve(_ context.Context, _ ports.NotificationResolution) error {
	return nil
}

func supWiringManager(enabled bool, store *supWiringStore, msgr *supWiringMessenger, notifier *supWiringNotifier) *lifecycle.Manager {
	return lifecycle.New(store, msgr,
		lifecycle.WithNotificationSink(notifier),
		lifecycle.WithSupervision(lifecycle.SupervisionConfig{
			Enabled:  enabled,
			Watchdog: supervision.DefaultWatchdogConfig(),
			Chain:    supervision.DefaultChainTimeouts(),
			Budget:   supervision.DefaultBudget(),
		}),
	)
}

func supWiringSession(id domain.SessionID, state domain.ActivityState) domain.SessionRecord {
	return domain.SessionRecord{
		ID:        id,
		ProjectID: "mer",
		Activity:  domain.Activity{State: state, LastActivityAt: time.Now()},
	}
}

func TestSupervisionKindForReportMapping(t *testing.T) {
	for state, kind := range map[domain.ReportState]supervision.ReportKind{
		domain.ReportDone:       supervision.ReportDone,
		domain.ReportCheckpoint: supervision.ReportCheckpoint,
	} {
		got, ok := supervisionKindForReport(state)
		if !ok || got != kind {
			t.Fatalf("state %q maps to (%q, %v), want (%q, true)", state, got, ok, kind)
		}
	}
	for _, state := range []domain.ReportState{domain.ReportNeedsInput, domain.ReportStuck, ""} {
		if _, ok := supervisionKindForReport(state); ok {
			t.Fatalf("state %q must carry no wake-up mandate", state)
		}
	}
}

func TestSuperviseReportTurnReachesProductionReportPath(t *testing.T) {
	ctx := context.Background()
	store := &supWiringStore{sessions: map[domain.SessionID]domain.SessionRecord{}}
	msgr := &supWiringMessenger{}
	notifier := &supWiringNotifier{}
	lcm := supWiringManager(true, store, msgr, notifier)
	worker := domain.SessionID("sess-prod-worker")
	orch := domain.SessionID("sess-prod-orch")
	store.sessions[worker] = domain.SessionRecord{ID: worker, ProjectID: "mer", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityActive}}
	store.sessions[orch] = domain.SessionRecord{ID: orch, ProjectID: "mer", Kind: domain.KindOrchestrator, Activity: domain.Activity{State: domain.ActivityIdle}}

	// Enabled config: a done worker report on the production hook
	// mandates and delivers exactly one turn to the active
	// orchestrator — never to the reporting worker.
	plan := superviseReportTurn(ctx, lcm, domain.ReportRecord{ID: "rpt-1", SessionID: worker, ProjectID: "mer", State: domain.ReportDone})
	if !plan.Decision.Allow {
		t.Fatalf("enabled report hook plan = %+v, want delivered turn", plan)
	}
	if msgr.sends != 1 {
		t.Fatalf("enabled report hook sends = %d, want 1 orchestrator turn", msgr.sends)
	}
	if len(msgr.ids) != 1 || msgr.ids[0] != orch {
		t.Fatalf("report hook deliveries = %q, want exactly [%q]", msgr.ids, orch)
	}
	// Non-mandating states stay silent.
	plan = superviseReportTurn(ctx, lcm, domain.ReportRecord{ID: "rpt-2", SessionID: worker, ProjectID: "mer", State: domain.ReportStuck})
	if plan.Decision.Allow {
		t.Fatalf("non-mandating report hook plan = %+v, want closed deny", plan)
	}
	if msgr.sends != 1 {
		t.Fatalf("non-mandating report hook sends = %d, want still 1", msgr.sends)
	}
	// Enabled stall path: watchdog escalation reaches the human durably.
	check := lcm.SupervisionStallCheck(ctx, orch, "mer", 24*time.Hour)
	if check.Outcome != supervision.OutcomeEscalate {
		t.Fatalf("stall check = %+v, want escalation", check)
	}
	if len(notifier.intents) == 0 {
		t.Fatal("escalation must persist a durable human notification")
	}
}

func TestSuperviseReportTurnDisabledIsSideEffectFree(t *testing.T) {
	ctx := context.Background()
	store := &supWiringStore{sessions: map[domain.SessionID]domain.SessionRecord{}}
	msgr := &supWiringMessenger{}
	notifier := &supWiringNotifier{}
	lcm := supWiringManager(false, store, msgr, notifier)
	id := domain.SessionID("sess-off")
	store.sessions[id] = supWiringSession(id, domain.ActivityIdle)

	superviseReportTurn(ctx, lcm, domain.ReportRecord{ID: "rpt-1", SessionID: id, ProjectID: "mer", State: domain.ReportDone})
	superviseReportTurn(ctx, lcm, domain.ReportRecord{ID: "rpt-2", SessionID: id, ProjectID: "mer", State: domain.ReportCheckpoint})
	if check := lcm.SupervisionStallCheck(ctx, id, "mer", 24*time.Hour); check.Decision.Allow {
		t.Fatalf("disabled stall check allowed: %+v", check)
	}
	if msgr.sends != 0 || len(notifier.intents) != 0 || len(lcm.SupervisionAudits()) != 0 {
		t.Fatalf("disabled supervision must be side-effect free: sends=%d intents=%d audits=%d",
			msgr.sends, len(notifier.intents), len(lcm.SupervisionAudits()))
	}
	// Nil-manager hook never blocks report delivery.
	superviseReportTurn(ctx, nil, domain.ReportRecord{ID: "rpt-3", SessionID: id, State: domain.ReportDone})
}

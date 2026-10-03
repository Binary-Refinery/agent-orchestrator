package daemon

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	automationobserver "github.com/aoagents/agent-orchestrator/backend/internal/observe/automations"
	automationsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/automation"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// startAutomations repairs crash-interrupted durable state before launching the
// cadence-only observer. Reconciliation is best effort so one malformed legacy
// row cannot prevent daemon readiness.
func startAutomations(ctx context.Context, store *sqlite.Store, sessions *sessionsvc.Service, logger *slog.Logger) (*automationsvc.Service, <-chan struct{}) {
	service := automationsvc.New(automationsvc.Deps{Store: store, Spawner: sessions, Policy: automationPolicyFromEnv()})
	if err := service.Reconcile(ctx); err != nil {
		logger.Warn("automation startup reconciliation completed with errors", "err", err)
	}
	observer := automationobserver.New(service, automationobserver.Config{Logger: logger})
	return service, observer.Start(ctx)
}

// automationPolicyFromEnv builds the opt-in automation guardrails. Every
// switch defaults to off; set an AO_AUTOMATION_* variable to "1" or "true"
// to enable it. The reviewer budget is always capped at 1.
func automationPolicyFromEnv() domain.AutomationPolicy {
	policy := domain.DefaultAutomationPolicy()
	if envEnabled(os.Getenv("AO_AUTOMATION_SPAWN_GATE")) {
		policy.SpawnGate = true
	}
	if envEnabled(os.Getenv("AO_AUTOMATION_REVIEWER_ARCHIVE")) {
		policy.ReviewerArchive = true
	}
	if envEnabled(os.Getenv("AO_AUTOMATION_DRAFT_ONLY_REPORTS")) {
		policy.DraftOnlyReports = true
	}
	if envEnabled(os.Getenv("AO_AUTOMATION_AUTO_REVIEWER")) {
		policy.AutoReviewer = true
	}
	return policy
}

func envEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

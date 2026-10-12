package daemon

import (
	"context"
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/opencodeeffort"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/supervision"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

// The allowlist reports effective switches from the process that starts the
// runtime. Never log the full environment, policy contents, paths or credentials.
func instructorRuntimeReadback(getenv func(string) string, appVersion, policyVersion string, instructorEnabled bool, build *debug.BuildInfo) []any {
	daemonVersion, buildRevision, buildModified := "unknown", "unknown", "unknown"
	if build != nil {
		if build.Main.Version != "" {
			daemonVersion = build.Main.Version
		}
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				buildRevision = setting.Value
			case "vcs.modified":
				buildModified = setting.Value
			}
		}
	}
	return []any{
		"pid", os.Getpid(), "appVersion", appVersion, "daemonVersion", daemonVersion,
		"buildRevision", buildRevision, "buildModified", buildModified,
		"instructor", instructorEnabled,
		"supervision", supervision.EnabledFromEnv(getenv("AO_SUPERVISION")),
		"openCodeEffortFloor", opencodeeffort.EnabledFromEnv(getenv("AO_OPENCODE_EFFORT_FLOOR")),
		"reportSyncRequested", envEnabled(getenv("AO_REPORT_SYNC")),
		// The legacy flag has no production caller. Requested is deliberately
		// separate from runtime state until a validated adapter is implemented.
		"reportSyncRuntime", "unsupported",
		"reactionLoopRequested", envEnabled(getenv("AO_REACTION_LOOP")),
		"reactionLoopRuntime", "unsupported",
		"triageGateRequested", envEnabled(getenv("AO_TRIAGE_GATE")),
		"triageGateRuntime", "unsupported",
		"policyVersion", policyVersion,
	}
}

func logInstructorRuntimeReadback(ctx context.Context, log *slog.Logger, cfg config.Config, enabled bool, svc *instructorsvc.Service) {
	policyVersion := "inactive"
	if enabled {
		policy, err := svc.GetPolicy(ctx)
		policyVersion = "unavailable"
		if err == nil {
			policyVersion = "invalid"
			if instructorsvc.ValidatePolicy(policy) == nil && instructorsvc.ValidateLockedPolicy(policy) == nil {
				policyVersion = instructorsvc.PolicyVersion(policy)
			}
		}
	}
	build, _ := debug.ReadBuildInfo()
	log.Info("Instructor runtime readback", instructorRuntimeReadback(os.Getenv, cfg.Telemetry.AppVersion, policyVersion, enabled, build)...)
}

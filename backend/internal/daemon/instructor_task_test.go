package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestGovernedWorkerRequiresTaskBeforeWorkRegistration(t *testing.T) {
	// No service or store is installed: rejection must precede even looking up
	// or registering work, regardless of role, standing rules, or attachments.
	guard := instructorGovernance{}
	for _, role := range []string{"", "worker", "reviewer", "triage"} {
		for _, workID := range []string{"", "existing-work-with-a-goal"} {
			for _, prompt := range []string{"", " \t\n"} {
				_, err := guard.PrepareLaunch(context.Background(), ports.SpawnConfig{
					Kind: domain.KindWorker, WorkID: workID, WorkRole: role, Prompt: prompt,
					Attachments: []ports.SpawnAttachment{{Ext: ".txt", Data: []byte("context")}},
				}, domain.ProjectRecord{Config: domain.ProjectConfig{AgentRules: "Run the tests"}})
				if !errors.Is(err, ports.ErrGovernedTaskRequired) {
					t.Fatalf("role=%q work=%q prompt=%q: %v", role, workID, prompt, err)
				}
			}
		}
	}
}

func TestGovernedIssueTaskKeepsRegistrationGoalIndependentOfFetchedContext(t *testing.T) {
	for _, prompt := range []string{"", " \t\n", "Fix only the parser boundary"} {
		t.Run(prompt, func(t *testing.T) {
			ctx := context.Background()
			store := sqlitetest.MustOpen(t)
			project := domain.ProjectRecord{ID: "task-project", Path: t.TempDir()}
			if err := store.UpsertProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			policy := instructorsvc.DefaultPolicy()
			role := policy.Roles["worker"]
			role.EffortByModel = map[string]string{"provider/worker": "high"}
			policy.Roles["worker"] = role
			policy.Version = instructorsvc.PolicyVersion(policy)
			if err := store.PutInstructorPolicy(ctx, policy); err != nil {
				t.Fatal(err)
			}
			validator := &exactInstructorRoleValidator{}
			service := instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: validator})
			guard := instructorGovernance{svc: service, store: store, validator: validator}
			cfg := ports.SpawnConfig{
				ProjectID: "task-project", Kind: domain.KindWorker, Harness: domain.HarnessOpenCode,
				Prompt: prompt, IssueID: "117", ResolvedTaskPrompt: "Work on issue 117.\n" + strings.Repeat("issue context ", 1000),
				AgentConfig: ports.AgentConfig{Model: "provider/worker", Effort: "high"},
			}
			workID, err := guard.PrepareLaunch(ctx, cfg, project)
			if err != nil {
				t.Fatalf("valid issue task refused: %v", err)
			}
			work, found, err := store.GetInstructorWork(ctx, workID)
			if err != nil || !found || strings.TrimSpace(work.Goal) == "" || strings.Contains(work.Goal, "issue context") {
				t.Fatalf("registration goal was replaced by fetched task context: %+v, %v", work, err)
			}
			if strings.TrimSpace(prompt) != "" && work.Goal != prompt {
				t.Fatalf("explicit Work goal changed: %q", work.Goal)
			}
		})
	}
}

func TestWorkerCannotUseControlRoleToBypassTaskAdmission(t *testing.T) {
	guard := instructorGovernance{}
	for _, role := range []string{"orchestrator", "instructor"} {
		for _, prompt := range []string{"", "A real task"} {
			_, err := guard.PrepareLaunch(context.Background(), ports.SpawnConfig{
				Kind: domain.KindWorker, WorkRole: role, Prompt: prompt,
			}, domain.ProjectRecord{})
			if !errors.Is(err, instructorsvc.ErrValidation) {
				t.Fatalf("role=%s prompt=%q bypassed kind boundary: %v", role, prompt, err)
			}
		}
	}
}

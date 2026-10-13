package daemon

import (
	"context"
	"fmt"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type selectedProjectValidator struct{}

func (selectedProjectValidator) ValidateInstructorAgentConfig(_ context.Context, _ domain.ProjectID, role domain.InstructorRole, harness, model, effort string) error {
	if role.Harness != harness || role.Model != model || role.Effort != "configured" && role.Effort != effort {
		return fmt.Errorf("native selection does not match project role")
	}
	return nil
}

func TestProjectOrchestratorUsesCanonicalSettingsWithoutRequiringWorkerSetup(t *testing.T) {
	ctx := context.Background()
	project := domain.ProjectRecord{ID: "project", Path: t.TempDir(), Config: domain.ProjectConfig{
		Orchestrator: domain.RoleOverride{Harness: domain.HarnessOpenCode, AgentConfig: domain.AgentConfig{Model: "provider/control", Effort: "high"}},
	}}
	store := unboundControlStore{instructorTestStore: instructorTestStore{projects: []domain.ProjectRecord{project}, sessions: []domain.SessionRecord{{ID: "control", ProjectID: "project", Kind: domain.KindOrchestrator, Harness: domain.HarnessOpenCode, Metadata: domain.SessionMetadata{Model: "provider/control", Effort: "high"}}}}}
	validator := selectedProjectValidator{}
	svc := instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: validator, ProjectRoleDefaults: true})
	guard := instructorGovernance{svc: svc, store: store, validator: validator}
	input := ports.SpawnConfig{ProjectID: "project", Kind: domain.KindOrchestrator, Harness: domain.HarnessOpenCode, AgentConfig: project.Config.Orchestrator.AgentConfig}
	if work, err := guard.PrepareLaunch(ctx, input, project); err != nil || work != "" {
		t.Fatalf("project-sourced control role cannot spawn independently: %s %v", work, err)
	}
	if err := guard.ValidateResume(ctx, "control"); err != nil {
		t.Fatalf("project-sourced control role cannot resume/restore: %v", err)
	}
	store.projects[0].Config.Orchestrator.AgentConfig.Model = "next-provider-control"
	guard.store = store
	guard.svc = instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: validator, ProjectRoleDefaults: true})
	if err := guard.ValidateResume(ctx, "control"); err != nil {
		t.Fatalf("new project defaults stranded existing conversation: %v", err)
	}
	if err := guard.ValidateControllerRecovery(ctx, "control"); err != nil {
		t.Fatalf("new project defaults stranded controller recovery: %v", err)
	}
	if err := guard.ValidateChatAgentConfig(ctx, "control", "provider/control", "high"); err != nil {
		t.Fatalf("new defaults blocked the existing conversation's next turn: %v", err)
	}
	if err := guard.ValidateChatAgentConfig(ctx, "control", "unapproved-control", "high"); err == nil {
		t.Fatal("preserving the existing selection authorized an unrelated new model")
	}
	input.Harness = domain.HarnessCodex
	if _, err := guard.PrepareLaunch(ctx, input, project); err == nil {
		t.Fatal("spawn override bypassed canonical project selection")
	}
}

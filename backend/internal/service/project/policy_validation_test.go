package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/project"
)

func TestProjectPolicyConflictRejectsSettingsBeforePersistence(t *testing.T) {
	ctx := context.Background()
	m := newManager(t).(*project.Service)
	_, err := m.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("pilot"), Name: ptr("Original")})
	if err != nil {
		t.Fatal(err)
	}
	m.SetGovernanceValidator(func(_ context.Context, row domain.ProjectRecord) error {
		if row.Config.Orchestrator.Harness != domain.HarnessOpenCode {
			t.Fatalf("validator did not receive candidate config: %+v", row.Config)
		}
		return errors.New("orchestrator policy requires codex; project settings select opencode")
	})
	cfg := domain.ProjectConfig{Orchestrator: domain.RoleOverride{Harness: domain.HarnessOpenCode}}
	_, err = m.SetConfig(ctx, "pilot", project.SetConfigInput{Config: cfg})
	wantCode(t, err, "PROJECT_POLICY_CONFLICT")
	_, err = m.UpdateSettings(ctx, "pilot", project.UpdateSettingsInput{DisplayName: "Rejected", Config: cfg})
	wantCode(t, err, "PROJECT_POLICY_CONFLICT")
	got, err := m.Get(ctx, "pilot")
	if err != nil || got.Project == nil || got.Project.Name != "Original" || got.Project.Config != nil && got.Project.Config.Orchestrator.Harness != "" {
		t.Fatalf("rejected settings changed durable project: %+v, %v", got, err)
	}
	_, err = m.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("rejected"), Config: &cfg})
	wantCode(t, err, "PROJECT_POLICY_CONFLICT")
	_, err = m.Get(ctx, "rejected")
	wantCode(t, err, "PROJECT_NOT_FOUND")
}

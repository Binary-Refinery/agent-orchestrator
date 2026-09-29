package project_test

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/project"
)

func TestGovernanceModeIsChosenOnlyAtRegistration(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "managed"}[managed], func(t *testing.T) {
			ctx := context.Background()
			m := newManager(t)
			cfg := domain.ProjectConfig{GovernanceManaged: managed}
			_, err := m.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("pilot"), Config: &cfg})
			if err != nil {
				t.Fatal(err)
			}
			opposite := domain.ProjectConfig{GovernanceManaged: !managed}
			_, err = m.SetConfig(ctx, "pilot", project.SetConfigInput{Config: opposite})
			wantCode(t, err, "MANAGED_MODE_LOCKED")
			_, err = m.UpdateSettings(ctx, "pilot", project.UpdateSettingsInput{DisplayName: "Pilot", Config: opposite})
			wantCode(t, err, "MANAGED_MODE_LOCKED")
			got, err := m.Get(ctx, "pilot")
			if err != nil || got.Status != "ok" || got.Project == nil {
				t.Fatalf("Get failed: status=%q, error=%v", got.Status, err)
			}
			gotManaged := got.Project.Config != nil && got.Project.Config.GovernanceManaged
			if gotManaged != managed {
				t.Fatalf("mode changed: got %v, want %v", gotManaged, managed)
			}
			if managed {
				_, err = m.Remove(ctx, "pilot")
				wantCode(t, err, "MANAGED_MODE_LOCKED")
			}
		})
	}
}

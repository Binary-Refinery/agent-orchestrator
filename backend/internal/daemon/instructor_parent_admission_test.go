package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type instructorParentValidator struct{ calls int }

func (v *instructorParentValidator) ValidateInstructorAgentConfig(context.Context, domain.ProjectID, domain.InstructorRole, string, string, string) error {
	v.calls++
	return nil
}

func TestInstructorNativeParentCannotUseProjectOrchestratorShortcut(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitetest.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	now := time.Now()
	project := domain.ProjectRecord{ID: "parent-guard", Path: t.TempDir(), RegisteredAt: now}
	projectID := domain.ProjectID(project.ID)
	if err = store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	validator := &instructorParentValidator{}
	service := instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: validator})
	policy := instructorsvc.DefaultPolicy()
	workerRole := policy.Roles["worker"]
	workerRole.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = workerRole
	work, err := service.Register(ctx, instructorsvc.RegisterInput{ProjectID: projectID, Name: "Native delegation", Goal: "Keep delegated work governed", Source: "manual", SourceRef: "native-parent", Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: projectID, Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, Metadata: domain.SessionMetadata{Model: "provider/worker", Effort: "high"}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AttachSession(ctx, work.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	guard := instructorGovernance{svc: service, store: store, validator: validator}
	control := ports.SpawnConfig{ProjectID: projectID, ParentSessionID: parent.ID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, AgentConfig: ports.AgentConfig{Model: "provider/control", Effort: "high"}}
	if _, err = guard.PrepareLaunch(ctx, control, project); !errors.Is(err, instructorsvc.ErrValidation) || validator.calls != 0 {
		t.Fatalf("managed worker bypassed parent admission through unbound orchestrator shortcut: err=%v calls=%d", err, validator.calls)
	}
	control.WorkID, control.WorkRole = work.ID, "worker"
	if _, err = guard.PrepareLaunch(ctx, control, project); !errors.Is(err, instructorsvc.ErrValidation) || validator.calls != 0 {
		t.Fatalf("native orchestrator kind disguised as worker role: err=%v calls=%d", err, validator.calls)
	}
	child := ports.SpawnConfig{ProjectID: projectID, ParentSessionID: parent.ID, Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, AgentConfig: ports.AgentConfig{Model: "provider/worker", Effort: "high"}}
	if childWork, childErr := guard.PrepareLaunch(ctx, child, project); childErr != nil || childWork != work.ID {
		t.Fatalf("approved ordinary child lost parent work: work=%s err=%v", childWork, childErr)
	}
	control.ParentSessionID, control.WorkID, control.WorkRole = "", "", ""
	if id, launchErr := guard.PrepareLaunch(ctx, control, project); launchErr != nil || id != "" {
		t.Fatalf("genuine operator-started project control changed: work=%s err=%v", id, launchErr)
	}
	controlParent, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: projectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Metadata: domain.SessionMetadata{Model: "provider/control", Effort: "high"}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AttachSession(ctx, work.ID, controlParent.ID); err != nil {
		t.Fatal(err)
	}
	control.ParentSessionID = controlParent.ID
	if id, launchErr := guard.PrepareLaunch(ctx, control, project); launchErr != nil || id != "" {
		t.Fatalf("actual control parent's independent launch changed: work=%s err=%v", id, launchErr)
	}
	current, _, err := store.GetInstructorWork(ctx, work.ID)
	if err != nil || len(current.Launches) != 0 {
		t.Fatalf("native parent admission fabricated launch evidence: %+v err=%v", current.Launches, err)
	}
}

package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type exactInstructorRoleValidator struct{ roles []string }

func (v *exactInstructorRoleValidator) ValidateInstructorAgentConfig(_ context.Context, _ domain.ProjectID, role domain.InstructorRole, harness, model, effort string) error {
	v.roles = append(v.roles, role.Harness)
	if role.Harness != harness {
		return fmt.Errorf("%w: role requires %s", ports.ErrUnsupportedEffort, role.Harness)
	}
	if role.Model != "" && role.Model != model {
		return ports.ErrUnsupportedEffort
	}
	expected := role.Effort
	if expected == "maximum" {
		expected = role.EffortByModel[model]
	}
	if expected != "configured" && expected != effort {
		return ports.ErrUnsupportedEffort
	}
	return nil
}

func TestInstructorGovernanceReusedIDCannotInheritOldOrchestratorRole(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpen(t)
	now := time.Now().UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "role-project", Path: t.TempDir(), RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	validator := &exactInstructorRoleValidator{}
	service := instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: validator})
	policy := instructorsvc.DefaultPolicy()
	workerRole := policy.Roles["worker"]
	workerRole.Model = "provider/worker"
	workerRole.EffortByModel = map[string]string{"provider/worker": "max"}
	policy.Roles["worker"] = workerRole
	work, err := service.Register(ctx, instructorsvc.RegisterInput{ProjectID: "role-project", Name: "Role identity", Goal: "Preserve current worker authority", Source: "manual", SourceRef: "role-reuse", Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: work.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Metadata: domain.SessionMetadata{Model: "provider/control", Effort: "minimal"}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AttachSession(ctx, work.ID, old.ID); err != nil {
		t.Fatal(err)
	}
	control := domain.InstructorAgentSelection{Harness: "codex", Model: "provider/control", Effort: "minimal"}
	if err := service.RecordLaunch(ctx, work.ID, domain.InstructorLaunch{SessionID: old.ID, Role: "orchestrator", Requested: control, Sent: &control, Status: "launched"}); err != nil {
		t.Fatal(err)
	}
	oldInstance, _, err := store.InstructorSessionInstance(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.DeleteSession(ctx, old.ID); err != nil || !deleted {
		t.Fatalf("delete old control: %v %v", deleted, err)
	}
	worker, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: work.ProjectID, Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, Metadata: domain.SessionMetadata{Model: "provider/worker", Effort: "max"}, CreatedAt: now, UpdatedAt: now})
	if err != nil || worker.ID != old.ID {
		t.Fatalf("replacement worker: %+v %v", worker, err)
	}
	if err := service.AttachSession(ctx, work.ID, worker.ID); err != nil {
		t.Fatal(err)
	}
	newInstance, _, err := store.InstructorSessionInstance(ctx, worker.ID)
	if err != nil || newInstance == oldInstance {
		t.Fatalf("instance old=%q new=%q err=%v", oldInstance, newInstance, err)
	}
	guard := instructorGovernance{svc: service, store: store, validator: validator}
	// A readmitted worker has no launch proof yet. Its actual persisted kind
	// determines admission; an old same-ID control launch is not authority.
	// The pinned role check now rejects before native capability validation.
	nativeValidations := len(validator.roles)
	if err := guard.ValidateAgentChange(ctx, worker.ID, domain.HarnessCodex, ports.AgentConfig{Model: "provider/control", Effort: "minimal"}); !errors.Is(err, instructorsvc.ErrValidation) || !strings.Contains(err.Error(), "role worker requires harness opencode") {
		t.Fatalf("old Codex/minimal control role was accepted for worker: %v", err)
	}
	if len(validator.roles) != nativeValidations {
		t.Fatal("rejected predecessor role reached native capability validation")
	}
	if err := guard.ValidateResume(ctx, worker.ID); err != nil {
		t.Fatalf("valid worker resume used the old control role: %v", err)
	}
	if err := guard.ValidateAgentChange(ctx, worker.ID, domain.HarnessOpenCode, ports.AgentConfig{Model: "provider/worker", Effort: "max"}); err != nil {
		t.Fatalf("valid current worker settings rejected: %v", err)
	}
	if err := guard.ObserveChatAgentConfig(ctx, worker.ID, "provider/worker", "max", "native-config-reported"); err != nil {
		t.Fatal(err)
	}
	current, _, err := store.GetInstructorWork(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	launch := current.Launches[len(current.Launches)-1]
	if launch.Role != "worker" || launch.SessionInstanceID != newInstance || launch.Requested.Harness != "opencode" {
		t.Fatalf("observer transferred old role authority: %+v", launch)
	}
	// Future resume after native observation must still select the worker role.
	if err := guard.ValidateResume(ctx, worker.ID); err != nil {
		t.Fatalf("observation poisoned future worker admission: %v", err)
	}
}

func TestInstructorGovernedReviewerScopeCannotDowngradeAfterOwnerIDReuse(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpen(t)
	now := time.Now().UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "review-scope-owner", Path: t.TempDir(), RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	validator := &exactInstructorRoleValidator{}
	service := instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: validator})
	work, err := service.Register(ctx, instructorsvc.RegisterInput{ProjectID: "review-scope-owner", Name: "Retained reviewer scope", Goal: "Never downgrade a captured governed scope", Source: "manual", SourceRef: "reviewer-orphan"})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: work.ProjectID, Kind: domain.KindWorker, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AttachSession(ctx, work.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	spec := reviewcore.LaunchSpec{WorkerID: owner.ID, ProjectID: work.ProjectID, ScopeKey: instructorReviewScope(work), Harness: domain.ReviewerCodex}
	if deleted, err := store.DeleteSession(ctx, owner.ID); err != nil || !deleted {
		t.Fatalf("delete original reviewer owner: %v %v", deleted, err)
	}
	replacement, err := store.CreateSession(ctx, owner)
	if err != nil || replacement.ID != owner.ID {
		t.Fatalf("reuse owner display ID: %+v %v", replacement, err)
	}
	if _, managed, err := service.WorkForSession(ctx, replacement.ID); err != nil || managed {
		t.Fatalf("replacement unexpectedly admitted: %v %v", managed, err)
	}
	guard := instructorGovernance{svc: service, store: store, validator: validator}
	callbacks := []struct {
		name string
		call func(context.Context, reviewcore.LaunchSpec) error
	}{{"launch", guard.ValidateReviewerLaunch}, {"reuse", guard.ValidateReviewerReuse}, {"record-after-start", guard.RecordReviewerLaunch}}
	for _, callback := range callbacks {
		if err := callback.call(ctx, spec); !errors.Is(err, instructorsvc.ErrConflict) {
			t.Fatalf("%s downgraded captured governance to unmanaged: %v", callback.name, err)
		}
		unassigned := spec
		unassigned.ScopeKey = ""
		if err := callback.call(ctx, unassigned); err != nil {
			t.Fatalf("%s changed genuine unmanaged review behavior: %v", callback.name, err)
		}
	}
	if len(validator.roles) != 0 {
		t.Fatalf("orphan scope reached native role validation: %+v", validator.roles)
	}
}

type replacingInstructorRoleStore struct {
	*sqlite.Store
	afterAtomicRead func()
	replaced        bool
}

func (s *replacingInstructorRoleStore) InstructorSessionForMembership(ctx context.Context, id domain.SessionID) (domain.SessionRecord, string, bool, error) {
	rec, instance, found, err := s.Store.InstructorSessionForMembership(ctx, id)
	if !s.replaced && found && err == nil {
		s.replaced = true
		s.afterAtomicRead()
	}
	return rec, instance, found, err
}

func TestInstructorGovernanceRoleReadFencesConcurrentSameWorkReadmission(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpen(t)
	now := time.Now().UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "concurrent-role", Path: t.TempDir(), RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	validator := &exactInstructorRoleValidator{}
	service := instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: validator})
	policy := instructorsvc.DefaultPolicy()
	workerRole := policy.Roles["worker"]
	workerRole.Model = "provider/worker"
	workerRole.EffortByModel = map[string]string{"provider/worker": "max"}
	policy.Roles["worker"] = workerRole
	work, err := service.Register(ctx, instructorsvc.RegisterInput{ProjectID: "concurrent-role", Name: "Concurrent role admission", Goal: "Fence the captured work and row together", Source: "manual", SourceRef: "concurrent-role", Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: work.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Metadata: domain.SessionMetadata{Model: "provider/control", Effort: "minimal"}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AttachSession(ctx, work.ID, old.ID); err != nil {
		t.Fatal(err)
	}
	selection := domain.InstructorAgentSelection{Harness: "codex", Model: "provider/control", Effort: "minimal"}
	if err := service.RecordLaunch(ctx, work.ID, domain.InstructorLaunch{SessionID: old.ID, Role: "orchestrator", Requested: selection, Sent: &selection, Status: "launched"}); err != nil {
		t.Fatal(err)
	}
	replacementStore := &replacingInstructorRoleStore{Store: store}
	replacementStore.afterAtomicRead = func() {
		if deleted, err := store.DeleteSession(ctx, old.ID); err != nil || !deleted {
			t.Fatalf("delete captured role row: %v %v", deleted, err)
		}
		replacement, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: work.ProjectID, Kind: domain.KindWorker, Harness: domain.HarnessOpenCode, Metadata: domain.SessionMetadata{Model: "provider/worker", Effort: "max"}, CreatedAt: now, UpdatedAt: now})
		if err != nil || replacement.ID != old.ID {
			t.Fatalf("replace captured role row: %+v %v", replacement, err)
		}
		if err := service.AttachSession(ctx, work.ID, replacement.ID); err != nil {
			t.Fatal(err)
		}
	}
	guard := instructorGovernance{svc: service, store: replacementStore, validator: validator}
	// The old row and instance pair was read atomically, but the same work's
	// binding changed immediately afterwards. A current WorkID alone is not proof
	// that the caller's captured launch history belongs to the admitted row.
	err = guard.ValidateAgentChange(ctx, old.ID, domain.HarnessCodex, ports.AgentConfig{Model: "provider/control", Effort: "minimal"})
	if !replacementStore.replaced || !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("concurrent same-work readmission reused old control authority: replaced=%v err=%v", replacementStore.replaced, err)
	}
}

func TestInstructorGovernanceRoleReadAllowsUnrelatedWorkProgress(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpen(t)
	now := time.Now().UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "role-progress", Path: t.TempDir(), RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	validator := &exactInstructorRoleValidator{}
	service := instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: validator})
	work, err := service.Register(ctx, instructorsvc.RegisterInput{ProjectID: "role-progress", Name: "Role with parallel progress", Goal: "Keep native role checks independent of unrelated progress", Source: "manual", SourceRef: "role-progress"})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: work.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Metadata: domain.SessionMetadata{Model: "provider/control", Effort: "minimal"}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AttachSession(ctx, work.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	observed := &replacingInstructorRoleStore{Store: store}
	observed.afterAtomicRead = func() {
		current, found, err := store.GetInstructorWork(ctx, work.ID)
		if err != nil || !found {
			t.Fatalf("read work progress: %v %v", found, err)
		}
		expected := current.Revision
		current.Revision++
		current.Summary = "Another agent reported progress"
		if updated, err := store.SaveInstructorWork(ctx, current, expected, domain.InstructorEvent{ID: "role-progress-update", WorkID: work.ID, Kind: "progress", At: now}); err != nil || !updated {
			t.Fatalf("save unrelated work progress: %v %v", updated, err)
		}
	}
	guard := instructorGovernance{svc: service, store: observed, validator: validator}
	if err := guard.ValidateAgentChange(ctx, owner.ID, domain.HarnessCodex, ports.AgentConfig{Model: "provider/control", Effort: "minimal"}); err != nil {
		t.Fatalf("unrelated work progress introduced an unnecessary role gate: %v", err)
	}
	if !observed.replaced {
		t.Fatal("concurrent progress was not injected")
	}
}

package instructor_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type projectNativeValidator struct{ err error }

func (v projectNativeValidator) ValidateInstructorAgentConfig(context.Context, domain.ProjectID, domain.InstructorRole, string, string, string) error {
	return v.err
}

func projectRoleFixture(t *testing.T) (*instructorFixture, domain.ProjectRecord, domain.InstructorPolicy) {
	t.Helper()
	f := newInstructorFixture(t)
	project, _, err := f.store.GetProject(context.Background(), "governed")
	if err != nil {
		t.Fatal(err)
	}
	project.Config = domain.ProjectConfig{
		Worker:       domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Model: "worker-model", Effort: "high"}},
		Orchestrator: domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Model: "control-model", Effort: "max"}},
		Reviewers:    []domain.ReviewerConfig{{Harness: domain.ReviewerOpenCode, AgentConfig: domain.AgentConfig{Model: "review-model", Effort: "ultra"}}},
	}
	if err := f.store.UpsertProject(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, RoleValidator: projectNativeValidator{}, ProjectRoleDefaults: true})
	p := instructorsvc.DefaultProjectPolicy()
	for role, mapping := range map[string]map[string]string{"worker": {"worker-model": "high"}, "reviewer": {"review-model": "ultra"}} {
		r := p.Roles[role]
		r.EffortByModel = mapping
		p.Roles[role] = r
	}
	p.Version = ""
	return f, project, p
}

func TestProjectRolesPinChoicesAndKeepExistingOriginAfterSettingsChange(t *testing.T) {
	ctx := context.Background()
	f, project, p := projectRoleFixture(t)
	if _, err := f.service.SetPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	in := instructorsvc.RegisterInput{ProjectID: "governed", Name: "pinned project roles", Goal: "Retain identity", Source: "manual", SourceRef: "stable"}
	first, err := f.service.Register(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if first.Policy.Roles["worker"].Harness != "codex" || first.Policy.Roles["reviewer"].Harness != "opencode" || first.Policy.Roles["orchestrator"].Effort != "max" {
		t.Fatalf("did not pin project choices: %+v", first.Policy.Roles)
	}
	if p.Roles["worker"].Harness != "" {
		t.Fatal("resolving a work mutated the policy template")
	}
	project.Config.Orchestrator.AgentConfig.Model = "next-control-model"
	if err := f.store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Register(ctx, in)
	if err != nil || second.ID != first.ID || second.PolicyVersion != first.PolicyVersion || second.Policy.Roles["orchestrator"].Model != "control-model" {
		t.Fatalf("existing work changed on repeat registration: %+v, %v", second, err)
	}
	in.Name, in.SourceRef = "new configuration", "new"
	next, err := f.service.Register(ctx, in)
	if err != nil || next.PolicyVersion == first.PolicyVersion || next.Policy.Roles["orchestrator"].Model != "next-control-model" {
		t.Fatalf("new work did not consume current project configuration: %+v, %v", next, err)
	}
}

func TestProjectPolicyNativeValidationAndMaximumApprovalFailClosed(t *testing.T) {
	for _, failure := range []string{"mapping", "capability", "missing validator", "reviewer independence"} {
		t.Run(failure, func(t *testing.T) {
			f, _, p := projectRoleFixture(t)
			switch failure {
			case "mapping":
				r := p.Roles["worker"]
				r.EffortByModel = nil
				p.Roles["worker"] = r
			case "capability":
				f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, RoleValidator: projectNativeValidator{err: errors.New("native effort unsupported")}})
			case "missing validator":
				f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store})
			case "reviewer independence":
				r := p.Roles["reviewer"]
				r.Independent = false
				p.Roles["reviewer"] = r
			}
			_, err := f.service.Register(context.Background(), instructorsvc.RegisterInput{ProjectID: "governed", Name: "invalid", Goal: "Do not launch", Policy: p})
			if !errors.Is(err, instructorsvc.ErrValidation) {
				t.Fatalf("expected actionable validation error, got %v", err)
			}
			works, err := f.store.ListInstructorWorks(context.Background())
			if err != nil || len(works) != 0 {
				t.Fatalf("invalid selection created a work: %+v, %v", works, err)
			}
		})
	}
}

func TestProjectPolicyRepositoryPrecedenceAndLegacyConflict(t *testing.T) {
	ctx := context.Background()
	f, project, p := projectRoleFixture(t)
	if _, err := f.service.SetPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	repoPolicy := p
	repoPolicy.Roles = map[string]domain.InstructorRole{}
	for name, role := range p.Roles {
		repoPolicy.Roles[name] = role
	}
	role := repoPolicy.Roles["orchestrator"]
	role.SelectionSource, role.Harness = "", "opencode"
	repoPolicy.Roles["orchestrator"] = role
	data, _ := json.Marshal(repoPolicy)
	if err := os.MkdirAll(filepath.Join(project.Path, ".ao"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.Path, ".ao", "governance.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	_, source, err := f.service.ResolveProjectPolicy(ctx, "governed")
	if !errors.Is(err, instructorsvc.ErrValidation) || !strings.Contains(err.Error(), "policy requires harness opencode") || !strings.HasPrefix(source, "repository:.ao/governance.json") {
		t.Fatalf("expected repository conflict and provenance, source=%s err=%v", source, err)
	}
	if err := f.service.ValidateProjectConfiguration(ctx, project); !errors.Is(err, instructorsvc.ErrValidation) {
		t.Fatalf("contradictory project settings accepted: %v", err)
	}
}

func TestGlobalPolicyCannotIntroduceProjectHarnessConflict(t *testing.T) {
	ctx := context.Background()
	f, _, p := projectRoleFixture(t)
	initial, err := f.service.SetPolicy(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	role := initial.Roles["orchestrator"]
	role.SelectionSource, role.Harness = "", "opencode"
	initial.Roles["orchestrator"] = role
	if _, err := f.service.SetPolicy(ctx, initial); !errors.Is(err, instructorsvc.ErrValidation) {
		t.Fatalf("policy/project contradiction saved: %v", err)
	}
	readback, err := f.service.GetPolicy(ctx)
	if err != nil || readback.Roles["orchestrator"].SelectionSource != "project" {
		t.Fatalf("rejected policy changed durable settings: %+v, %v", readback, err)
	}
}

func TestLegacyRoleJSONRemainsByteCompatible(t *testing.T) {
	const legacy = `{"harness":"codex","model":"old-model","effort":"maximum","independent":true,"effortByModel":{"old-model":"high"}}`
	var role domain.InstructorRole
	if err := json.Unmarshal([]byte(legacy), &role); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(role)
	if err != nil || string(data) != legacy || role.SelectionSource != "" {
		t.Fatalf("legacy policy hash input changed: %s (%v)", data, err)
	}
}

func TestProjectPolicyNewDiscoveryScopeDefersOnlyNativeValidation(t *testing.T) {
	ctx := context.Background()
	f, project, p := projectRoleFixture(t)
	if _, err := f.service.SetPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, RoleValidator: projectNativeValidator{err: errors.New("old discovery scope cannot advertise new model")}})
	if err := f.service.ValidateProjectConfiguration(ctx, project); err == nil {
		t.Fatal("stable project scope skipped native validation")
	}
	project.Config.Env = map[string]string{"MODEL_SCOPE": "changed"}
	if err := f.service.ValidateProjectConfiguration(ctx, project); err != nil {
		t.Fatalf("candidate checked against old discovery scope: %v", err)
	}
	project.Config.Worker.AgentConfig.Effort = "not-approved"
	if err := f.service.ValidateProjectConfiguration(ctx, project); !errors.Is(err, instructorsvc.ErrValidation) {
		t.Fatalf("changed discovery scope bypassed policy permission: %v", err)
	}
	project.Config.Worker.AgentConfig.Effort = "high"
	if err := f.store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.service.ResolveProjectPolicy(ctx, "governed"); !errors.Is(err, instructorsvc.ErrValidation) {
		t.Fatalf("resolved work policy skipped admission native validation: %v", err)
	}
}

func TestProjectPolicyPreviewPinsLegacyWildcardWithoutChangingTemplate(t *testing.T) {
	ctx := context.Background()
	f, project, p := projectRoleFixture(t)
	project.Config.Worker.Harness = domain.HarnessOpenCode
	project.Config.Reviewers[0].Harness = domain.ReviewerCodex
	if err := f.store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	for name, harness := range map[string]string{"worker": "opencode", "reviewer": "codex", "orchestrator": "codex"} {
		role := p.Roles[name]
		role.SelectionSource, role.Harness = "", harness
		p.Roles[name] = role
	}
	if _, err := f.service.SetPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	resolved, _, err := f.service.ResolveProjectPolicy(ctx, "governed")
	if err != nil || resolved.Roles["orchestrator"].Model != "control-model" || resolved.Roles["orchestrator"].Effort != "max" || resolved.Version != instructorsvc.PolicyVersion(resolved) {
		t.Fatalf("legacy target did not pin concrete selection: %+v, %v", resolved, err)
	}
	stored, err := f.service.GetPolicy(ctx)
	if err != nil || stored.Roles["orchestrator"].Model != "" || stored.Roles["orchestrator"].Effort != "configured" {
		t.Fatalf("preview rewrote legacy template: %+v, %v", stored, err)
	}
	project.Config.Orchestrator.AgentConfig.Effort = "high"
	if err := f.store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	changed, _, err := f.service.ResolveProjectPolicy(ctx, "governed")
	if err != nil || resolved.Version == changed.Version {
		t.Fatalf("changed native effort did not change exact approval target: %+v, %v", changed, err)
	}
}

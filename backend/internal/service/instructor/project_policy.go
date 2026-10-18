package instructor

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// WithConfigurationMutation serializes daemon-owned project and policy writes
// across their entire validation/write boundary. It is independent of work
// locks; callers must enter it before taking a project registration lock.
func (s *Service) WithConfigurationMutation(ctx context.Context, mutate func() error) error {
	select {
	case s.configurationGate <- struct{}{}:
		defer func() { <-s.configurationGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return mutate()
}

// ResolveProjectPolicy resolves the actual repository/global policy and the
// project selections into a candidate immutable work policy. It never updates
// existing works, and requires current native capability validation.
func (s *Service) ResolveProjectPolicy(ctx context.Context, id domain.ProjectID) (domain.InstructorPolicy, string, error) {
	project, err := s.policyProject(ctx, id)
	if err != nil {
		return domain.InstructorPolicy{}, "", err
	}
	p, source, err := s.ResolvePolicyForProject(ctx, project)
	if err == nil {
		err = s.ValidateResolvedPolicy(ctx, id, p)
	}
	if err == nil {
		// An explicitly approved rebind pins even legacy wildcard choices to
		// the project selection shown in its preview. Never change the stored
		// template or an existing work merely because it was read.
		for _, name := range []string{"worker", "reviewer", "orchestrator"} {
			role := p.Roles[name]
			selection := projectRoleSelection(project.Config, name)
			if role.Model == "" {
				role.Model = selection.Model
			}
			if role.Effort == "configured" {
				role.Effort = selection.Effort
			}
			p.Roles[name] = role
		}
		p.Version = PolicyVersion(p)
	}
	return p, source, err
}

func (s *Service) policyProject(ctx context.Context, id domain.ProjectID) (domain.ProjectRecord, error) {
	projects, ok := s.deps.Store.(interface {
		GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	})
	if !ok {
		return domain.ProjectRecord{}, invalid("project configuration lookup unavailable")
	}
	project, found, err := projects.GetProject(ctx, string(id))
	if err != nil {
		return project, err
	}
	if !found || !project.ArchivedAt.IsZero() {
		return project, invalid("project configuration not found")
	}
	return project, nil
}

// ProjectPolicyTemplate applies repository precedence without resolving choices.
// Project save validation uses the candidate row, before it is persisted.
func (s *Service) ProjectPolicyTemplate(ctx context.Context, project domain.ProjectRecord) (domain.InstructorPolicy, string, error) {
	p, err := s.GetPolicy(ctx)
	if err != nil {
		return p, "global", err
	}
	return projectPolicyTemplate(project, p)
}

func projectPolicyTemplate(project domain.ProjectRecord, p domain.InstructorPolicy) (domain.InstructorPolicy, string, error) {
	relative := project.Config.GovernanceFile
	if relative == "" {
		relative = ".ao/governance.json"
	}
	if project.Path != "" {
		repo, loadErr := LoadPolicy(project.Path, relative)
		if loadErr == nil {
			return repo, "repository:" + relative, nil
		}
		if !os.IsNotExist(loadErr) || project.Config.GovernanceFile != "" {
			return p, "repository:" + relative, loadErr
		}
	}
	return p, "global", nil
}

// ResolvePolicyForProject resolves project-sourced roles from the supplied row.
// The provenance names both the workflow origin and native-selection source.
func (s *Service) ResolvePolicyForProject(ctx context.Context, project domain.ProjectRecord) (domain.InstructorPolicy, string, error) {
	p, source, err := s.ProjectPolicyTemplate(ctx, project)
	if err != nil {
		return p, source, err
	}
	p, err = s.resolveProjectSelections(ctx, project, p)
	for _, role := range p.Roles {
		if role.SelectionSource == "project" {
			source += ";project-settings"
			break
		}
	}
	return p, source, err
}

func (s *Service) resolveProjectSelections(ctx context.Context, project domain.ProjectRecord, p domain.InstructorPolicy) (domain.InstructorPolicy, error) {
	if err := ValidatePolicy(p); err != nil {
		return p, err
	}
	if err := ValidateLockedPolicy(p); err != nil {
		return p, err
	}
	p.Roles = maps.Clone(p.Roles)
	for _, name := range []string{"worker", "reviewer", "orchestrator"} {
		role, exists := p.Roles[name]
		if !exists || role.SelectionSource != "project" {
			continue
		}
		var resolved domain.InstructorRole
		var err error
		selection := projectRoleSelection(project.Config, name)
		if governed, bindErr := validateBackboneSelection(p, name, selection.Harness, selection.Model); governed || bindErr != nil {
			if bindErr != nil {
				return p, bindErr
			}
			if selection.Harness == "" || selection.Model == "" || selection.Effort == "" {
				return p, invalid(fmt.Sprintf("project %s role %s needs an explicit harness, model and native effort in project settings", project.ID, name))
			}
			if err = validateSelectionConstraint(role, selection); err == nil {
				resolved = role
				resolved.Harness, resolved.Model = selection.Harness, selection.Model
				resolved.EffortByModel = maps.Clone(role.EffortByModel)
				if resolved.Effort == "configured" {
					resolved.Effort = selection.Effort
				}
			}
		} else {
			resolved, err = s.ResolveProjectRole(ctx, project, name, role)
		}
		if err != nil {
			return p, err
		}
		p.Roles[name] = resolved
	}
	p.Version = PolicyVersion(p)
	return p, nil
}

// ResolveProjectRole pins one project role. Its maximum mapping remains an
// explicit permission: selecting an arbitrary native variant cannot approve it.
func (s *Service) ResolveProjectRole(ctx context.Context, project domain.ProjectRecord, name string, role domain.InstructorRole) (domain.InstructorRole, error) {
	if role.SelectionSource != "project" {
		return role, nil
	}
	selection := projectRoleSelection(project.Config, name)
	if selection.Harness == "" || selection.Model == "" || selection.Effort == "" {
		return role, invalid(fmt.Sprintf("project %s role %s needs an explicit harness, model and native effort in project settings", project.ID, name))
	}
	role.Harness, role.Model = selection.Harness, selection.Model
	role.EffortByModel = maps.Clone(role.EffortByModel)
	if err := validateSelectionConstraint(role, selection); err != nil {
		return role, invalid(fmt.Sprintf("project %s role %s: %v", project.ID, name, err))
	}
	if err := s.validateNativeSelection(ctx, domain.ProjectID(project.ID), name, role, selection); err != nil {
		return role, err
	}
	if role.Effort == "configured" {
		role.Effort = selection.Effort
	}
	return role, nil
}

func projectRoleSelection(cfg domain.ProjectConfig, name string) domain.InstructorAgentSelection {
	var harness string
	var override domain.AgentConfig
	switch name {
	case "worker":
		harness, override = string(cfg.Worker.Harness), cfg.Worker.AgentConfig
	case "orchestrator":
		harness, override = string(cfg.Orchestrator.Harness), cfg.Orchestrator.AgentConfig
	case "reviewer":
		if len(cfg.Reviewers) == 0 {
			return domain.InstructorAgentSelection{}
		}
		harness, override = string(cfg.Reviewers[0].Harness), cfg.Reviewers[0].AgentConfig
	}
	base := cfg.AgentConfig
	if name == "reviewer" {
		// Review Engine consumes the reviewer slot independently of worker and
		// orchestrator defaults; mirror that existing adapter boundary exactly.
		base = domain.AgentConfig{}
	}
	base.NativeEffortDefaults = domain.MergeNativeEffortDefaults(base.NativeEffortDefaults, override.NativeEffortDefaults)
	if override.Model != "" {
		if override.Model != base.Model && override.Effort == "" && base.NativeEffortDefaults != nil {
			base.Effort = ""
		}
		base.Model = override.Model
	}
	if override.Effort != "" {
		base.Effort = override.Effort
	}
	if base.Effort == "" {
		base.Effort, _ = base.NativeEffort(harness, base.Model)
	}
	return domain.InstructorAgentSelection{Harness: harness, Model: base.Model, Effort: base.Effort}
}

func validateSelectionConstraint(role domain.InstructorRole, selection domain.InstructorAgentSelection) error {
	if role.Harness != "" && selection.Harness != "" && role.Harness != selection.Harness {
		return fmt.Errorf("policy requires harness %s; project settings select %s", role.Harness, selection.Harness)
	}
	if role.Model != "" && selection.Model != "" && role.Model != selection.Model {
		return fmt.Errorf("policy requires model %s; project settings select %s", role.Model, selection.Model)
	}
	if selection.Model == "" || selection.Effort == "" {
		return nil
	}
	expected := role.Effort
	if expected == "maximum" {
		expected = role.EffortByModel[selection.Model]
		if expected == "" {
			return fmt.Errorf("maximum needs an approved native effort mapping for model %s in the policy", selection.Model)
		}
	}
	if expected != "configured" && expected != "" && expected != selection.Effort {
		return fmt.Errorf("policy requires native effort %s; project settings select %s", expected, selection.Effort)
	}
	return nil
}

func (s *Service) validateNativeSelection(ctx context.Context, id domain.ProjectID, name string, role domain.InstructorRole, selection domain.InstructorAgentSelection) error {
	if s.deps.RoleValidator == nil {
		return invalid("native model capability validator unavailable")
	}
	if err := s.deps.RoleValidator.ValidateInstructorAgentConfig(ctx, id, role, selection.Harness, selection.Model, selection.Effort); err != nil {
		return fmt.Errorf("%w: project %s role %s: %w", ErrValidation, id, name, err)
	}
	return nil
}

// ValidateResolvedPolicy validates every executable project role, including
// legacy constraints, before accepting an explicit existing-work rebind.
func (s *Service) ValidateResolvedPolicy(ctx context.Context, id domain.ProjectID, p domain.InstructorPolicy) error {
	project, err := s.policyProject(ctx, id)
	if err != nil {
		return err
	}
	for _, name := range []string{"worker", "reviewer", "orchestrator"} {
		role, ok := p.Roles[name]
		if !ok {
			return invalid("policy has no " + name + " role")
		}
		selection := projectRoleSelection(project.Config, name)
		if selection.Harness == "" || selection.Model == "" || selection.Effort == "" {
			return invalid("project role " + name + " needs an explicit harness, model and native effort")
		}
		if err := validateSelectionConstraint(role, selection); err != nil {
			return invalid(fmt.Sprintf("project %s role %s: %v", id, name, err))
		}
		if governed, bindErr := validateBackboneSelection(p, name, selection.Harness, selection.Model); governed || bindErr != nil {
			if bindErr != nil {
				return bindErr
			}
			continue
		}
		if err := s.validateNativeSelection(ctx, id, name, role, selection); err != nil {
			return err
		}
	}
	return nil
}

// ValidateProjectConfiguration rejects contradictory configured selections
// before project persistence. Incomplete drafts remain configurable; registration
// and rebind require fully explicit, native-validated project-sourced roles.
func (s *Service) ValidateProjectConfiguration(ctx context.Context, project domain.ProjectRecord) error {
	p, _, err := s.ProjectPolicyTemplate(ctx, project)
	if err != nil {
		return err
	}
	// Discovery reads a persisted project scope. A new project or changed
	// discovery environment must not be checked against a different old/global
	// scope. Check policy consistency now and native capabilities at admission.
	validateNative := false
	if projects, ok := s.deps.Store.(interface {
		GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	}); ok {
		current, found, err := projects.GetProject(ctx, project.ID)
		if err != nil {
			return err
		}
		validateNative = found && current.Path == project.Path && maps.Equal(current.Config.Env, project.Config.Env)
	}
	return s.validateProjectSelections(ctx, project, p, validateNative)
}

func (s *Service) validateProjectSelections(ctx context.Context, project domain.ProjectRecord, p domain.InstructorPolicy, validateNative bool) error {
	for _, name := range []string{"worker", "reviewer", "orchestrator"} {
		role, exists := p.Roles[name]
		if !exists {
			continue
		}
		selection := projectRoleSelection(project.Config, name)
		if role.SelectionSource == "project" {
			role.Harness, role.Model = selection.Harness, selection.Model
		}
		if err := validateSelectionConstraint(role, selection); err != nil {
			return invalid(fmt.Sprintf("project %s role %s: %v; use project-sourced selection or choose a permitted value", project.ID, name, err))
		}
		if len(p.Backbone) != 0 && selection.Harness == "opencode" && selection.Model != "" {
			if _, err := validateBackboneSelection(p, name, selection.Harness, selection.Model); err != nil {
				return err
			}
			continue
		}
		if validateNative && selection.Harness != "" && strings.TrimSpace(selection.Model) != "" && strings.TrimSpace(selection.Effort) != "" {
			if err := s.validateNativeSelection(ctx, domain.ProjectID(project.ID), name, role, selection); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) validatePolicyProjectConflicts(ctx context.Context, p domain.InstructorPolicy) error {
	projects, ok := s.deps.Store.(interface {
		ListProjects(context.Context) ([]domain.ProjectRecord, error)
	})
	if !ok {
		return nil
	}
	rows, err := projects.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, project := range rows {
		if !project.ArchivedAt.IsZero() {
			continue
		}
		_, source, err := projectPolicyTemplate(project, p)
		if err != nil {
			return err
		}
		if source != "global" {
			continue // A repository policy owns this project's constraints.
		}
		if err := s.validateProjectSelections(ctx, project, p, true); err != nil {
			return err
		}
	}
	return nil
}

package instructor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/backbone"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type backboneProjectValidator struct{ calls int }

func (v *backboneProjectValidator) ValidateInstructorAgentConfig(context.Context, domain.ProjectID, domain.InstructorRole, string, string, string) error {
	v.calls++
	return errors.New("governed registration must not probe native capabilities")
}

func backboneProjectPolicyFixture(t *testing.T, binding *backbone.Binding, policyEffort, projectEffort string) (*instructorFixture, domain.InstructorPolicy, *backboneProjectValidator) {
	t.Helper()
	f := newInstructorFixture(t)
	project, _, err := f.store.GetProject(context.Background(), "governed")
	if err != nil {
		t.Fatal(err)
	}
	project.Config.Orchestrator = domain.RoleOverride{Harness: domain.HarnessOpenCode, AgentConfig: domain.AgentConfig{Model: "provider/worker", Effort: projectEffort}}
	if err := f.store.UpsertProject(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	validator := &backboneProjectValidator{}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, RoleValidator: validator})
	policy := instructorsvc.DefaultPolicy()
	policy.Roles["orchestrator"] = domain.InstructorRole{SelectionSource: "project", Effort: policyEffort, EffortByModel: map[string]string{"provider/worker": "high"}}
	if binding != nil {
		bound := *binding
		bound.Contexts = map[string]backbone.RoleBinding{"orchestrator": binding.Contexts["worker"]}
		policy.Backbone = producerJSON(t, bound)
	}
	policy.Version = ""
	if err := instructorsvc.ValidatePolicy(policy); err != nil {
		t.Fatalf("invalid policy template: %v", err)
	}
	if err := instructorsvc.ValidateLockedPolicy(policy); err != nil {
		t.Fatalf("invalid locked policy template: %v", err)
	}
	return f, policy, validator
}

func TestBackboneProjectSelectionRejectsMissingEffortBeforePersistence(t *testing.T) {
	_, binding := producerWorkspace(t)
	for _, governed := range []bool{false, true} {
		name := "unbound"
		var bound *backbone.Binding
		if governed {
			name, bound = "governed", &binding
		}
		for _, policyEffort := range []string{"configured", "high", "maximum"} {
			t.Run(name+"/"+policyEffort, func(t *testing.T) {
				f, policy, validator := backboneProjectPolicyFixture(t, bound, policyEffort, "")
				_, err := f.service.Register(context.Background(), instructorsvc.RegisterInput{ProjectID: "governed", Name: "incomplete native selection", Goal: "Reject incomplete project effort", Policy: policy})
				if !errors.Is(err, instructorsvc.ErrValidation) {
					t.Errorf("missing project effort was not rejected: %v", err)
				}
				works, err := f.store.ListInstructorWorks(context.Background())
				if err != nil || len(works) != 0 {
					t.Errorf("incomplete selection persisted work: count=%d err=%v", len(works), err)
				}
				if validator.calls != 0 {
					t.Errorf("incomplete selection invoked native capability validator %d times", validator.calls)
				}
			})
		}
	}
}

func TestBackboneProjectSelectionPinsExplicitEffortWithoutNativeProbe(t *testing.T) {
	_, binding := producerWorkspace(t)
	for _, policyEffort := range []string{"configured", "high", "maximum"} {
		t.Run(policyEffort, func(t *testing.T) {
			f, policy, validator := backboneProjectPolicyFixture(t, &binding, policyEffort, "high")
			work, err := f.service.Register(context.Background(), instructorsvc.RegisterInput{ProjectID: "governed", Name: "explicit native selection", Goal: "Pin approved project effort", Policy: policy})
			if err != nil {
				t.Fatal(err)
			}
			wantEffort := policyEffort
			if wantEffort == "configured" {
				wantEffort = "high"
			}
			role := work.Policy.Roles["orchestrator"]
			if role.SelectionSource != "project" || role.Harness != "opencode" || role.Model != "provider/worker" || role.Effort != wantEffort || role.EffortByModel["provider/worker"] != "high" {
				t.Fatalf("explicit project selection was not pinned: %+v", role)
			}
			if err := instructorsvc.ValidatePolicy(work.Policy); err != nil {
				t.Fatalf("registered policy is invalid: %v", err)
			}
			works, err := f.store.ListInstructorWorks(context.Background())
			if err != nil || len(works) != 1 || works[0].PolicyVersion != instructorsvc.PolicyVersion(work.Policy) || works[0].Policy.Roles["orchestrator"].Effort != wantEffort {
				t.Fatalf("pinned policy did not survive persistence: %+v, %v", works, err)
			}
			if validator.calls != 0 {
				t.Fatalf("governed registration invoked native capability validator %d times", validator.calls)
			}
			if policy.Roles["orchestrator"].Harness != "" || policy.Roles["orchestrator"].Model != "" || policy.Roles["orchestrator"].Effort != policyEffort {
				t.Fatal("registration mutated the policy template")
			}
		})
	}
}

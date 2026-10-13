package instructor_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	projectsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/project"
)

type instructorMutationPersistence interface{ instructorsvc.Store }
type projectMutationPersistence interface{ projectsvc.Store }

// Unused persistence operations deliberately have no implementation: this test
// exercises only the real project/policy service validation and write boundaries.
type configurationRaceStore struct {
	instructorMutationPersistence
	projectMutationPersistence
	mu      sync.Mutex
	project domain.ProjectRecord
	policy  domain.InstructorPolicy
}

func (s *configurationRaceStore) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.project, true, nil
}
func (s *configurationRaceStore) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []domain.ProjectRecord{s.project}, nil
}
func (s *configurationRaceStore) UpsertProject(_ context.Context, row domain.ProjectRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.project = row
	return nil
}
func (s *configurationRaceStore) UpdateProjectSettings(ctx context.Context, _ string, name string, config domain.ProjectConfig) (bool, error) {
	row, _, _ := s.GetProject(ctx, "project")
	row.Config, row.DisplayName = config, name
	return true, s.UpsertProject(ctx, row)
}
func (s *configurationRaceStore) GetInstructorPolicy(context.Context) (domain.InstructorPolicy, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy, true, nil
}
func (s *configurationRaceStore) PutInstructorPolicy(_ context.Context, policy domain.InstructorPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = policy
	return nil
}

func TestConcurrentProjectAndPolicyMutationsCannotPersistContradictorySelections(t *testing.T) {
	for _, operation := range []string{"config", "settings"} {
		t.Run(operation, func(t *testing.T) {
			path := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				store := &configurationRaceStore{policy: instructorsvc.DefaultPolicy(), project: domain.ProjectRecord{ID: "project", Path: path, Config: domain.ProjectConfig{DefaultBranch: "main", Orchestrator: domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Model: "model-a", Effort: "high"}}}}}
				instructor := instructorsvc.New(instructorsvc.Deps{Store: store, RoleValidator: projectNativeValidator{}})
				project := projectsvc.New(store)
				project.SetGovernanceMutationGuard(instructor.WithConfigurationMutation)
				validated, release := make(chan struct{}), make(chan struct{})
				project.SetGovernanceValidator(func(ctx context.Context, row domain.ProjectRecord) error {
					if err := instructor.ValidateProjectConfiguration(ctx, row); err != nil {
						return err
					}
					close(validated)
					<-release // Preserve the exact validation-before-write window.
					return nil
				})
				candidate := store.project.Config
				candidate.Orchestrator.AgentConfig.Model = "model-b"
				projectDone, policyDone := make(chan error, 1), make(chan error, 1)
				go func() {
					var err error
					if operation == "config" {
						_, err = project.SetConfig(ctx, "project", projectsvc.SetConfigInput{Config: candidate})
					} else {
						_, err = project.UpdateSettings(ctx, "project", projectsvc.UpdateSettingsInput{DisplayName: "Project", Config: candidate})
					}
					projectDone <- err
				}()
				<-validated
				go func() {
					policy := instructorsvc.DefaultPolicy()
					role := policy.Roles["orchestrator"]
					role.Model = "model-a"
					policy.Roles["orchestrator"] = role
					_, err := instructor.SetPolicy(ctx, policy)
					policyDone <- err
				}()
				synctest.Wait()
				select {
				case err := <-policyDone:
					t.Fatalf("policy mutation passed the project validation/write guard: %v", err)
				default:
				}
				close(release)
				if err := <-projectDone; err != nil {
					t.Fatal(err)
				}
				if err := <-policyDone; !errors.Is(err, instructorsvc.ErrValidation) {
					t.Fatalf("policy did not validate committed model-b selection: %v", err)
				}
				row, _, _ := store.GetProject(ctx, "project")
				policy, _, _ := store.GetInstructorPolicy(ctx)
				if row.Config.Orchestrator.AgentConfig.Model != "model-b" || policy.Roles["orchestrator"].Model != "" {
					t.Fatalf("concurrent edits persisted inconsistent settings: %+v %+v", row.Config, policy.Roles)
				}
			})
		})
	}
}

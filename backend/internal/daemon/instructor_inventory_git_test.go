package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func instructorTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func instructorTestRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	instructorTestGit(t, repo, "init", "--initial-branch=main")
	instructorTestGit(t, repo, "-c", "user.name=Operator", "-c", "user.email=operator@example.test", "commit", "--allow-empty", "-m", "initial")
	return repo
}

func TestInstructorGitInventoryDiscoversDirtyAndUnassignedResourcesReadOnly(t *testing.T) {
	repo := instructorTestRepository(t)
	linked, unassigned := filepath.Join(t.TempDir(), "linked"), filepath.Join(t.TempDir(), "unassigned")
	instructorTestGit(t, repo, "worktree", "add", "-b", "managed", linked)
	instructorTestGit(t, repo, "worktree", "add", "-b", "forgotten", unassigned)
	if err := os.WriteFile(filepath.Join(linked, "changes.txt"), []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := instructorTestGit(t, linked, "status", "--porcelain=v1")
	projects := []domain.ProjectRecord{{ID: "project", Path: repo}}
	sessions := []domain.SessionRecord{{ID: "worker", ProjectID: "project", Metadata: domain.SessionMetadata{WorkspacePath: linked, Branch: "managed"}}}
	works := []domain.InstructorWork{{ID: "work", ProjectID: "project", Branch: "managed", SessionIDs: []domain.SessionID{"worker"}}}
	resources, count := (instructorInventory{store: instructorTestStore{works: works, sessions: sessions}}).gitInventory(context.Background(), projects, sessions, works)
	var dirty, missing, branch bool
	for _, resource := range resources {
		if resource.Kind == "uncommitted" && resource.Path == linked {
			dirty = resource.Dirty && resource.Assignment == "linked" && len(resource.WorkIDs) == 1 && resource.WorkIDs[0] == "work"
		}
		if resource.Kind == "worktree" && resource.Path == unassigned {
			missing = resource.Assignment == "unassigned" && len(resource.WorkIDs) == 0
		}
		if resource.Kind == "branch" && resource.Branch == "forgotten" {
			branch = resource.Assignment == "unassigned"
		}
	}
	if !dirty || !missing || !branch || count == 0 {
		t.Fatalf("dirty=%v unassigned=%v branch=%v count=%d resources=%+v", dirty, missing, branch, count, resources)
	}
	if after := instructorTestGit(t, linked, "status", "--porcelain=v1"); before != after {
		t.Fatalf("inventory mutated workspace: before=%q after=%q", before, after)
	}
}

func TestInstructorGitInventoryMarksUnavailableInsteadOfInventingFacts(t *testing.T) {
	resources, count := (instructorInventory{}).gitInventory(context.Background(), []domain.ProjectRecord{{ID: "project", Path: filepath.Join(t.TempDir(), "missing")}}, nil, nil)
	if len(resources) != 1 || resources[0].Assignment != "unknown" || resources[0].Error == "" || count != 0 {
		t.Fatalf("resources=%+v count=%d", resources, count)
	}
}

func TestInstructorGitInventoryCancellationReturnsVisibleIncompleteScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resources, _ := (instructorInventory{}).gitInventory(ctx, []domain.ProjectRecord{{ID: "project", Path: "/unused"}}, nil, nil)
	if len(resources) != 1 || resources[0].Error == "" {
		t.Fatalf("resources=%+v", resources)
	}
}

func TestInstructorParseWorktreesPreservesSpacesAndDetachedHeads(t *testing.T) {
	data := []byte("worktree /repo with spaces\x00HEAD abc\x00branch refs/heads/feature\x00\x00worktree /detached\x00HEAD def\x00detached\x00\x00")
	worktrees := instructorParseWorktrees(data)
	if len(worktrees) != 2 || worktrees[0].path != "/repo with spaces" || worktrees[0].branch != "feature" || worktrees[1].branch != "" {
		t.Fatalf("worktrees=%+v", worktrees)
	}
}

func TestInstructorGitInventoryDoesNotLinkSameBranchAcrossRepositories(t *testing.T) {
	first, second := instructorTestRepository(t), instructorTestRepository(t)
	instructorTestGit(t, first, "checkout", "-b", "shared-name")
	instructorTestGit(t, second, "checkout", "-b", "shared-name")
	projects := []domain.ProjectRecord{{ID: "workspace", Path: first}}
	sessions := []domain.SessionRecord{
		{ID: "worker", ProjectID: "workspace", Metadata: domain.SessionMetadata{WorkspacePath: first, Branch: "shared-name"}},
		{ID: "unassociated", ProjectID: "workspace", Metadata: domain.SessionMetadata{WorkspacePath: second, Branch: "shared-name"}},
	}
	works := []domain.InstructorWork{{ID: "work", ProjectID: "workspace", Branch: "shared-name", SessionIDs: []domain.SessionID{"worker"}}}
	resources, _ := (instructorInventory{store: instructorTestStore{works: works, sessions: sessions}}).gitInventory(context.Background(), projects, sessions, works)
	var linked, unassigned bool
	for _, resource := range resources {
		if resource.Kind != "branch" || resource.Branch != "shared-name" {
			continue
		}
		if resource.RepositoryPath == filepath.Join(first, ".git") {
			linked = resource.Assignment == "linked" && len(resource.WorkIDs) == 1 && len(resource.SessionIDs) == 1 && resource.SessionIDs[0] == "worker"
		}
		if resource.RepositoryPath == filepath.Join(second, ".git") {
			unassigned = resource.Assignment == "unassigned" && len(resource.WorkIDs) == 0 && len(resource.SessionIDs) == 1 && resource.SessionIDs[0] == "unassociated"
		}
	}
	if !linked || !unassigned {
		t.Fatalf("repository-scoped association incorrect: %+v", resources)
	}
}

func TestInstructorGitBranchIdentitySurvivesRegisteredSourceChange(t *testing.T) {
	repo := instructorTestRepository(t)
	alternate := filepath.Join(t.TempDir(), "alternate")
	instructorTestGit(t, repo, "worktree", "add", "-b", "managed", alternate)
	first, _ := (instructorInventory{}).gitInventory(context.Background(), []domain.ProjectRecord{{ID: "project", Path: repo}}, nil, nil)
	second, _ := (instructorInventory{}).gitInventory(context.Background(), nil, []domain.SessionRecord{{ID: "worker", ProjectID: "project", Metadata: domain.SessionMetadata{WorkspacePath: alternate, Branch: "managed"}}}, nil)
	ids := map[string]string{}
	for _, resource := range first {
		if resource.Kind == "branch" {
			ids[resource.Branch] = resource.ID
		}
	}
	for _, resource := range second {
		if resource.Kind == "branch" && ids[resource.Branch] != resource.ID {
			t.Fatalf("branch %s acquired a new identity after source change: %s != %s", resource.Branch, ids[resource.Branch], resource.ID)
		}
	}
}

type failingInstructorWorkBindingStore struct {
	instructorTestStore
	err   error
	calls int
}

func (s *failingInstructorWorkBindingStore) InstructorWorkForSession(context.Context, domain.SessionID) (domain.InstructorWork, bool, error) {
	s.calls++
	return domain.InstructorWork{}, false, s.err
}

func TestInstructorGitInventoryBindingReadFailureIsUnknown(t *testing.T) {
	for _, cause := range []error{errors.New("database is unavailable; sensitive internal detail"), context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			repo := instructorTestRepository(t)
			sessions := []domain.SessionRecord{{ID: "owner", ProjectID: "binding-project", Metadata: domain.SessionMetadata{WorkspacePath: repo, Branch: "main"}}}
			store := &failingInstructorWorkBindingStore{err: cause}
			resources, count := (instructorInventory{store: store}).gitInventory(context.Background(), []domain.ProjectRecord{{ID: "binding-project", Path: repo}}, sessions, nil)
			if store.calls != 1 || len(resources) == 0 {
				t.Fatalf("binding read not exercised: calls=%d resources=%+v", store.calls, resources)
			}
			affected := 0
			for _, resource := range resources {
				if len(resource.SessionIDs) == 0 {
					continue
				}
				affected++
				if resource.Assignment != "unknown" || resource.Error == "" || strings.Contains(resource.Error, "sensitive internal detail") {
					t.Fatalf("binding failure converted to unassigned or leaked internal detail: %+v", resource)
				}
			}
			if affected == 0 || count != 0 {
				t.Fatalf("failed associations counted as unassigned: affected=%d count=%d resources=%+v", affected, count, resources)
			}
		})
	}
}

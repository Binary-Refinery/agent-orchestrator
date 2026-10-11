package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

const instructorGitOutputLimit = 2 << 20

// git output and total collection time are bounded: inventory must remain a
// short read even when a repository has a huge status or broken filesystem.
type instructorBoundedOutput struct{ bytes.Buffer }

func (b *instructorBoundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > instructorGitOutputLimit {
		return 0, fmt.Errorf("git output exceeds inventory limit")
	}
	return b.Buffer.Write(p)
}

func instructorReadCommand(ctx context.Context, dir, binary string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	command := aoprocess.CommandContext(commandCtx, binary, args...)
	command.Dir = dir
	var output instructorBoundedOutput
	command.Stdout = &output
	// Stderr may contain URLs with credentials. Return an operation-level error
	// instead of exposing arbitrary provider or repository diagnostic output.
	if err := command.Run(); err != nil {
		if commandCtx.Err() != nil {
			return nil, fmt.Errorf("%s read timed out or was cancelled", binary)
		}
		return nil, fmt.Errorf("%s read failed: %w", binary, err)
	}
	return output.Bytes(), nil
}

func instructorReadGit(ctx context.Context, path string, args ...string) ([]byte, error) {
	return instructorReadCommand(ctx, path, "git", append([]string{"--no-replace-objects", "--no-optional-locks", "-c", "core.fsmonitor=false"}, args...)...)
}

type instructorGitSource struct{ projectID, path string }
type instructorWorkspaceInventoryStore interface {
	ListWorkspaceRepos(context.Context, string) ([]domain.WorkspaceRepoRecord, error)
	ListSessionWorktrees(context.Context, domain.SessionID) ([]domain.SessionWorktreeRecord, error)
}

// gitInventory inspects only registered roots and registered session sources.
// It neither adopts discoveries nor reads arbitrary directory trees.
func (i instructorInventory) gitInventory(ctx context.Context, projects []domain.ProjectRecord, sessions []domain.SessionRecord, works []domain.InstructorWork) ([]controllers.InstructorGitResourceInventory, int) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resources := []controllers.InstructorGitResourceInventory{}
	sources := map[string]instructorGitSource{}
	pathSessions, pathBranchSessions := map[string]map[string]bool{}, map[string]map[string]bool{}
	repositoryByPath := map[string]string{}
	linkSession := func(project, path, branch, id string) {
		if path != "" {
			key := project + "\x00" + instructorInventoryPathKey(path)
			if pathSessions[key] == nil {
				pathSessions[key] = map[string]bool{}
			}
			pathSessions[key][id] = true
		}
		if branch != "" && path != "" {
			key := project + "\x00" + instructorInventoryPathKey(path) + "\x00" + branch
			if pathBranchSessions[key] == nil {
				pathBranchSessions[key] = map[string]bool{}
			}
			pathBranchSessions[key][id] = true
		}
	}
	addSource := func(projectID, path string) {
		if path != "" {
			path = filepath.Clean(path)
			sources[projectID+"\x00"+path] = instructorGitSource{projectID, path}
		}
	}
	extraStore, extra := i.store.(instructorWorkspaceInventoryStore)
	for _, project := range projects {
		addSource(project.ID, project.Path)
		if extra && project.Kind == domain.ProjectKindWorkspace {
			children, err := extraStore.ListWorkspaceRepos(ctx, project.ID)
			if err != nil {
				resources = append(resources, instructorGitFailure(project.ID, project.Path, "registered child repository list is unavailable"))
			}
			for _, child := range children {
				clean := filepath.Clean(child.RelativePath)
				if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
					resources = append(resources, instructorGitFailure(project.ID, project.Path, "registered child repository path is invalid"))
					continue
				}
				addSource(project.ID, filepath.Join(project.Path, clean))
			}
		}
	}
	for _, session := range sessions {
		addSource(string(session.ProjectID), session.Metadata.WorkspaceRepoPath)
		addSource(string(session.ProjectID), session.Metadata.WorkspacePath)
		linkSession(string(session.ProjectID), session.Metadata.WorkspaceRepoPath, session.Metadata.Branch, string(session.ID))
		linkSession(string(session.ProjectID), session.Metadata.WorkspacePath, session.Metadata.Branch, string(session.ID))
		if extra {
			worktrees, err := extraStore.ListSessionWorktrees(ctx, session.ID)
			if err != nil {
				resources = append(resources, instructorGitFailure(string(session.ProjectID), session.Metadata.WorkspacePath, "registered session worktree list is unavailable"))
			}
			for _, worktree := range worktrees {
				addSource(string(session.ProjectID), worktree.WorktreePath)
				linkSession(string(session.ProjectID), worktree.WorktreePath, worktree.Branch, string(session.ID))
			}
		}
	}
	keys := make([]string, 0, len(sources))
	for key := range sources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	for n, key := range keys {
		source := sources[key]
		if n >= 128 || ctx.Err() != nil {
			resources = append(resources, instructorGitFailure(source.projectID, source.path, "inventory scan limit reached; remaining registered sources were not inspected"))
			break
		}
		common, err := instructorReadGit(ctx, source.path, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			resources = append(resources, instructorGitFailure(source.projectID, source.path, err.Error()))
			continue
		}
		repository := strings.TrimSpace(string(common))
		repositoryByPath[source.projectID+"\x00"+instructorInventoryPathKey(source.path)] = repository
		identity := source.projectID + "\x00" + repository
		if seen[identity] {
			continue
		}
		seen[identity] = true
		repo := instructorGitResource("repository", source.projectID, repository, source.path, "", "")
		repo.Status = "observed"
		resources = append(resources, repo)
		worktreeData, err := instructorReadGit(ctx, source.path, "worktree", "list", "--porcelain", "-z")
		if err != nil {
			resources = append(resources, instructorGitFailure(source.projectID, source.path, "worktree list: "+err.Error()))
		} else {
			for _, tree := range instructorParseWorktrees(worktreeData) {
				repositoryByPath[source.projectID+"\x00"+instructorInventoryPathKey(tree.path)] = repository
				resource := instructorGitResource("worktree", source.projectID, repository, tree.path, tree.branch, tree.sha)
				resource.Status = "observed"
				status, statusErr := instructorReadGit(ctx, tree.path, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
				if statusErr != nil {
					resource.Status, resource.Error = "unavailable", "workspace status: "+statusErr.Error()
				} else if len(status) != 0 {
					resource.Dirty = true
					uncommitted := instructorGitResource("uncommitted", source.projectID, repository, tree.path, tree.branch, tree.sha)
					uncommitted.Dirty, uncommitted.Status = true, "uncommitted changes observed"
					resources = append(resources, uncommitted)
				}
				resources = append(resources, resource)
			}
		}
		branches, err := instructorReadGit(ctx, source.path, "for-each-ref", "--count=501", "--format=%(refname:short)%00%(objectname)%00", "refs/heads/")
		if err != nil {
			resources = append(resources, instructorGitFailure(source.projectID, source.path, "branch list: "+err.Error()))
		} else {
			for n, line := range strings.Split(strings.TrimSpace(string(branches)), "\n") {
				if n == 500 {
					resources = append(resources, instructorGitFailure(source.projectID, source.path, "branch list truncated after 500 branches"))
					break
				}
				parts := strings.Split(line, "\x00")
				if len(parts) < 2 || parts[0] == "" {
					continue
				}
				resource := instructorGitResource("branch", source.projectID, repository, source.path, parts[0], parts[1])
				resource.Status = "observed"
				resources = append(resources, resource)
			}
		}
	}
	// Branch names only identify a branch inside its own repository. A workspace
	// may contain multiple unrelated repositories with the same branch name.
	branchSessions := map[string]map[string]bool{}
	for key, ids := range pathBranchSessions {
		parts := strings.Split(key, "\x00")
		repository := repositoryByPath[parts[0]+"\x00"+parts[1]]
		if repository == "" {
			continue
		}
		branchKey := parts[0] + "\x00" + repository + "\x00" + parts[2]
		if branchSessions[branchKey] == nil {
			branchSessions[branchKey] = map[string]bool{}
		}
		for id := range ids {
			branchSessions[branchKey][id] = true
		}
	}
	projectRepositories := map[string]string{}
	for _, project := range projects {
		projectRepositories[project.ID] = repositoryByPath[project.ID+"\x00"+instructorInventoryPathKey(project.Path)]
	}
	unassigned := 0
	for index := range resources {
		resource := &resources[index]
		sessionSet, workSet := map[string]bool{}, map[string]bool{}
		association := pathSessions[resource.ProjectID+"\x00"+instructorInventoryPathKey(resource.Path)]
		if resource.Kind == "branch" {
			association = branchSessions[resource.ProjectID+"\x00"+resource.RepositoryPath+"\x00"+resource.Branch]
		}
		for id := range association {
			sessionSet[id] = true
		}
		for _, work := range works {
			if string(work.ProjectID) != resource.ProjectID {
				continue
			}
			linked := resource.Kind == "branch" && resource.Branch != "" && resource.Branch == work.Branch && resource.RepositoryPath == projectRepositories[resource.ProjectID]
			for _, id := range work.SessionIDs {
				linked = linked || sessionSet[string(id)]
			}
			if linked {
				workSet[work.ID] = true
			}
		}
		for id := range sessionSet {
			resource.SessionIDs = append(resource.SessionIDs, id)
		}
		for id := range workSet {
			resource.WorkIDs = append(resource.WorkIDs, id)
		}
		sort.Strings(resource.SessionIDs)
		sort.Strings(resource.WorkIDs)
		resource.Assignment = "unassigned"
		if resource.Error != "" {
			resource.Assignment = "unknown"
		} else if len(resource.WorkIDs) != 0 {
			resource.Assignment = "linked"
		} else {
			unassigned++
		}
	}
	sort.Slice(resources, func(a, b int) bool { return resources[a].ID < resources[b].ID })
	return resources, unassigned
}

func instructorInventoryPathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func instructorGitResource(kind, project, repository, path, branch, sha string) controllers.InstructorGitResourceInventory {
	identityPath := path
	if kind == "repository" || kind == "branch" {
		// The registered source used to discover a repository may change as
		// sessions attach and disappear. That cannot change a branch's identity.
		identityPath = ""
	}
	id := sha256.Sum256([]byte(strings.Join([]string{kind, project, repository, identityPath, branch}, "\x00")))
	return controllers.InstructorGitResourceInventory{ID: fmt.Sprintf("git-%x", id[:12]), Kind: kind, ProjectID: project, RepositoryPath: repository, Path: path, Branch: branch, SHA: sha, SessionIDs: []string{}, WorkIDs: []string{}}
}

func instructorGitFailure(project, path, message string) controllers.InstructorGitResourceInventory {
	resource := instructorGitResource("repository", project, path, path, "", "")
	// Distinct failed operations on one source must not share a renderer key.
	id := sha256.Sum256([]byte(resource.ID + "\x00" + message))
	resource.ID = fmt.Sprintf("git-error-%x", id[:12])
	resource.Status, resource.Error = "unavailable", message
	return resource
}

type instructorGitWorktree struct{ path, branch, sha string }

func instructorParseWorktrees(data []byte) []instructorGitWorktree {
	var result []instructorGitWorktree
	var current instructorGitWorktree
	for _, field := range strings.Split(string(data), "\x00") {
		if field == "" {
			if current.path != "" {
				result = append(result, current)
				current = instructorGitWorktree{}
			}
			continue
		}
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
			current.path = value
		case "HEAD":
			current.sha = value
		case "branch":
			current.branch = strings.TrimPrefix(value, "refs/heads/")
		}
	}
	if current.path != "" {
		result = append(result, current)
	}
	return result
}

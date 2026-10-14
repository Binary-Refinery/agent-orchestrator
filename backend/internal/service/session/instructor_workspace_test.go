package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

type workspaceLeaseManager struct {
	*fakeCommander
	held bool
	busy bool
}

func (m *workspaceLeaseManager) AcquireSessionInput(domain.SessionID) (func(), bool) {
	if m.busy {
		return nil, false
	}
	m.held = true
	return func() { m.held = false }, true
}

type workspaceCaptureStore struct {
	*fakeStore
	capture func(context.Context, string, domain.SessionID, string, domain.ProjectID) (domain.InstructorWorkspaceSnapshot, error)
}

func (s *workspaceCaptureStore) CaptureInstructorWorkspace(ctx context.Context, work string, id domain.SessionID, instance string, project domain.ProjectID) (domain.InstructorWorkspaceSnapshot, error) {
	return s.capture(ctx, work, id, instance, project)
}

func TestScopedWorkspacePinsResourcesThroughReadAndEdit(t *testing.T) {
	owned := t.TempDir()
	foreign := t.TempDir()
	writeWorkspaceFile(t, owned, "task.txt", "owned content\n")
	writeWorkspaceFile(t, foreign, "task.txt", "foreign content\n")
	base := newFakeStore()
	base.projects["project"] = domain.ProjectRecord{ID: "project", Kind: domain.ProjectKindScratch}
	rec := domain.SessionRecord{ID: "same-id", ProjectID: "project", Metadata: domain.SessionMetadata{WorkspacePath: owned}}
	manager := &workspaceLeaseManager{fakeCommander: &fakeCommander{}}
	store := &workspaceCaptureStore{fakeStore: base}
	store.capture = func(_ context.Context, work string, id domain.SessionID, instance string, project domain.ProjectID) (domain.InstructorWorkspaceSnapshot, error) {
		if !manager.held || work != "work" || id != rec.ID || instance != "old-instance" || project != rec.ProjectID {
			t.Fatal("capture ran without expected scope and lease")
		}
		// A later display-ID lookup would now resolve foreign resources. Even a
		// side channel outside the lifecycle manager cannot change pinned roots.
		base.sessions[rec.ID] = domain.SessionRecord{ID: rec.ID, ProjectID: "other-project", Metadata: domain.SessionMetadata{WorkspacePath: foreign}}
		return domain.InstructorWorkspaceSnapshot{Session: rec}, nil
	}
	svc := &Service{manager: manager, store: store}
	scope := InstructorWorkspaceScope{WorkID: "work", SessionID: rec.ID, InstanceID: "old-instance", ProjectID: rec.ProjectID}
	err := svc.WithScopedWorkspace(context.Background(), scope, func(ctx context.Context) error {
		if !manager.held {
			t.Fatal("lease released before filesystem access")
		}
		file, err := svc.GetWorkspaceFile(ctx, rec.ID, "task.txt", "")
		if err != nil || file.Content != "owned content\n" {
			t.Fatalf("read escaped captured root: %+v %v", file, err)
		}
		_, err = svc.UpdateWorkspaceFile(ctx, rec.ID, UpdateWorkspaceFileInput{Path: "task.txt", Content: "changed owned\n", ExpectedFileFingerprint: file.FileFingerprint})
		return err
	})
	if err != nil || manager.held {
		t.Fatalf("operation=%v lease still held=%v", err, manager.held)
	}
	for root, expected := range map[string]string{owned: "changed owned\n", foreign: "foreign content\n"} {
		content, err := os.ReadFile(filepath.Join(root, "task.txt"))
		if err != nil || string(content) != expected {
			t.Fatalf("root=%s content=%q error=%v", root, content, err)
		}
	}
}

func TestScopedWorkspacePinsChildRootsAndPreservesStaleFingerprint(t *testing.T) {
	root := newWorkspaceRepo(t)
	child := filepath.Join(root, "api")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, child, "init")
	runGit(t, child, "config", "user.email", "ao@example.invalid")
	runGit(t, child, "config", "user.name", "AO Test")
	writeWorkspaceFile(t, child, "task.txt", "owned child\n")
	runGit(t, child, "add", ".")
	runGit(t, child, "commit", "-m", "initial child")
	foreign := newWorkspaceRepo(t)
	writeWorkspaceFile(t, foreign, "task.txt", "foreign child\n")
	base := newFakeStore()
	base.projects["project"] = domain.ProjectRecord{ID: "project", Kind: domain.ProjectKindWorkspace}
	rec := domain.SessionRecord{ID: "same-id", ProjectID: "project", Metadata: domain.SessionMetadata{WorkspacePath: root}}
	manager := &workspaceLeaseManager{fakeCommander: &fakeCommander{}}
	store := &workspaceCaptureStore{fakeStore: base, capture: func(context.Context, string, domain.SessionID, string, domain.ProjectID) (domain.InstructorWorkspaceSnapshot, error) {
		base.worktrees[rec.ID] = []domain.SessionWorktreeRecord{{SessionID: rec.ID, RepoName: "api", WorktreePath: foreign}}
		return domain.InstructorWorkspaceSnapshot{Session: rec, Worktrees: []domain.SessionWorktreeRecord{
			{SessionID: rec.ID, RepoName: domain.RootWorkspaceRepoName, WorktreePath: root},
			{SessionID: rec.ID, RepoName: "api", WorktreePath: child},
		}}, nil
	}}
	svc := &Service{manager: manager, store: store}
	err := svc.WithScopedWorkspace(context.Background(), InstructorWorkspaceScope{WorkID: "work", SessionID: rec.ID, InstanceID: "instance", ProjectID: rec.ProjectID}, func(ctx context.Context) error {
		file, err := svc.GetWorkspaceFile(ctx, rec.ID, "api/task.txt", "")
		if err != nil || file.Content != "owned child\n" {
			t.Fatalf("child lookup escaped capture: %+v %v", file, err)
		}
		_, err = svc.UpdateWorkspaceFile(ctx, rec.ID, UpdateWorkspaceFileInput{Path: "api/task.txt", Content: "must not write", ExpectedFileFingerprint: "stale"})
		var apiError *apierr.Error
		if !errors.As(err, &apiError) || apiError.Code != "WORKSPACE_FILE_STALE" {
			t.Fatalf("stale fingerprint error: %v", err)
		}
		return nil
	})
	if err != nil || manager.held {
		t.Fatalf("operation=%v lease=%v", err, manager.held)
	}
	content, err := os.ReadFile(filepath.Join(child, "task.txt"))
	if err != nil || string(content) != "owned child\n" {
		t.Fatalf("stale write had effect: %q %v", content, err)
	}
}

func TestScopedWorkspaceRefusesMissingUnknownAndBusyScopeBeforeCallback(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		missing, busy, unknown bool
		code                   string
	}{
		{name: "missing", missing: true, code: "INSTRUCTOR_WORKSPACE_SCOPE_REQUIRED"},
		{name: "busy", busy: true, code: "INSTRUCTOR_WORKSPACE_BUSY"},
		{name: "unknown instance", unknown: true, code: "INSTRUCTOR_WORKSPACE_SCOPE_CHANGED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := &workspaceLeaseManager{fakeCommander: &fakeCommander{}, busy: tc.busy}
			store := &workspaceCaptureStore{fakeStore: newFakeStore(), capture: func(context.Context, string, domain.SessionID, string, domain.ProjectID) (domain.InstructorWorkspaceSnapshot, error) {
				if !tc.unknown {
					t.Fatal("unexpected capture")
				}
				return domain.InstructorWorkspaceSnapshot{}, domain.ErrInstructorProducerChanged
			}}
			scope := InstructorWorkspaceScope{WorkID: "work", SessionID: "session", ProjectID: "project", InstanceID: "instance"}
			if tc.missing {
				scope.InstanceID = ""
			}
			err := (&Service{manager: manager, store: store}).WithScopedWorkspace(context.Background(), scope, func(context.Context) error { t.Fatal("refused scope reached I/O"); return nil })
			var apiError *apierr.Error
			if !errors.As(err, &apiError) || apiError.Code != tc.code || manager.held {
				t.Fatalf("error=%v lease=%v", err, manager.held)
			}
		})
	}
}

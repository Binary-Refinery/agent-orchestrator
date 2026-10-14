package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestInstructorWorkspaceCaptureRejectsReusedDisplayIDAndPinsRoots(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "scope")
	seedProject(t, s, "foreign")
	seed := sampleRecord("scope")
	seed.Metadata = domain.SessionMetadata{}
	original, err := s.CreateSession(ctx, seed)
	if err != nil {
		t.Fatal(err)
	}
	oldInstance, _, err := s.InstructorSessionInstance(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	work := domain.InstructorWork{ID: "scope-work", ProjectID: "scope", Source: "manual", SourceRef: "scope-test", Revision: 1}
	if _, err := s.CreateInstructorWork(ctx, work, domain.InstructorEvent{ID: "scope-created", WorkID: work.ID}); err != nil {
		t.Fatal(err)
	}
	work.Revision++
	work.SessionIDs = []domain.SessionID{original.ID}
	if saved, err := s.AttachInstructorSession(ctx, work, 1, domain.InstructorEvent{ID: "old-attached", WorkID: work.ID}, domain.InstructorSessionMembership{SessionID: original.ID, InstanceID: oldInstance, ProjectID: work.ProjectID, Provenance: "admitted"}); err != nil || !saved {
		t.Fatalf("attach old: %v %v", saved, err)
	}
	if _, err := s.CaptureInstructorWorkspace(ctx, work.ID, original.ID, oldInstance, work.ProjectID); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("seed workspace accepted: %v", err)
	}
	if deleted, err := s.DeleteSession(ctx, original.ID); err != nil || !deleted {
		t.Fatalf("delete seed: %v %v", deleted, err)
	}
	seed.Metadata.WorkspacePath = "/new-workspace"
	replacement, err := s.CreateSession(ctx, seed)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID != original.ID || !replacement.CreatedAt.Equal(original.CreatedAt) {
		t.Fatal("test must replace the same display ID and creation time")
	}
	newInstance, _, err := s.InstructorSessionInstance(ctx, replacement.ID)
	if err != nil || newInstance == oldInstance {
		t.Fatalf("new instance=%s old=%s error=%v", newInstance, oldInstance, err)
	}
	if _, err := s.CaptureInstructorWorkspace(ctx, work.ID, replacement.ID, newInstance, work.ProjectID); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("replacement inherited historical membership: %v", err)
	}
	work.Revision++
	if saved, err := s.AttachInstructorSession(ctx, work, 2, domain.InstructorEvent{ID: "new-attached", WorkID: work.ID}, domain.InstructorSessionMembership{SessionID: replacement.ID, InstanceID: newInstance, ProjectID: work.ProjectID, Provenance: "admitted"}); err != nil || !saved {
		t.Fatalf("attach new: %v %v", saved, err)
	}
	if err := s.UpsertSessionWorktree(ctx, domain.SessionWorktreeRecord{SessionID: replacement.ID, RepoName: "api", WorktreePath: "/new-workspace/api"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, work, instance string
		project              domain.ProjectID
	}{
		{"old instance", work.ID, oldInstance, work.ProjectID},
		{"unknown instance", work.ID, "unknown", work.ProjectID},
		{"different work", "foreign-work", newInstance, work.ProjectID},
		{"different project", work.ID, newInstance, "foreign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.CaptureInstructorWorkspace(ctx, tc.work, replacement.ID, tc.instance, tc.project); !errors.Is(err, domain.ErrInstructorProducerChanged) {
				t.Fatalf("scope accepted: %v", err)
			}
		})
	}
	captured, err := s.CaptureInstructorWorkspace(ctx, work.ID, replacement.ID, newInstance, work.ProjectID)
	if err != nil || captured.Session.Metadata.WorkspacePath != "/new-workspace" || len(captured.Worktrees) != 1 || captured.Worktrees[0].WorktreePath != "/new-workspace/api" {
		t.Fatalf("capture=%+v error=%v", captured, err)
	}
	if err := s.UpsertSessionWorktree(ctx, domain.SessionWorktreeRecord{SessionID: replacement.ID, RepoName: "api", WorktreePath: "/foreign-replacement/api"}); err != nil {
		t.Fatal(err)
	}
	if captured.Worktrees[0].WorktreePath != "/new-workspace/api" {
		t.Fatal("captured roots changed after a later row mutation")
	}
}

func TestInstructorWorkspaceCaptureRejectsUnprovedMembership(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "legacy")
	rec, err := s.CreateSession(ctx, sampleRecord("legacy"))
	if err != nil {
		t.Fatal(err)
	}
	instance, _, err := s.InstructorSessionInstance(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	work := domain.InstructorWork{ID: "legacy-work", ProjectID: "legacy", Source: "manual", SourceRef: "legacy", Revision: 1}
	if _, err := s.CreateInstructorWork(ctx, work, domain.InstructorEvent{ID: "legacy-created", WorkID: work.ID}); err != nil {
		t.Fatal(err)
	}
	work.Revision++
	work.SessionIDs = []domain.SessionID{rec.ID}
	if saved, err := s.AttachInstructorSession(ctx, work, 1, domain.InstructorEvent{ID: "legacy-attached", WorkID: work.ID}, domain.InstructorSessionMembership{SessionID: rec.ID, InstanceID: instance, ProjectID: work.ProjectID, Provenance: "legacy_bound"}); err != nil || !saved {
		t.Fatalf("attach legacy fixture: %v %v", saved, err)
	}
	if _, err := s.CaptureInstructorWorkspace(ctx, work.ID, rec.ID, instance, work.ProjectID); !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("legacy authority accepted: %v", err)
	}
}

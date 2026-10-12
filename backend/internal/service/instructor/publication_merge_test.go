package instructor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	projectsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/project"
)

func TestPublicationCannotBypassThroughAOMergeOrSessionReassignment(t *testing.T) {
	f := newInstructorFixture(t)
	if err := f.store.UpsertProject(context.Background(), domain.ProjectRecord{ID: "governed", Path: t.TempDir(), RepoOriginURL: "git@github.com:fork/repository.git", RegisteredAt: f.now}); err != nil {
		t.Fatal(err)
	}
	w := publicationReadyWork(t, f)
	for _, tc := range []struct {
		name   string
		pr     domain.PullRequest
		denied bool
	}{
		{"managed-session", domain.PullRequest{SessionID: w.SessionIDs[0]}, true},
		{"reassigned-session", domain.PullRequest{Repo: "fork/repository", Host: "github.com", SourceBranch: w.Branch, HeadSHA: w.SHA}, true},
		{"default-github-host", domain.PullRequest{Repo: "fork/repository", SourceBranch: w.Branch, HeadSHA: w.SHA}, true},
		{"changed-head", domain.PullRequest{Repo: "fork/repository", SourceBranch: w.Branch, HeadSHA: "2222222222222222222222222222222222222222"}, true},
		{"unknown-branch", domain.PullRequest{Repo: "fork/repository", HeadSHA: "2222222222222222222222222222222222222222"}, true},
		{"unmanaged-branch", domain.PullRequest{Repo: "fork/repository", SourceBranch: "legacy/other", HeadSHA: "2222222222222222222222222222222222222222"}, false},
		{"other-repository", domain.PullRequest{Repo: "fork/another", SourceBranch: w.Branch, HeadSHA: w.SHA}, false},
		{"other-host", domain.PullRequest{Repo: "fork/repository", Host: "gitlab.example", SourceBranch: w.Branch, HeadSHA: w.SHA}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.service.ValidatePRMerge(context.Background(), tc.pr)
			if tc.denied && !errors.Is(err, instructorsvc.ErrPublicationUnavailable) || !tc.denied && err != nil {
				t.Fatalf("denied=%v err=%v", tc.denied, err)
			}
		})
	}
}

func TestPublicationMergeAssociationSurvivesRealProjectConfigChanges(t *testing.T) {
	for _, source := range []string{"registered-repository", "legacy-pinned-policy", "legacy-verified-pr"} {
		t.Run(source, func(t *testing.T) {
			f := newInstructorFixture(t)
			ctx := context.Background()
			project := domain.ProjectRecord{ID: "governed", Path: t.TempDir(), RepoOriginURL: "git@github.com:fork/repository.git", RegisteredAt: f.now, Config: domain.ProjectConfig{GovernanceManaged: true, CanonicalRepoURL: "https://github.com/upstream/repository"}}
			if err := f.store.UpsertProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			w := publicationReadyWork(t, f)
			w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
				if source != "registered-repository" {
					w.PublicationRepositories = nil
				}
				if source == "legacy-pinned-policy" {
					w.Policy.Traceability.AllowedRepositories = []string{"upstream/repository"}
				}
				if source == "legacy-verified-pr" {
					w.Evidence = append(w.Evidence, domain.InstructorEvidence{ID: "verified-historical-pr", Kind: "traceability", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Ref: "https://github.com/upstream/repository/pull/7", Verified: true})
				}
			})
			pr := domain.PullRequest{URL: "https://github.com/upstream/repository/pull/7", Repo: "upstream/repository", Host: "github.com", SourceBranch: w.Branch, HeadSHA: w.SHA}
			if err := f.service.ValidatePRMerge(ctx, pr); !errors.Is(err, instructorsvc.ErrPublicationUnavailable) {
				t.Fatalf("initial governed association not denied: %v", err)
			}
			if _, err := projectsvc.New(f.store).SetConfig(ctx, "governed", projectsvc.SetConfigInput{Config: domain.ProjectConfig{GovernanceManaged: true}}); err != nil {
				t.Fatal(err)
			}
			if err := f.service.ValidatePRMerge(ctx, pr); !errors.Is(err, instructorsvc.ErrPublicationUnavailable) {
				t.Fatalf("mutable canonicalRepoURL erased durable governance: %v", err)
			}
			pr.HeadSHA, pr.SessionID = "2222222222222222222222222222222222222222", "reassigned-unmanaged"
			if err := f.service.ValidatePRMerge(ctx, pr); !errors.Is(err, instructorsvc.ErrPublicationUnavailable) {
				t.Fatalf("changed head/owner erased durable governance: %v", err)
			}
		})
	}
}

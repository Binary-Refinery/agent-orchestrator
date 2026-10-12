package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestInstructorIssueRepositoryCannotQueryAnUnregisteredOrDifferentHost(t *testing.T) {
	for _, tc := range []struct {
		name    string
		project domain.ProjectRecord
		want    string
	}{
		{"ssh origin", domain.ProjectRecord{RepoOriginURL: "git@github.com:Owner/Repo.git"}, "owner/repo"},
		{"canonical upstream", domain.ProjectRecord{RepoOriginURL: "https://github.com/Fork/Repo", Config: domain.ProjectConfig{CanonicalRepoURL: "https://github.com/Owner/Repo"}}, "owner/repo"},
		{"no origin", domain.ProjectRecord{Config: domain.ProjectConfig{CanonicalRepoURL: "https://github.com/Owner/Repo"}}, ""},
		{"archived", domain.ProjectRecord{RepoOriginURL: "https://github.com/Owner/Repo", ArchivedAt: time.Now()}, ""},
		{"enterprise host", domain.ProjectRecord{RepoOriginURL: "git@github.enterprise.test:Owner/Repo.git"}, ""},
		{"different canonical host", domain.ProjectRecord{RepoOriginURL: "https://github.com/Owner/Repo", Config: domain.ProjectConfig{CanonicalRepoURL: "https://gitlab.com/Owner/Repo"}}, ""},
		{"invalid path segment", domain.ProjectRecord{RepoOriginURL: "https://github.com/Owner/Repo;injected"}, ""},
		{"owner path too long", domain.ProjectRecord{RepoOriginURL: "https://github.com/" + strings.Repeat("a", 40) + "/Repo"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.InstructorGitHubRepository(tc.project)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("unusable repository accepted: %q", got)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("repository=%q err=%v", got, err)
			}
		})
	}
}

func TestInstructorIssueIdentityPreservesOriginalRepositoryAndRejectsAmbiguousNumbers(t *testing.T) {
	owner := domain.InstructorPublicationRepository{Host: "github.com", Repository: "owner/repo"}
	fork := domain.InstructorPublicationRepository{Host: "github.com", Repository: "fork/repo"}
	for _, tc := range []struct {
		name string
		work domain.InstructorWork
		want bool
	}{
		{"full reference", domain.InstructorWork{IssueID: "OWNER/Repo#42"}, true},
		{"full URL", domain.InstructorWork{IssueID: "https://github.com/owner/repo/issues/42"}, true},
		{"different repository", domain.InstructorWork{IssueID: "fork/repo#42"}, false},
		{"untrusted URL host", domain.InstructorWork{IssueID: "https://github.com.attacker.test/owner/repo/issues/42"}, false},
		{"pull request URL", domain.InstructorWork{IssueID: "https://github.com/owner/repo/pull/42"}, false},
		{"bare without captured repo", domain.InstructorWork{IssueID: "#42"}, false},
		{"bare captured repo", domain.InstructorWork{IssueID: "#42", PublicationRepositories: []domain.InstructorPublicationRepository{owner}}, true},
		{"bare repeated same repo", domain.InstructorWork{IssueID: "#42", PublicationRepositories: []domain.InstructorPublicationRepository{owner, owner}}, true},
		{"bare fork and upstream ambiguous", domain.InstructorWork{IssueID: "#42", PublicationRepositories: []domain.InstructorPublicationRepository{owner, fork}}, false},
		{"bare issue with explicit source", domain.InstructorWork{IssueID: "#42", Source: "issue", SourceRef: "owner/repo#42", PublicationRepositories: []domain.InstructorPublicationRepository{owner, fork}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			number, valid := domain.InstructorWorkGitHubIssue(tc.work, "owner/repo")
			if valid != tc.want || valid && number != 42 {
				t.Fatalf("number=%d valid=%v", number, valid)
			}
		})
	}
}

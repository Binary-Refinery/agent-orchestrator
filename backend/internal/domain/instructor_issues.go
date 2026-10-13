package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrInstructorIntakeChanged fences a registered project changed during issue intake.
var ErrInstructorIntakeChanged = errors.New("registered project or issue association changed during intake")

// InstructorIssueCandidate is external issue data, never executable authority.
type InstructorIssueCandidate struct {
	Number        int                  `json:"number"`
	Title         string               `json:"title"`
	Body          string               `json:"body"`
	URL           string               `json:"url"`
	State         NormalizedIssueState `json:"state"`
	Labels        []string             `json:"labels"`
	Assignees     []string             `json:"assignees"`
	LinkedWorkID  string               `json:"linkedWorkId,omitempty"`
	BodyTruncated bool                 `json:"bodyTruncated"`
}

// InstructorIssueCandidates is a bounded, explicitly dated repository observation.
type InstructorIssueCandidates struct {
	ProjectID  ProjectID                  `json:"projectId"`
	Repository string                     `json:"repository"`
	CapturedAt time.Time                  `json:"capturedAt"`
	Issues     []InstructorIssueCandidate `json:"issues"`
	Truncated  bool                       `json:"truncated"`
}

// InstructorGitHubRepository resolves only the registered checkout and its
// explicitly configured canonical repository. The GitHub tracker currently serves
// github.com only; enterprise/self-managed hosts must not silently use that API.
func InstructorGitHubRepository(project ProjectRecord) (string, error) {
	if !project.ArchivedAt.IsZero() {
		return "", errors.New("archived projects cannot intake issues")
	}
	origin, err := ParseRepositoryIdentity(project.RepoOriginURL)
	if err != nil || origin.Provider != "github" || origin.Host != "github.com" {
		return "", errors.New("project needs a registered github.com repository")
	}
	repo := origin
	if project.Config.CanonicalRepoURL != "" {
		if err := project.Config.ValidateCanonicalRepository(project.RepoOriginURL); err != nil {
			return "", err
		}
		repo, err = ParseRepositoryIdentity(project.Config.CanonicalRepoURL)
		if err != nil {
			return "", err
		}
	}
	for _, part := range []string{repo.Namespace, repo.Name} {
		if strings.IndexFunc(part, func(r rune) bool {
			allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
			return !allowed
		}) >= 0 {
			return "", errors.New("project repository has invalid GitHub path segments")
		}
	}
	if len(repo.Namespace) > 39 || len(repo.Name) > 100 {
		return "", errors.New("project repository exceeds GitHub path limits")
	}
	return strings.ToLower(repo.Namespace + "/" + repo.Name), nil
}

// InstructorWorkGitHubIssue resolves an issue against immutable work references.
// Bare historical numbers require one unambiguous repository captured when the
// work was created; today's mutable project configuration cannot establish their
// original identity. A captured fork and upstream cannot disambiguate a number.
func InstructorWorkGitHubIssue(work InstructorWork, repository string) (int, bool) {
	raw := strings.TrimSpace(string(work.IssueID))
	if work.Source == "issue" && (raw == "" || !strings.Contains(raw, "://") && !strings.Contains(strings.TrimPrefix(raw, "#"), "#")) {
		raw = work.SourceRef
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return 0, false
		}
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) != 4 || parts[2] != "issues" || !strings.EqualFold(parts[0]+"/"+parts[1], repository) {
			return 0, false
		}
		raw = parts[3]
	} else if before, after, ok := strings.Cut(raw, "#"); ok && before != "" {
		if !strings.EqualFold(before, repository) {
			return 0, false
		}
		raw = after
	} else {
		captured := map[string]bool{}
		for _, repo := range work.PublicationRepositories {
			captured[strings.ToLower(repo.Host+"/"+repo.Repository)] = true
		}
		if len(captured) != 1 || !captured["github.com/"+strings.ToLower(repository)] {
			return 0, false
		}
		raw = strings.TrimPrefix(raw, "#")
	}
	number, err := strconv.Atoi(raw)
	return number, err == nil && number > 0
}

// InstructorIssueReference constructs the stable GitHub issue origin.
func InstructorIssueReference(repository string, number int) string {
	return fmt.Sprintf("%s#%d", strings.ToLower(repository), number)
}

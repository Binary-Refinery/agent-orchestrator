package instructor

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ValidatePRMerge prevents AO's ordinary SCM endpoint from bypassing governed
// publication. A protected merge must use ExecutePublication instead, never
// the host credential-bearing SCM adapter. Unmanaged PR behavior is preserved.
func (s *Service) ValidatePRMerge(ctx context.Context, pr domain.PullRequest) error {
	if pr.SessionID != "" {
		_, managed, err := s.deps.Store.InstructorWorkForSession(ctx, pr.SessionID)
		if err != nil {
			return err
		}
		if managed {
			return fmt.Errorf("%w: governed pull requests require the controlled publication executor", ErrPublicationUnavailable)
		}
	}
	// Reassigning a PR to an unmanaged session cannot erase the repository and
	// branch association of a durable work or a historical publication grant.
	works, err := s.deps.Store.ListInstructorWorks(ctx)
	if err != nil {
		return err
	}
	projects, ok := s.deps.Store.(interface {
		GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	})
	prHost := strings.ToLower(pr.Host)
	if prHost == "" && (pr.Provider == "" || pr.Provider == "github") {
		prHost = "github.com"
	}
	for _, w := range works {
		// Verified historical PR evidence remains an association even when
		// project defaults, the work's current SHA, or PR ownership later change.
		for _, e := range w.Evidence {
			if e.WorkID == w.ID && e.PolicyVersion != "" && fullSHA(e.SHA) && (e.Verified || e.Revoked) && e.Ref == pr.URL && pr.URL != "" {
				return fmt.Errorf("%w: pull request belongs to historical governed work %s", ErrPublicationUnavailable, w.ID)
			}
		}
		branchMatch := pr.SourceBranch != "" && w.Branch == pr.SourceBranch || pr.SourceBranch == "" && w.Branch != "" || fullSHA(w.SHA) && strings.EqualFold(w.SHA, pr.HeadSHA)
		for _, g := range w.PublicationGrants {
			if pr.SourceBranch != "" && g.Branch == pr.SourceBranch {
				branchMatch = true
			}
		}
		if !branchMatch {
			continue
		}
		for _, repo := range w.PublicationRepositories {
			if strings.EqualFold(repo.Host, prHost) && strings.EqualFold(repo.Repository, pr.Repo) {
				return fmt.Errorf("%w: repository branch belongs to pinned governed work %s", ErrPublicationUnavailable, w.ID)
			}
		}
		// The existing traceability policy's owner/repository contract is
		// GitHub-specific; do not assign its bare names to another host.
		if prHost == "github.com" {
			for _, repo := range w.Policy.Traceability.AllowedRepositories {
				if strings.EqualFold(repo, pr.Repo) {
					return fmt.Errorf("%w: repository branch belongs to pinned work policy %s", ErrPublicationUnavailable, w.ID)
				}
			}
		}
		// Independently verified PR references also pin the destination of
		// older works registered before immutable repository bindings existed.
		for _, e := range w.Evidence {
			if e.WorkID != w.ID || e.PolicyVersion == "" || !fullSHA(e.SHA) || !e.Verified && !e.Revoked {
				continue
			}
			parsed, err := url.Parse(e.Ref)
			if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != "https" {
				continue
			}
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(parts) == 4 && parts[2] == "pull" && strings.EqualFold(parsed.Host, prHost) && strings.EqualFold(parts[0]+"/"+parts[1], pr.Repo) {
				return fmt.Errorf("%w: repository branch belongs to verified historical work %s", ErrPublicationUnavailable, w.ID)
			}
		}
		if !ok {
			continue
		}
		project, found, err := projects.GetProject(ctx, string(w.ProjectID))
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		for _, raw := range []string{project.RepoOriginURL, project.Config.CanonicalRepoURL} {
			repo, parseErr := domain.ParseRepositoryIdentity(raw)
			if parseErr == nil && strings.EqualFold(repo.Host, prHost) && strings.EqualFold(repo.Namespace+"/"+repo.Name, pr.Repo) {
				return fmt.Errorf("%w: repository branch belongs to governed work %s; use the controlled publication executor", ErrPublicationUnavailable, w.ID)
			}
		}
	}
	return nil
}

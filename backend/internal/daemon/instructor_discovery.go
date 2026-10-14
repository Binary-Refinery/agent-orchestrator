package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

// Discover produces candidates from actual AO stores and immutable worktree
// artifacts. The service independently verifies each; discovery grants nothing.
func (v instructorEvidenceVerifier) Discover(ctx context.Context, w domain.InstructorWork) ([]domain.InstructorEvidence, error) {
	if len(w.SHA) != 40 {
		return nil, nil
	}
	candidates := []domain.InstructorEvidence{}
	seen := map[string]bool{}
	add := func(e domain.InstructorEvidence) {
		key := e.Kind + "\x00" + e.Ref + "\x00" + string(e.Producer)
		if seen[key] {
			return
		}
		seen[key] = true
		e.WorkID, e.PolicyVersion, e.SHA = w.ID, w.PolicyVersion, w.SHA
		e.Verified = false
		candidates = append(candidates, e)
	}
	// Historical claimed paths remain discoverable at the new exact revision;
	// native checks and human grants are exclusively owned by their executors.
	for _, e := range w.Evidence {
		if e.SHA == w.SHA && e.PolicyVersion == w.PolicyVersion && e.Kind != "check" && !strings.HasPrefix(e.Ref, "gate:") && e.Kind != "delta" && e.Kind != "review-report" && e.Kind != "triage" {
			add(e)
		}
	}
	latest := map[int]domain.ReviewRun{}
	launches := map[string]domain.InstructorLaunch{}
	for _, launch := range w.Launches {
		if launch.Status != "launched" || launch.ReviewRunID == "" || launch.SHA != w.SHA {
			continue
		}
		run, found, err := v.store.GetReviewRun(ctx, launch.ReviewRunID)
		if err != nil {
			return nil, err
		}
		if !found || run.TargetSHA != w.SHA || run.ReviewID != launch.ReviewID || (run.Status != domain.ReviewRunComplete && run.Status != domain.ReviewRunDelivered) {
			continue
		}
		sequence := int(launch.PhaseSequence)
		if prior, exists := latest[sequence]; !exists || run.CreatedAt.After(prior.CreatedAt) {
			latest[sequence] = run
			launches[run.ID] = launch
		}
	}
	ordered := make([]domain.ReviewRun, 0, len(latest))
	for _, run := range latest {
		ordered = append(ordered, run)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})
	for _, run := range ordered {
		launch := launches[run.ID]
		e := domain.InstructorEvidence{Kind: "review-report", Producer: domain.SessionID(run.ReviewID), Role: launch.Role, Harness: launch.Requested.Harness, Model: launch.Requested.Model, Effort: launch.Requested.Effort, Ref: run.ID}
		add(e)
		if launch.Phase == "delta" && launch.PhaseSequence == w.DeltaPhaseSequence && run.Verdict == domain.VerdictApproved {
			e.Kind = "delta"
			add(e)
			e.Kind = "triage"
			add(e)
		}
	}
	for _, id := range w.SessionIDs {
		worker := false
		for _, launch := range w.Launches {
			if launch.SessionID == id && launch.Role == "worker" && launch.Status == "launched" {
				worker = true
			}
		}
		if !worker {
			continue
		}
		base := domain.InstructorEvidence{Producer: id, Role: "worker"}
		session, found, err := v.store.GetSession(ctx, id)
		if err != nil {
			return nil, err
		}
		if found {
			native := base
			native.Harness, native.Model, native.Effort = string(session.Harness), session.Metadata.Model, session.Metadata.Effort
			native.Kind, native.Ref = "attribution", w.SHA
			add(native)
			path, err := v.attributionWorkspace(ctx, w, native)
			if err == nil {
				ref := fmt.Sprintf(".ao/task-packets/%s.md", w.ID)
				if content, readErr := instructorReadGit(ctx, path, "show", w.SHA+":"+ref); readErr == nil && strings.TrimSpace(string(content)) != "" {
					native.Kind, native.Ref = "task-packet", ref
					add(native)
				}
			}
		}
		prs, err := v.store.ListPRsBySession(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, pr := range prs {
			if pr.HeadSHA != w.SHA {
				continue
			}
			base.Kind, base.Ref = "ci", pr.URL
			add(base)
			checks, err := v.store.ListChecks(ctx, pr.URL)
			if err != nil {
				return nil, err
			}
			if instructorFailedCI(checks, w.SHA) {
				base.Kind = "ci-failed"
				add(base)
			}
			base.Kind = "traceability"
			add(base)
		}
	}
	return candidates, nil
}

// VerificationKey reads cheap persisted provider facts only. A newly completed
// review or changed CI conclusion wakes deep verification without polling git
// or GitHub on every Status request.
func (v instructorEvidenceVerifier) VerificationKey(ctx context.Context, w domain.InstructorWork) (string, error) {
	facts := []string{}
	appendFact := func(value any) error {
		data, err := json.Marshal(value)
		if err == nil {
			facts = append(facts, string(data))
		}
		return err
	}
	if err := appendFact(struct{ PublicationApproved bool }{instructorsvc.HasPublicationGrant(w)}); err != nil {
		return "", err
	}
	for _, evidence := range w.Evidence {
		if evidence.SHA != w.SHA || evidence.PolicyVersion != w.PolicyVersion || evidence.Kind == "check" || strings.HasPrefix(evidence.Ref, "gate:") {
			continue
		}
		// Claim identity excludes generated IDs, attestation and timestamps so
		// re-verifying it cannot manufacture another verification budget.
		if err := appendFact(struct{ Kind, Ref, Producer, Role, Harness, Model, Effort string }{evidence.Kind, evidence.Ref, string(evidence.Producer), evidence.Role, evidence.Harness, evidence.Model, evidence.Effort}); err != nil {
			return "", err
		}
	}
	for _, launch := range w.Launches {
		if launch.Status == "launched" || launch.Status == "sent" || launch.Status == "native-config-reported" {
			if err := appendFact(struct {
				SessionID       domain.SessionID
				Role            string
				Requested       domain.InstructorAgentSelection
				Sent, Confirmed *domain.InstructorAgentSelection
				PhaseSequence   int64
			}{launch.SessionID, launch.Role, launch.Requested, launch.Sent, launch.Confirmed, launch.PhaseSequence}); err != nil {
				return "", err
			}
		}
		if launch.ReviewRunID == "" || launch.SHA != w.SHA {
			continue
		}
		run, found, err := v.store.GetReviewRun(ctx, launch.ReviewRunID)
		if err != nil {
			return "", err
		}
		if found {
			if err := appendFact(struct{ ID, ReviewID, SessionID, SHA, Status, Verdict, Body, Scope string }{run.ID, run.ReviewID, string(run.SessionID), run.TargetSHA, string(run.Status), string(run.Verdict), run.Body, run.ScopeKey}); err != nil {
				return "", err
			}
		}
	}
	for _, id := range w.SessionIDs {
		session, found, err := v.store.GetSession(ctx, id)
		if err != nil {
			return "", err
		}
		if err := appendFact(struct {
			SessionID     domain.SessionID
			Found         bool
			Harness       domain.AgentHarness
			Model, Effort string
		}{id, found, session.Harness, session.Metadata.Model, session.Metadata.Effort}); err != nil {
			return "", err
		}
		prs, err := v.store.ListPRsBySession(ctx, id)
		if err != nil {
			return "", err
		}
		for _, pr := range prs {
			if pr.HeadSHA != w.SHA {
				continue
			}
			if err := appendFact(struct {
				URL, Provider, Host, Repo, ProviderID, Head, Source, Target, CI, Review string
				Draft, Merged, Closed                                                   bool
			}{pr.URL, pr.Provider, pr.Host, pr.Repo, pr.ProviderID, pr.HeadSHA, pr.SourceBranch, pr.TargetBranch, string(pr.CI), string(pr.Review), pr.Draft, pr.Merged, pr.Closed}); err != nil {
				return "", err
			}
			checks, err := v.store.ListChecks(ctx, pr.URL)
			if err != nil {
				return "", err
			}
			for _, check := range checks {
				if check.CommitHash != w.SHA {
					continue
				}
				if err := appendFact(struct{ Name, SHA, Status, Conclusion, URL string }{check.Name, check.CommitHash, string(check.Status), check.Conclusion, check.URL}); err != nil {
					return "", err
				}
			}
		}
	}
	sort.Strings(facts)
	unique := facts[:0]
	for _, fact := range facts {
		if len(unique) == 0 || unique[len(unique)-1] != fact {
			unique = append(unique, fact)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(unique, "\x00")))
	return hex.EncodeToString(sum[:]), nil
}

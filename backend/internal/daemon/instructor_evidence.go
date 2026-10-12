package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type instructorEvidenceStore interface {
	GetReviewRun(context.Context, string) (domain.ReviewRun, bool, error)
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error)
	ListChecks(context.Context, string) ([]domain.PullRequestCheck, error)
}

// instructorEvidenceVerifier resolves claims against daemon-owned review/SCM
// facts or an immutable committed artifact. A report's "verified" flag is never
// authority. Checks are produced by the separate bounded profile executor.
type instructorEvidenceVerifier struct{ store instructorEvidenceStore }

func evidenceInvalid(message string) error {
	return fmt.Errorf("%w: %s", instructorsvc.ErrValidation, message)
}

// Assess derives the review outcome from the actual recorded pass. Client
// supplied outcomes and finding counts never certify a corrective workflow.
func (v instructorEvidenceVerifier) Assess(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence) (domain.InstructorEvidence, error) {
	evidence.Outcome = ""
	evidence.CriticalFindings = 0
	if err := v.Verify(ctx, work, evidence); err != nil {
		return evidence, err
	}
	if evidence.Kind == "ci-failed" {
		evidence.Outcome = "failed"
	}
	if evidence.Kind == "review-report" || evidence.Kind == "triage" || evidence.Kind == "delta" {
		run, _, err := v.store.GetReviewRun(ctx, evidence.Ref)
		if err != nil {
			return evidence, err
		}
		evidence.Outcome = string(run.Verdict)
		for _, launch := range work.Launches {
			if launch.ReviewRunID == run.ID && launch.ReviewID == run.ReviewID && launch.SHA == run.TargetSHA && launch.Status == "launched" {
				evidence.Phase, evidence.PhaseSequence = launch.Phase, launch.PhaseSequence
				break
			}
		}
		if run.Verdict == domain.VerdictChangesRequested {
			// Existing AO review text is unstructured. Conservatively retain an
			// unresolved critical blocker until a genuine approved delta review.
			evidence.CriticalFindings = 1
		}
	}
	return evidence, nil
}

func workHasSession(work domain.InstructorWork, id domain.SessionID) bool {
	for _, candidate := range work.SessionIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

func (v instructorEvidenceVerifier) Verify(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence) error {
	if evidence.WorkID != work.ID || evidence.PolicyVersion != work.PolicyVersion || evidence.SHA != work.SHA || len(work.SHA) != 40 {
		return evidenceInvalid("evidence does not match the exact work, policy and commit")
	}
	switch evidence.Kind {
	case "review-report", "delta", "triage":
		return v.verifyReview(ctx, work, evidence)
	case "task-packet":
		return v.verifyCommittedArtifact(ctx, work, evidence)
	case "ci", "ci-failed":
		return v.verifyCI(ctx, work, evidence)
	case "attribution":
		return v.verifyAttribution(ctx, work, evidence)
	case "traceability":
		return v.verifyTraceability(ctx, work, evidence)
	default:
		return evidenceInvalid("no trusted verifier for evidence kind " + evidence.Kind)
	}
}

func (v instructorEvidenceVerifier) verifyReview(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence) error {
	run, found, err := v.store.GetReviewRun(ctx, evidence.Ref)
	if err != nil {
		return err
	}
	if !found || !workHasSession(work, run.SessionID) || run.TargetSHA != work.SHA {
		return evidenceInvalid("review is not bound to a worker of this work at its exact commit")
	}
	if (run.Status != domain.ReviewRunComplete && run.Status != domain.ReviewRunDelivered) || (run.Verdict != domain.VerdictApproved && run.Verdict != domain.VerdictChangesRequested) {
		return evidenceInvalid("independent review has not completed for this commit")
	}
	if evidence.Kind == "delta" && run.Verdict != domain.VerdictApproved {
		return evidenceInvalid("independent delta review has not approved this commit")
	}
	if run.ReviewID == "" || run.ReviewID == string(run.SessionID) || evidence.Producer != domain.SessionID(run.ReviewID) {
		return evidenceInvalid("evidence producer must be the independent recorded reviewer")
	}
	role, found := work.Policy.Roles[evidence.Role]
	if !found || !role.Independent || role.Harness != string(run.Harness) {
		return evidenceInvalid("review role must be independent and match the actual reviewer harness")
	}
	for _, launch := range work.Launches {
		if launch.ReviewID != run.ReviewID || launch.ReviewRunID != run.ID || launch.SHA != run.TargetSHA || launch.Role != evidence.Role || launch.Status != "launched" {
			continue
		}
		if evidence.Kind == "delta" {
			if launch.Phase != "delta" || launch.PhaseSequence != work.DeltaPhaseSequence || launch.PhaseSequence == 0 {
				return evidenceInvalid("delta approval must belong to the current distinct delta phase")
			}
			if run.ScopeKey != fmt.Sprintf("%s/delta/%d", work.ID, work.DeltaPhaseSequence) {
				return evidenceInvalid("delta pass does not belong to its durable review scope")
			}
			// A later completed negative pass at the same commit invalidates a
			// historical approval. SHA equality alone cannot clear findings.
			for _, candidate := range work.Launches {
				if candidate.ReviewRunID == "" || candidate.SHA != work.SHA || candidate.Status != "launched" {
					continue
				}
				other, present, err := v.store.GetReviewRun(ctx, candidate.ReviewRunID)
				if err != nil {
					return err
				}
				if present && other.CreatedAt.After(run.CreatedAt) && (other.Status == domain.ReviewRunComplete || other.Status == domain.ReviewRunDelivered) {
					return evidenceInvalid("a newer completed review supersedes this delta approval")
				}
			}
		}
		selection := launch.Requested
		if selection.Harness != string(run.Harness) || selection.Model == "" || selection.Effort == "" || launch.Sent == nil || *launch.Sent != selection {
			continue
		}
		if evidence.Harness != selection.Harness || evidence.Model != selection.Model || evidence.Effort != selection.Effort {
			return evidenceInvalid("review evidence differs from the recorded model configuration")
		}
		expected := role.Effort
		if expected == "maximum" {
			expected = role.EffortByModel[selection.Model]
		}
		if expected != "configured" && expected != selection.Effort {
			return evidenceInvalid("review effort does not satisfy the pinned policy")
		}
		return nil
	}
	return evidenceInvalid("review has no matching successful managed launch provenance")
}

func (v instructorEvidenceVerifier) verifyCommittedArtifact(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence) error {
	if !workHasSession(work, evidence.Producer) || evidence.Ref == "" || filepath.IsAbs(evidence.Ref) {
		return evidenceInvalid("artifact must be repository relative and produced by a work member")
	}
	clean := filepath.ToSlash(filepath.Clean(evidence.Ref))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsAny(clean, "\x00\r\n") {
		return evidenceInvalid("artifact path is invalid")
	}
	session, found, err := v.store.GetSession(ctx, evidence.Producer)
	if err != nil {
		return err
	}
	if !found || session.ProjectID != work.ProjectID || session.Metadata.WorkspacePath == "" {
		return evidenceInvalid("artifact producer has no matching workspace")
	}
	data, err := instructorReadGit(ctx, session.Metadata.WorkspacePath, "show", work.SHA+":"+clean)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return evidenceInvalid("artifact is absent from the exact commit or empty")
	}
	return nil
}

func (v instructorEvidenceVerifier) verifyCI(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence) error {
	if !instructorsvc.HasPublicationGrant(work) {
		return evidenceInvalid("remote CI requires the exact recorded publication approval")
	}
	if !workHasSession(work, evidence.Producer) {
		return evidenceInvalid("CI producer is not a work member")
	}
	prs, err := v.store.ListPRsBySession(ctx, evidence.Producer)
	if err != nil {
		return err
	}
	for _, pr := range prs {
		if pr.URL != evidence.Ref || pr.HeadSHA != work.SHA {
			continue
		}
		checks, err := v.store.ListChecks(ctx, pr.URL)
		if err != nil {
			return err
		}
		if len(checks) == 0 {
			return evidenceInvalid("no provider checks observed for this commit")
		}
		if evidence.Kind == "ci-failed" {
			if !instructorFailedCI(checks, work.SHA) {
				return evidenceInvalid("no failed provider check observed for the exact commit")
			}
			return nil
		}
		passed := false
		for _, check := range checks {
			// The provider cache retains checks for previous PR commits. Those
			// neither certify nor invalidate this exact head's checks.
			if check.CommitHash != work.SHA {
				continue
			}
			switch check.Status {
			case domain.PRCheckPassed:
				if check.Conclusion == "success" || check.Conclusion == "neutral" {
					passed = true
					continue
				}
			case domain.PRCheckSkipped:
				// The normalized skipped status also includes stale checks and
				// unexecuted manual jobs. Only a native skipped conclusion is
				// harmless alongside an actually passed provider check.
				if check.Conclusion == "skipped" {
					continue
				}
			}
			return evidenceInvalid("provider checks are missing, pending or unsuccessful for the exact commit")
		}
		if !passed {
			return evidenceInvalid("no passed provider check observed for the exact commit")
		}
		return nil
	}
	return evidenceInvalid("CI reference is not attached to this work at its exact commit")
}

func instructorFailedCI(checks []domain.PullRequestCheck, sha string) bool {
	for _, check := range checks {
		if check.CommitHash != sha {
			continue
		}
		switch check.Conclusion {
		case "failure", "timed_out", "cancelled", "action_required", "startup_failure", "stale":
			if check.Status != domain.PRCheckQueued && check.Status != domain.PRCheckInProgress {
				return true
			}
		}
	}
	return false
}

// ObservedPublication reports persisted SCM facts, never client evidence claims.
func (v instructorEvidenceVerifier) ObservedPublication(ctx context.Context, work domain.InstructorWork) (string, bool, error) {
	for _, id := range work.SessionIDs {
		prs, err := v.store.ListPRsBySession(ctx, id)
		if err != nil {
			return "", false, err
		}
		for _, pr := range prs {
			if pr.URL != "" && pr.HeadSHA == work.SHA && len(work.SHA) == 40 {
				return pr.URL, true, nil
			}
		}
	}
	return "", false, nil
}

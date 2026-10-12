package instructor

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// PublicationObserver reads actual remote/store observations independently of
// evidence claims. A client-provided PR URL never establishes a violation.
type PublicationObserver interface {
	ObservedPublication(context.Context, domain.InstructorWork) (string, bool, error)
}

func (s *Service) fenceUnauthorizedPublication(ctx context.Context, w domain.InstructorWork) error {
	observer, ok := s.deps.Verifier.(PublicationObserver)
	if !ok || HasPublicationGrant(w) {
		return nil
	}
	ref, observed, err := observer.ObservedPublication(ctx, w)
	if err != nil || !observed {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(ctx, w.ID)
	if err != nil {
		return err
	}
	if HasPublicationGrant(current) || current.SHA != w.SHA || current.PolicyVersion != w.PolicyVersion || current.Branch != w.Branch {
		return nil
	}
	if current.Gate != nil && current.Gate.Kind == "publication_violation" {
		return nil
	}
	if current.Gate != nil {
		cloned := *current.Gate
		current.SuspendedGate = &cloned
	}
	current.Gate = &domain.InstructorGate{ID: "violation-" + s.deps.NewID(), Kind: "publication_violation", Title: "Veröffentlichung ohne passende menschliche Freigabe beobachtet", OpenedAt: s.now()}
	if current.State != "parked" {
		current.State = "blocked"
	}
	current.Summary = current.Gate.Title
	current.WaitCondition = "Beobachtete Veröffentlichung " + ref + " für SHA " + current.SHA + "; nachträgliches Go autorisiert keine frühere Veröffentlichung"
	_, err = s.save(ctx, current, current.Revision, "policy_rejected", "publication without exact grant: "+ref+" sha="+current.SHA)
	return err
}

// HasPublicationGrant is the durable authorization check. Remote publication
// observations must separately establish what actually happened after approval.
func HasPublicationGrant(w domain.InstructorWork) bool {
	for _, g := range w.PublicationGrants {
		if validPublicationGrant(g, w.ID) && g.PolicyVersion == w.PolicyVersion && g.SHA == w.SHA && g.Branch == w.Branch {
			return true
		}
	}
	return false
}

func validPublicationGrant(g domain.InstructorPublicationGrant, workID string) bool {
	return g.WorkID == workID && g.PolicyVersion != "" && fullSHA(g.SHA) && g.Branch != "" &&
		g.GateID != "" && g.SnapshotID != "" && g.WorkRevision > 0 &&
		g.Approval == "PUBLISH-GO "+g.Branch && !g.ApprovedAt.IsZero()
}

func hasPublicationHistory(w domain.InstructorWork) bool {
	for _, g := range w.PublicationGrants {
		if validPublicationGrant(g, w.ID) {
			return true
		}
	}
	return false
}

// publicationReadiness verifies the local review/check chain before its first
// publication. Remote CI and board/PR traceability become enforceable afterwards.
func publicationReadiness(w domain.InstructorWork) error {
	if w.Phase != "closeout" {
		return invalid("publication requires the closeout phase")
	}
	if w.Branch == "" {
		return invalid("publication requires an exact branch")
	}
	if w.OpenCriticalFindings > 0 || !fullSHA(w.SHA) {
		return invalid("publication requires a full commit SHA and no P0/P1 findings")
	}
	if hasEvidence(w, "ci-failed") {
		return invalid("the current remote CI failure requires the governed correction loop")
	}
	for _, need := range w.Policy.CloseoutRequires {
		if need == "ci" || need == "traceability" {
			continue
		}
		if !hasEvidence(w, need) {
			return invalid(fmt.Sprintf("publication requires verified local %s", need))
		}
	}
	if !hasEvidence(w, "delta") {
		return invalid("publication requires an approved independent current delta review")
	}
	return nil
}

package instructor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// PolicyRebindInput binds a replacement to the displayed work and effective
// project policy. Apply requires the target version returned by the preview.
type PolicyRebindInput struct {
	SnapshotID          string `json:"snapshotId"`
	WorkID              string `json:"workId"`
	Reason              string `json:"reason"`
	TargetPolicyVersion string `json:"targetPolicyVersion,omitempty"`
	Approval            string `json:"approval,omitempty"`
}

// PolicyRebindPreview describes the exact replacement and invalidated authority.
type PolicyRebindPreview struct {
	WorkID                  string                  `json:"workId"`
	SnapshotID              string                  `json:"snapshotId"`
	WorkRevision            int64                   `json:"workRevision"`
	BeforePolicyVersion     string                  `json:"beforePolicyVersion"`
	TargetPolicyVersion     string                  `json:"targetPolicyVersion"`
	PolicySource            string                  `json:"policySource"`
	BeforePolicy            domain.InstructorPolicy `json:"beforePolicy"`
	EffectivePolicy         domain.InstructorPolicy `json:"effectivePolicy"`
	ProposalHash            string                  `json:"proposalHash"`
	ExactApproval           string                  `json:"exactApproval"`
	InvalidatedTransitions  []string                `json:"invalidatedTransitions"`
	SupersededExceptionIDs  []string                `json:"supersededExceptionIds"`
	HistoricalEvidenceCount int                     `json:"historicalEvidenceCount"`
}

func policyRebindEligible(w domain.InstructorWork) error {
	if w.Cancellation != nil || w.State == "done" || w.State == "cancelled" {
		return invalid("terminal or cancelling work cannot change policy")
	}
	if w.State != "ready" && w.State != "parked" && w.State != "waiting_human" && w.State != "blocked" {
		return invalid("pause the work before changing its policy")
	}
	// Even a revoked historical grant can describe an irreversible effect.
	if len(w.PublicationGrants) > 0 {
		return invalid("publication history freezes the work policy; create a follow-up work")
	}
	if len(w.UncertainDispatchIDs) > 0 {
		return fmt.Errorf("%w: reconcile uncertain dispatches before changing policy", ErrDeliveryUncertain)
	}
	for _, gate := range []*domain.InstructorGate{w.Gate, w.SuspendedGate} {
		if gate != nil && (gate.Kind == "delivery_uncertain" || gate.Kind == "publication_violation" || gate.Kind == "cancellation_pending") {
			return invalid("resolve the current safety gate before changing policy: " + gate.Kind)
		}
	}
	for _, action := range w.PhaseActions {
		if action.State == "running" || action.State == "uncertain" {
			return fmt.Errorf("%w: mechanical action must settle before changing policy", ErrDeliveryUncertain)
		}
	}
	return nil
}

func (s *Service) policyRebindProposal(ctx context.Context, in PolicyRebindInput) (domain.InstructorWork, PolicyRebindPreview, error) {
	w, err := s.get(ctx, in.WorkID)
	if err != nil {
		return w, PolicyRebindPreview{}, err
	}
	if err := policyRebindEligible(w); err != nil {
		return w, PolicyRebindPreview{}, err
	}
	if strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2048 {
		return w, PolicyRebindPreview{}, invalid("policy change needs a reason of at most 2048 bytes")
	}
	snapshot, found, err := s.deps.Store.GetInstructorSnapshot(ctx, in.SnapshotID)
	if err != nil {
		return w, PolicyRebindPreview{}, err
	}
	if !found {
		return w, PolicyRebindPreview{}, ErrNotFound
	}
	matched := false
	for _, captured := range snapshot.Works {
		if captured.ID == w.ID {
			if captured.Revision != w.Revision || captured.PolicyVersion != w.PolicyVersion {
				return w, PolicyRebindPreview{}, ErrConflict
			}
			matched = true
		}
	}
	if !matched {
		return w, PolicyRebindPreview{}, invalid("work is outside captured snapshot")
	}
	policy, source, err := s.ResolveProjectPolicy(ctx, w.ProjectID)
	if err != nil {
		return w, PolicyRebindPreview{}, err
	}
	if policy.Version != PolicyVersion(policy) {
		return w, PolicyRebindPreview{}, invalid("resolved target policy hash mismatch")
	}
	if in.TargetPolicyVersion != "" && in.TargetPolicyVersion != policy.Version {
		return w, PolicyRebindPreview{}, fmt.Errorf("%w: effective project policy changed; preview the replacement again", ErrConflict)
	}
	if policy.Version == w.PolicyVersion {
		return w, PolicyRebindPreview{}, invalid("work already uses the effective project policy")
	}
	phaseDeclared := policy.InitialPhase == w.Phase
	for _, transition := range policy.Transitions {
		phaseDeclared = phaseDeclared || transition.From == w.Phase || transition.To == w.Phase
	}
	if !phaseDeclared {
		return w, PolicyRebindPreview{}, invalid("target policy does not declare the current work phase")
	}
	for _, gate := range []*domain.InstructorGate{w.Gate, w.SuspendedGate} {
		if gate == nil || gate.TransitionTo == "" {
			continue
		}
		compatible := false
		for _, transition := range policy.Transitions {
			if transition.From == w.Phase && transition.To == gate.TransitionTo && transition.HumanGate {
				compatible = true
			}
		}
		if !compatible {
			return w, PolicyRebindPreview{}, invalid("target policy does not preserve the pending human transition " + w.Phase + "->" + gate.TransitionTo)
		}
	}
	if policy.MaxFixRounds < w.FixRounds {
		return w, PolicyRebindPreview{}, invalid("target policy fix limit is below already consumed rounds")
	}
	preview := PolicyRebindPreview{WorkID: w.ID, SnapshotID: in.SnapshotID, WorkRevision: w.Revision,
		BeforePolicyVersion: w.PolicyVersion, TargetPolicyVersion: policy.Version, PolicySource: source,
		BeforePolicy: w.Policy, EffectivePolicy: policy, InvalidatedTransitions: append([]string{}, w.ApprovedTransitions...),
		SupersededExceptionIDs: []string{}, HistoricalEvidenceCount: len(w.Evidence)}
	for _, exception := range w.Exceptions {
		if !exception.Superseded {
			preview.SupersededExceptionIDs = append(preview.SupersededExceptionIDs, exception.ID)
		}
	}
	encoded, err := json.Marshal(struct {
		Preview PolicyRebindPreview
		Reason  string
	}{preview, strings.TrimSpace(in.Reason)})
	if err != nil {
		return w, PolicyRebindPreview{}, err
	}
	digest := sha256.Sum256(encoded)
	preview.ProposalHash = hex.EncodeToString(digest[:])
	preview.ExactApproval = "REBIND-GO " + w.ID + " " + preview.ProposalHash
	return w, preview, nil
}

func (s *Service) policyRebindQuiescence(ctx context.Context, w domain.InstructorWork) (domain.InstructorCancellationProof, error) {
	var proof domain.InstructorCancellationProof
	rows, err := s.deps.Store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil {
		return proof, err
	}
	for _, dispatch := range rows {
		if dispatch.State == "pending" || dispatch.State == "leased" || dispatch.State == "queued" || dispatch.State == "uncertain" {
			return proof, fmt.Errorf("%w: dispatch %s must settle before policy changes", ErrDeliveryUncertain, dispatch.ID)
		}
	}
	observer, ok := s.deps.Delivery.(CancellationObserver)
	if !ok {
		if len(w.SessionIDs) == 0 && len(w.Launches) == 0 {
			return proof, nil // The atomic writer also checks hidden reservations and members.
		}
		return proof, invalid("native quiescence observer unavailable; policy change remains blocked")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	proof, quiet, err := observer.ObserveCancellation(probeCtx, w)
	if err != nil {
		return proof, err
	}
	if !quiet {
		return proof, fmt.Errorf("%w: native producers must be positively quiet before changing policy", ErrDeliveryUncertain)
	}
	return proof, nil
}

// PreviewPolicyRebind performs no mutation and never consumes a human approval.
func (s *Service) PreviewPolicyRebind(ctx context.Context, in PolicyRebindInput) (PolicyRebindPreview, error) {
	w, preview, err := s.policyRebindProposal(ctx, in)
	if err != nil {
		return preview, err
	}
	_, err = s.policyRebindQuiescence(ctx, w)
	return preview, err
}

// ApplyPolicyRebind replaces only the pinned contract, under an exact human
// grant and native-effect fence. It never resumes the work or replenishes usage.
func (s *Service) ApplyPolicyRebind(ctx context.Context, in PolicyRebindInput) (domain.InstructorWork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(ctx, in.WorkID)
	if err != nil {
		return current, err
	}
	if in.TargetPolicyVersion == "" {
		return current, invalid("policy change requires the targetPolicyVersion from its preview")
	}
	// A persisted receipt survives restart and an expired snapshot. Returning the
	// current work never restores its former state after later stop/cancel actions.
	for _, receipt := range current.PolicyRebindings {
		if receipt.Approval == in.Approval && receipt.SnapshotID == in.SnapshotID && receipt.Reason == strings.TrimSpace(in.Reason) && receipt.TargetPolicyVersion == in.TargetPolicyVersion {
			return current, nil
		}
	}
	w, preview, err := s.policyRebindProposal(ctx, in)
	if err != nil {
		return w, err
	}
	if in.Approval != preview.ExactApproval {
		return w, invalid("exact human policy approval required: " + preview.ExactApproval)
	}
	proof, err := s.policyRebindQuiescence(ctx, w)
	if err != nil {
		return w, err
	}
	// Resolve again after native probes, which may block while settings change.
	target, source, err := s.ResolveProjectPolicy(ctx, w.ProjectID)
	if err != nil {
		return w, err
	}
	if target.Version != preview.TargetPolicyVersion || source != preview.PolicySource {
		return w, fmt.Errorf("%w: effective project policy changed during confirmation", ErrConflict)
	}
	receipt := domain.InstructorPolicyRebind{ProposalHash: preview.ProposalHash, SnapshotID: in.SnapshotID,
		WorkRevision: w.Revision, BeforePolicy: w.Policy, TargetPolicyVersion: preview.TargetPolicyVersion,
		PolicySource: source, Reason: strings.TrimSpace(in.Reason), Approval: in.Approval, PreviousState: w.State,
		PreviousGate: w.Gate, PreviousSuspendedGate: w.SuspendedGate, PreviousApprovalDispatchID: w.ApprovalDispatchID,
		InvalidatedTransitions: preview.InvalidatedTransitions, SupersededExceptionIDs: preview.SupersededExceptionIDs, AppliedAt: s.now()}
	w.PolicyRebindings = append(w.PolicyRebindings, receipt)
	w.Policy, w.PolicyVersion = preview.EffectivePolicy, preview.TargetPolicyVersion
	w.ApprovedTransitions, w.ApprovalDispatchID = nil, ""
	for i := range w.Exceptions {
		w.Exceptions[i].Superseded = true
	}
	// Configuration approval cannot satisfy an unrelated business decision.
	// Keep its semantics, but give it a fresh identity under the new policy.
	refreshGate := func(gate *domain.InstructorGate, suffix string) *domain.InstructorGate {
		if gate == nil {
			return nil
		}
		fresh := *gate
		fresh.ID, fresh.OpenedAt = "policy-rebound-"+preview.ProposalHash+suffix, s.now()
		if fresh.Kind == "policy_rebound" {
			fresh.ExactApproval = "RESUME-GO " + w.ID + " " + w.PolicyVersion
		}
		return &fresh
	}
	w.Gate, w.SuspendedGate = refreshGate(w.Gate, "-gate"), refreshGate(w.SuspendedGate, "-suspended")
	if w.Gate == nil && w.SuspendedGate != nil {
		w.Gate, w.SuspendedGate = w.SuspendedGate, nil
	}
	if w.Gate == nil {
		w.Gate = &domain.InstructorGate{ID: "policy-rebound-" + preview.ProposalHash, Kind: "policy_rebound",
			Title: "Einstellungen übernommen; Ausführung erneut freigeben", OpenedAt: s.now(),
			ExactApproval: "RESUME-GO " + w.ID + " " + w.PolicyVersion}
	}
	if w.State != "parked" {
		w.State = "waiting_human"
	}
	w.WaitCondition, w.Summary = "Neue Policy ist gebunden; bisherige Ausführungsfreigaben gelten nicht weiter", "Einstellungen übernommen; Arbeit bleibt angehalten"
	w.WaitUntil, w.NudgedAt = nil, nil
	expected := w.Revision
	w.Revision++
	w.UpdatedAt, w.Actions = s.now(), nil
	store, ok := s.deps.Store.(interface {
		SaveInstructorPolicyRebind(context.Context, domain.InstructorWork, int64, domain.InstructorEvent, domain.InstructorCancellationProof) (bool, error)
	})
	if !ok {
		return current, invalid("atomic policy replacement storage unavailable")
	}
	saved, err := store.SaveInstructorPolicyRebind(ctx, w, expected, s.event(w, "policy_rebound", "proposal="+preview.ProposalHash+" before="+preview.BeforePolicyVersion+" after="+preview.TargetPolicyVersion+" source="+source+" reason="+receipt.Reason), proof)
	if errors.Is(err, domain.ErrInstructorProducerChanged) {
		return current, fmt.Errorf("%w: reconcile native state before changing policy: %w", ErrDeliveryUncertain, err)
	}
	if err != nil {
		return current, err
	}
	if !saved {
		return current, ErrConflict
	}
	return w, nil
}

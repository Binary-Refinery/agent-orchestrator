package instructor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ExceptionInput proposes one expiring, snapshot-bound role or fix-budget exception.
type ExceptionInput struct {
	SnapshotID   string                           `json:"snapshotId"`
	WorkID       string                           `json:"workId"`
	Role         string                           `json:"role,omitempty"`
	Selection    *domain.InstructorAgentSelection `json:"selection,omitempty"`
	MaxFixRounds *int                             `json:"maxFixRounds,omitempty"`
	Reason       string                           `json:"reason"`
	ExpiresAt    time.Time                        `json:"expiresAt"`
	Approval     string                           `json:"approval,omitempty"`
}

// ExceptionPreview freezes the proposed policy and exact human approval phrase.
type ExceptionPreview struct {
	WorkID          string                  `json:"workId"`
	PolicyVersion   string                  `json:"policyVersion"`
	ProposalHash    string                  `json:"proposalHash"`
	ExactApproval   string                  `json:"exactApproval"`
	EffectivePolicy domain.InstructorPolicy `json:"effectivePolicy"`
}

func validateWorkExceptions(w domain.InstructorWork, now time.Time) error {
	if w.Policy.ExceptionVersion != "" && len(w.Exceptions) == 0 {
		return invalid("work exception has no persisted human grant")
	}
	for _, grant := range w.Exceptions {
		if !grant.Superseded && !now.Before(grant.ExpiresAt) {
			return invalid("work exception expired; a new scoped operator approval is required")
		}
	}
	return nil
}

func (s *Service) exceptionProposal(ctx context.Context, in ExceptionInput) (domain.InstructorWork, ExceptionPreview, error) {
	w, err := s.get(ctx, in.WorkID)
	if err != nil {
		return w, ExceptionPreview{}, err
	}
	if w.State == "done" || w.State == "cancelled" {
		return w, ExceptionPreview{}, invalid("terminal work cannot receive an exception")
	}
	if hasPublicationHistory(w) {
		return w, ExceptionPreview{}, invalid("a publication grant freezes this work policy; create a follow-up work for a different contract")
	}
	snapshot, found, err := s.deps.Store.GetInstructorSnapshot(ctx, in.SnapshotID)
	if err != nil {
		return w, ExceptionPreview{}, err
	}
	if !found {
		return w, ExceptionPreview{}, ErrNotFound
	}
	matched := false
	for _, captured := range snapshot.Works {
		if captured.ID == w.ID {
			if captured.Revision != w.Revision {
				return w, ExceptionPreview{}, ErrConflict
			}
			matched = true
		}
	}
	if !matched {
		return w, ExceptionPreview{}, invalid("work is outside captured snapshot")
	}
	if strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2048 || !in.ExpiresAt.After(s.now()) || in.ExpiresAt.After(s.now().Add(24*time.Hour)) {
		return w, ExceptionPreview{}, invalid("exception needs a reason and expiry within 24 hours")
	}
	if in.Selection == nil && in.MaxFixRounds == nil {
		return w, ExceptionPreview{}, invalid("exception must change a role selection or scoped fix budget")
	}
	// Deep copy all policy maps: previews never mutate stored work facts.
	var policy domain.InstructorPolicy
	body, _ := json.Marshal(w.Policy)
	if err := json.Unmarshal(body, &policy); err != nil {
		return w, ExceptionPreview{}, err
	}
	if in.Selection != nil {
		if in.Role != "worker" && in.Role != "reviewer" {
			return w, ExceptionPreview{}, invalid("role exception is limited to worker or reviewer")
		}
		role, exists := policy.Roles[in.Role]
		if !exists || in.Selection.Harness == "" || in.Selection.Model == "" || in.Selection.Effort == "" {
			return w, ExceptionPreview{}, invalid("exception requires a declared role and concrete native selection")
		}
		role.Harness, role.Model, role.Effort = in.Selection.Harness, in.Selection.Model, "maximum"
		role.EffortByModel = map[string]string{in.Selection.Model: in.Selection.Effort}
		if in.Role == "reviewer" {
			role.Independent = true
		}
		if s.deps.RoleValidator == nil {
			return w, ExceptionPreview{}, invalid("native model capability validator unavailable")
		}
		if err := s.deps.RoleValidator.ValidateInstructorAgentConfig(ctx, w.ProjectID, role, in.Selection.Harness, in.Selection.Model, in.Selection.Effort); err != nil {
			return w, ExceptionPreview{}, err
		}
		policy.Roles[in.Role] = role
	} else if in.Role != "" {
		return w, ExceptionPreview{}, invalid("role name needs an explicit native selection")
	}
	if in.MaxFixRounds != nil {
		if *in.MaxFixRounds < w.Policy.MaxFixRounds || *in.MaxFixRounds <= w.FixRounds || *in.MaxFixRounds > 10 {
			return w, ExceptionPreview{}, invalid("exception fix budget must extend available rounds and remain at most 10")
		}
		policy.MaxFixRounds = *in.MaxFixRounds
	}
	if err := ValidatePolicy(policy); err != nil {
		return w, ExceptionPreview{}, err
	}
	proposal := struct {
		SnapshotID, WorkID, CurrentPolicy, SHA string
		Role                                   string
		Selection                              *domain.InstructorAgentSelection
		MaxFixRounds                           *int
		Reason                                 string
		ExpiresAt                              time.Time
	}{in.SnapshotID, w.ID, w.PolicyVersion, w.SHA, in.Role, in.Selection, in.MaxFixRounds, strings.TrimSpace(in.Reason), in.ExpiresAt.UTC()}
	encoded, _ := json.Marshal(proposal)
	hash := sha256.Sum256(encoded)
	digest := hex.EncodeToString(hash[:])
	policy.ExceptionVersion = digest
	policy.Version = PolicyVersion(policy)
	return w, ExceptionPreview{WorkID: w.ID, PolicyVersion: w.PolicyVersion, ProposalHash: digest, ExactApproval: "EXCEPTION-GO " + w.ID + " " + digest, EffectivePolicy: policy}, nil
}

// PreviewException validates a scoped proposal without changing work or policy facts.
func (s *Service) PreviewException(ctx context.Context, in ExceptionInput) (ExceptionPreview, error) {
	_, preview, err := s.exceptionProposal(ctx, in)
	return preview, err
}

// ApplyException records an exact human grant and pins its new work-local policy.
func (s *Service) ApplyException(ctx context.Context, in ExceptionInput) (domain.InstructorWork, error) {
	w, preview, err := s.exceptionProposal(ctx, in)
	if err != nil {
		return w, err
	}
	if in.Approval != preview.ExactApproval {
		return w, invalid("exact human exception approval required: " + preview.ExactApproval)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(ctx, w.ID)
	if err != nil {
		return current, err
	}
	if current.Revision != w.Revision {
		return current, ErrConflict
	}
	before := w.PolicyVersion
	newGrant := func(kind, role string, selection *domain.InstructorAgentSelection, budget int) {
		for i := range w.Exceptions {
			if w.Exceptions[i].Kind == kind && w.Exceptions[i].Role == role {
				w.Exceptions[i].Superseded = true
			}
		}
		w.Exceptions = append(w.Exceptions, domain.InstructorException{ID: s.deps.NewID(), Kind: kind, Role: role, Selection: selection, MaxFixRounds: budget, ProposalHash: preview.ProposalHash, SnapshotID: in.SnapshotID, SHA: w.SHA, Approval: in.Approval, Reason: strings.TrimSpace(in.Reason), BeforePolicyVersion: before, AfterPolicyVersion: preview.EffectivePolicy.Version, GrantedAt: s.now(), ExpiresAt: in.ExpiresAt.UTC()})
	}
	if in.Selection != nil {
		cloned := *in.Selection
		newGrant("role", in.Role, &cloned, 0)
	}
	if in.MaxFixRounds != nil {
		newGrant("budget", "", nil, *in.MaxFixRounds)
	}
	w.Policy, w.PolicyVersion = preview.EffectivePolicy, preview.EffectivePolicy.Version
	w.ApprovedTransitions = nil
	// Work identity/history remain; all prior-policy proof loses acceptance by
	// its immutable policy binding. Publication/transition gates remain open.
	if w.State == "blocked" && w.Gate == nil {
		w.State = "ready"
		w.WaitCondition = ""
	}
	w.Summary = "Explizite befristete Ausnahme für diese Arbeit freigegeben"
	return s.save(ctx, w, w.Revision, "exception_approved", fmt.Sprintf("proposal=%s before=%s after=%s expires=%s reason=%s", preview.ProposalHash, before, w.PolicyVersion, in.ExpiresAt.UTC().Format(time.RFC3339), strings.TrimSpace(in.Reason)))
}

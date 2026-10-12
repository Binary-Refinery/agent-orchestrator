package instructor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrPublicationUnavailable is fail-closed: a human phrase cannot substitute
// for isolated native workers, an operator channel and publisher credentials.
var ErrPublicationUnavailable = errors.New("protected publication is unavailable")

// ErrPublicationConsumed fences replays and interrupted external mutations.
var ErrPublicationConsumed = errors.New("publication grant has already been consumed")

// PublicationBoundary is installed by the daemon's actual isolation adapter.
// Capabilities must describe enforced OS/runtime facts, never client flags.
// Isolation covers all work-controlled programs, including check commands and
// native reviewers; containing only the model process does not suffice.
// Authorize must independently authenticate an operator against the full
// decision. Execute must use credentials inaccessible to native workers and
// must pin the remote mutation to the exact supplied commit and destination.
type PublicationBoundary interface {
	Capabilities(context.Context, domain.InstructorWork) (domain.InstructorPublicationCapabilities, error)
	Authorize(context.Context, PublicationDecision) (PublicationAuthorization, error)
	Execute(context.Context, PublicationRequest) (PublicationReceipt, error)
}

// PublicationDecision is the immutable scope presented to a protected operator.
type PublicationDecision struct {
	WorkID, GateID, SnapshotID, PolicyVersion, SHA, Branch, Approval, Operation string
	WorkRevision                                                                int64
}

// PublicationAuthorization is returned by the trusted operator adapter; it is
// deliberately absent from public request DTOs and agent reports.
type PublicationAuthorization struct {
	OperatorID, AuthorizationID string
}

// PublicationRequest identifies the single claimed external effect.
type PublicationRequest struct {
	ExecutionID string
	Grant       domain.InstructorPublicationGrant
	Work        domain.InstructorWork
}

// PublicationReceipt is an actual publisher observation, not an agent claim.
type PublicationReceipt struct {
	Ref string
}

// PublicationInput pins executor admission to the current operator view.
type PublicationInput struct {
	WorkID       string `json:"workId"`
	GrantID      string `json:"grantId"`
	SnapshotID   string `json:"snapshotId"`
	WorkRevision int64  `json:"workRevision"`
	SHA          string `json:"sha"`
	Branch       string `json:"branch"`
	Operation    string `json:"operation"`
}

func publicationOperation(operation string) string {
	if operation == "" {
		return "publish"
	}
	return operation
}

func (s *Service) publicationCapabilities(ctx context.Context, w domain.InstructorWork) (domain.InstructorPublicationCapabilities, error) {
	if s.deps.Publication == nil {
		return domain.InstructorPublicationCapabilities{Reason: "No protected publication boundary is installed; native workers share the host's user and credentials."}, nil
	}
	c, err := s.deps.Publication.Capabilities(ctx, w)
	if err != nil {
		return c, err
	}
	c.Available = c.Available && c.WorkerIsolation && c.ProtectedOperator && c.IsolatedCredentials
	if !c.Available && c.Reason == "" {
		c.Reason = "Worker isolation, protected operator authorization and isolated publisher credentials are all required."
	}
	return c, nil
}

// PublicationCapabilities exposes why a publication button is disabled.
func (s *Service) PublicationCapabilities(ctx context.Context) domain.InstructorPublicationCapabilities {
	c, err := s.publicationCapabilities(ctx, domain.InstructorWork{})
	if err != nil {
		return domain.InstructorPublicationCapabilities{Reason: "Publication boundary inspection failed."}
	}
	return c
}

func (s *Service) requirePublicationBoundary(ctx context.Context, w domain.InstructorWork) error {
	c, err := s.publicationCapabilities(ctx, w)
	if err != nil {
		return fmt.Errorf("%w: boundary inspection failed", ErrPublicationUnavailable)
	}
	if !c.Available {
		return fmt.Errorf("%w: %s", ErrPublicationUnavailable, c.Reason)
	}
	return nil
}

func (s *Service) authorizePublication(ctx context.Context, w domain.InstructorWork, snapshotID string, in CommandInput) (PublicationAuthorization, error) {
	if in.RequesterSessionID != "" {
		return PublicationAuthorization{}, invalid("native sessions cannot authorize publication")
	}
	for _, g := range w.PublicationGrants {
		if g.Execution != nil && (g.Execution.State == "claimed" || g.Execution.State == "uncertain") {
			return PublicationAuthorization{}, fmt.Errorf("%w: an earlier attempt needs independent remote reconciliation", ErrPublicationConsumed)
		}
	}
	if err := s.requirePublicationBoundary(ctx, w); err != nil {
		return PublicationAuthorization{}, err
	}
	operation := publicationOperation(in.PublicationOperation)
	if operation != "publish" && operation != "merge" {
		return PublicationAuthorization{}, invalid("unsupported publication operation")
	}
	a, err := s.deps.Publication.Authorize(ctx, PublicationDecision{
		WorkID: w.ID, GateID: w.Gate.ID, SnapshotID: snapshotID, PolicyVersion: w.PolicyVersion,
		SHA: w.SHA, Branch: w.Branch, Approval: in.Approval, Operation: operation, WorkRevision: w.Revision,
	})
	if err != nil {
		return PublicationAuthorization{}, fmt.Errorf("%w: operator authorization was not established", ErrPublicationUnavailable)
	}
	if strings.TrimSpace(a.OperatorID) == "" || strings.TrimSpace(a.AuthorizationID) == "" {
		return PublicationAuthorization{}, fmt.Errorf("%w: protected operator identity and decision proof are required", ErrPublicationUnavailable)
	}
	return a, nil
}

// ExecutePublication claims the exact grant in SQLite before calling the
// protected publisher. Claiming is a revision CAS, so parallel requests or a
// daemon restart cannot replay the effect. Failure/interrupt remains consumed;
// reconcile actual remote facts before any fresh operator decision.
func (s *Service) ExecutePublication(ctx context.Context, in PublicationInput) (domain.InstructorWork, error) {
	s.mu.Lock()
	w, err := s.get(ctx, in.WorkID)
	if err != nil {
		s.mu.Unlock()
		return w, err
	}
	if err = s.requirePublicationBoundary(ctx, w); err != nil {
		s.mu.Unlock()
		return w, err
	}
	index := -1
	for i, g := range w.PublicationGrants {
		if g.ID == in.GrantID && in.GrantID != "" {
			index = i
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return w, invalid("an exact protected publication grant is required")
	}
	g := w.PublicationGrants[index]
	if g.Operation != "publish" && g.Operation != "merge" {
		s.mu.Unlock()
		return w, invalid("unsupported protected publication operation")
	}
	if g.Execution != nil {
		s.mu.Unlock()
		return w, ErrPublicationConsumed
	}
	if in.WorkRevision != w.Revision || in.SHA != w.SHA || in.Branch != w.Branch || in.SnapshotID != g.SnapshotID || in.Operation != g.Operation {
		s.mu.Unlock()
		return w, ErrConflict
	}
	if !validPublicationGrant(g, w.ID) || g.PolicyVersion != w.PolicyVersion || g.SHA != w.SHA || g.Branch != w.Branch || g.OperatorID == "" || g.AuthorizationID == "" {
		s.mu.Unlock()
		return w, invalid("grant does not authorize the current protected publication")
	}
	if w.Gate != nil || w.State == "parked" || w.State == "blocked" || w.State == "done" || w.State == "cancelled" {
		s.mu.Unlock()
		return w, invalid("work is not available for publication")
	}
	if err = validateWorkExceptions(w, s.now()); err != nil {
		s.mu.Unlock()
		return w, err
	}
	if w.PolicyVersion != PolicyVersion(w.Policy) {
		s.mu.Unlock()
		return w, invalid("publication pinned policy hash mismatch")
	}
	if g.DispatchID != "" {
		ready, admissionErr := s.approvalCohortReady(ctx, g.DispatchID)
		if admissionErr != nil || !ready {
			s.mu.Unlock()
			if admissionErr != nil {
				return w, admissionErr
			}
			return w, invalid("ordered approval cohort awaits native admission")
		}
	}
	if err = publicationReadiness(w); err == nil {
		err = s.CurrentEvidence(ctx, w)
	}
	if err == nil && g.Operation == "merge" {
		err = closeoutEvidence(w)
	}
	if err != nil {
		s.mu.Unlock()
		return w, err
	}
	execution := domain.InstructorPublicationExecution{ID: "publish-attempt-" + s.deps.NewID(), State: "claimed", ClaimedAt: s.now()}
	w.PublicationGrants[index].Execution = &execution
	w, err = s.save(ctx, w, w.Revision, "publication_claimed", execution.ID+" grant="+g.ID)
	s.mu.Unlock()
	if err != nil {
		return w, err
	}
	g = w.PublicationGrants[index]
	// An exception can expire while the claim is being persisted. Repeat its
	// clock check at the last admission boundary before the external call.
	receipt := PublicationReceipt{}
	publishErr := validateWorkExceptions(w, s.now())
	deniedBeforeEffect := publishErr != nil
	if publishErr == nil {
		receipt, publishErr = s.deps.Publication.Execute(ctx, PublicationRequest{ExecutionID: execution.ID, Grant: g, Work: w})
	}
	// Completion uses a fresh revision so unrelated heartbeats/progress cannot
	// erase the consumed grant. A persistence failure leaves the durable claim.
	s.mu.Lock()
	defer s.mu.Unlock()
	completionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	current, readErr := s.get(completionCtx, w.ID)
	if readErr != nil {
		return w, fmt.Errorf("%w: publisher outcome could not be recorded", ErrPublicationConsumed)
	}
	index = -1
	for i, grant := range current.PublicationGrants {
		if grant.ID == g.ID && grant.Execution != nil && grant.Execution.ID == execution.ID {
			index = i
			break
		}
	}
	if index < 0 {
		return current, fmt.Errorf("%w: claimed publication record changed", ErrPublicationConsumed)
	}
	completed := s.now()
	execution.CompletedAt, execution.Ref, execution.State = &completed, receipt.Ref, "completed"
	if publishErr != nil {
		execution.State, execution.Error = "uncertain", "Protected publisher failed; remote effects must be reconciled before another authorization."
		if deniedBeforeEffect {
			execution.State, execution.Error = "cancelled", "Work exception expired before publisher admission; no external publication was attempted."
		}
	}
	current.PublicationGrants[index].Execution = &execution
	current, err = s.save(completionCtx, current, current.Revision, "publication_"+execution.State, execution.ID)
	if err != nil {
		return current, fmt.Errorf("%w: publisher outcome could not be recorded", ErrPublicationConsumed)
	}
	if publishErr != nil {
		return current, fmt.Errorf("%w: %s", ErrPublicationConsumed, execution.Error)
	}
	return current, nil
}

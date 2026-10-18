package instructor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// RoleValidator verifies a concrete selection against provider-native capabilities.
type RoleValidator interface {
	ValidateInstructorAgentConfig(context.Context, domain.ProjectID, domain.InstructorRole, string, string, string) error
}

// SpawnInput describes a fully resolved launch before any native process starts.
type SpawnInput struct {
	ProjectID       domain.ProjectID
	ProjectPath     string
	GovernanceFile  string
	WorkID          string
	ParentSessionID domain.SessionID
	IssueID         domain.IssueID
	Source          string
	SourceRef       string
	Name            string
	Goal            string
	Role            string
	Harness         string
	Model           string
	Effort          string
}

// PrepareSpawn registers work identity and enforces the pinned launch role and effort.
func (s *Service) PrepareSpawn(ctx context.Context, in SpawnInput) (string, error) {
	if in.Role == "" {
		in.Role = "worker"
	}
	workID, err := s.ResolveSpawnWork(ctx, in.ParentSessionID, in.WorkID, in.Role)
	if err != nil {
		return workID, err
	}
	in.WorkID = workID
	var w domain.InstructorWork
	if in.WorkID != "" {
		var e error
		w, e = s.get(ctx, in.WorkID)
		if e != nil {
			return "", e
		}
		if w.ProjectID != in.ProjectID {
			return "", invalid("work belongs to another project")
		}
	} else {
		p, e := s.GetPolicy(ctx)
		if e != nil {
			return "", e
		}
		relative := in.GovernanceFile
		if relative == "" {
			relative = ".ao/governance.json"
		}
		if in.ProjectPath != "" {
			repoPolicy, e := LoadPolicy(in.ProjectPath, relative)
			if e == nil {
				p = repoPolicy
			} else if !os.IsNotExist(e) || in.GovernanceFile != "" {
				return "", e
			}
		}
		if in.Name == "" || in.Source == "automation" {
			if in.SourceRef == "" {
				in.SourceRef = s.deps.NewID()
			}
			digest := sha256.Sum256([]byte(string(in.ProjectID) + "\x00" + in.Source + "\x00" + in.SourceRef))
			suffix := " " + hex.EncodeToString(digest[:])[:10]
			prefix := strings.TrimSpace(in.Name)
			if prefix == "" {
				prefix = "Auftrag"
			}
			// A definition's display name repeats for every scheduled run.
			// Keep readable work names distinct by durable occurrence origin,
			// within the registration byte limit and on a Unicode boundary.
			for len(prefix)+len(suffix) > 120 {
				_, size := utf8.DecodeLastRuneInString(prefix)
				prefix = prefix[:len(prefix)-size]
			}
			in.Name = strings.TrimSpace(prefix) + suffix
		}
		if in.Goal == "" {
			in.Goal = "Manuell gestartete Arbeit"
		}
		w, e = s.Register(ctx, RegisterInput{ProjectID: in.ProjectID, Name: in.Name, Goal: in.Goal, Source: in.Source, SourceRef: in.SourceRef, IssueID: in.IssueID, Policy: p})
		if e != nil {
			return "", e
		}
	}
	if w.Cancellation != nil || w.State == "done" || w.State == "cancelled" || w.State == "parked" || w.State == "blocked" || w.State == "waiting_human" {
		return w.ID, invalid("cannot launch work in current state")
	}
	if err := ValidatePolicy(w.Policy); err != nil {
		return w.ID, err
	}
	if w.PolicyVersion != PolicyVersion(w.Policy) {
		return w.ID, invalid("pinned work policy hash mismatch")
	}
	if err := validateWorkExceptions(w, s.now()); err != nil {
		return w.ID, err
	}
	if w.ApprovalDispatchID != "" {
		ready, err := s.approvalCohortReady(ctx, w.ApprovalDispatchID)
		if err != nil {
			return w.ID, err
		}
		if !ready {
			return w.ID, invalid("ordered approval cohort awaits native admission")
		}
	}
	if in.Role == "worker" {
		if err := correctiveWorkerAdmission(w); err != nil {
			return w.ID, err
		}
	}
	return w.ID, s.validateRoleSelection(ctx, w, in.Role, in.Harness, in.Model, in.Effort)
}

// ResolveSpawnWork confines managed non-control delegation to the parent's work.
// The daemon calls it before its unbound project-orchestrator launch shortcut.
func (s *Service) ResolveSpawnWork(ctx context.Context, parentID domain.SessionID, requestedWorkID, childRole string) (string, error) {
	if parentID == "" {
		return requestedWorkID, nil
	}
	parent, managed, err := s.WorkForSession(ctx, parentID)
	if err != nil || !managed {
		return requestedWorkID, err
	}
	role, _, err := s.sessionRole(ctx, parent, parentID)
	if err != nil {
		return parent.ID, err
	}
	if role == "instructor" {
		if childRole != "orchestrator" {
			return parent.ID, invalid("instructor must delegate worker tasks through a project orchestrator")
		}
		return requestedWorkID, nil
	}
	if role == "orchestrator" {
		return requestedWorkID, nil
	}
	if requestedWorkID != "" && requestedWorkID != parent.ID {
		return parent.ID, invalid("managed non-control parent cannot delegate into another work")
	}
	if childRole == "orchestrator" || childRole == "instructor" {
		return parent.ID, invalid("managed non-control parent cannot delegate a control role")
	}
	if err := s.validateManagedResume(ctx, parent, parentID); err != nil {
		return parent.ID, err
	}
	return parent.ID, nil
}

func (s *Service) validateRoleSelection(ctx context.Context, w domain.InstructorWork, name, harness, model, effort string) error {
	role, ok := w.Policy.Roles[name]
	if !ok {
		return invalid(fmt.Sprintf("role %s is not declared in pinned policy", name))
	}
	if role.Harness != "" && role.Harness != harness {
		return invalid(fmt.Sprintf("role %s requires harness %s", name, role.Harness))
	}
	if role.Model != "" && role.Model != model {
		return invalid("launch model does not match pinned role")
	}
	if model == "" || effort == "" {
		return invalid("managed role needs explicit model and native effort")
	}
	if role.Effort == "maximum" {
		expected := role.EffortByModel[model]
		if expected == "" {
			return invalid("maximum requires explicit approved native effort mapping for this model")
		}
		if expected != effort {
			return invalid("launch effort does not match approved maximum mapping")
		}
	} else if role.Effort != "configured" && role.Effort != effort {
		return invalid("launch effort does not match pinned role")
	}
	if s.deps.RoleValidator == nil {
		return invalid("native model capability validator unavailable")
	}
	return s.deps.RoleValidator.ValidateInstructorAgentConfig(ctx, w.ProjectID, role, harness, model, effort)
}

// ValidateResume enforces managed work authority while preserving unassigned legacy sessions.
func (s *Service) ValidateResume(ctx context.Context, id domain.SessionID) error {
	w, ok, e := s.WorkForSession(ctx, id)
	if e != nil {
		return e
	}
	if !ok {
		return nil
	}
	return s.validateManagedResume(ctx, w, id)
}

func (s *Service) validateManagedResume(ctx context.Context, w domain.InstructorWork, id domain.SessionID) error {
	role, session, err := s.validateManagedExecutionAuthority(ctx, w, id)
	if err != nil {
		return err
	}
	return s.validateManagedSessionSelection(ctx, w, id, role, session)
}

// ValidateAgentChange checks the current work authority and the proposed native
// choice. An obsolete selection may be replaced after a rebind; it is not first
// required to satisfy the new policy, and no old provider conversation is moved.
func (s *Service) ValidateAgentChange(ctx context.Context, id domain.SessionID, selection domain.InstructorAgentSelection) error {
	w, managed, err := s.WorkForSession(ctx, id)
	if err != nil || !managed {
		return err
	}
	role, _, err := s.validateManagedExecutionAuthority(ctx, w, id)
	if err != nil {
		return err
	}
	return s.validateRoleSelection(ctx, w, role, selection.Harness, selection.Model, selection.Effort)
}

// ValidateExecutionAuthority checks work gates without authorizing the owner's
// native selection. Reviewer-only callers separately validate their reviewer;
// the retained worker is an owner of evidence, not a resumed native producer.
func (s *Service) ValidateExecutionAuthority(ctx context.Context, id domain.SessionID) error {
	w, managed, err := s.WorkForSession(ctx, id)
	if err != nil || !managed {
		return err
	}
	_, _, err = s.validateManagedExecutionAuthority(ctx, w, id)
	return err
}

func (s *Service) validateManagedExecutionAuthority(ctx context.Context, w domain.InstructorWork, id domain.SessionID) (string, *domain.SessionRecord, error) {
	if err := validateWorkExceptions(w, s.now()); err != nil {
		return "", nil, err
	}
	if e := ValidatePolicy(w.Policy); e != nil {
		return "", nil, e
	}
	if w.PolicyVersion != PolicyVersion(w.Policy) {
		return "", nil, invalid("pinned work policy hash mismatch")
	}
	if w.Cancellation != nil || w.State == "done" || w.State == "cancelled" || w.State == "parked" || w.State == "blocked" || w.State == "waiting_human" {
		return "", nil, invalid("work may not resume in current state")
	}
	role, adopted, err := s.sessionRole(ctx, w, id)
	if err != nil {
		return "", nil, err
	}
	// The orchestrator must be able to receive its own ordered Go directive.
	// Worker/reviewer execution cannot precede that native admission receipt.
	if role != "orchestrator" && w.ApprovalDispatchID != "" {
		ready, err := s.approvalCohortReady(ctx, w.ApprovalDispatchID)
		if err != nil {
			return "", nil, err
		}
		if !ready {
			return "", nil, invalid("ordered approval cohort awaits native admission")
		}
	}
	if role == "worker" {
		if err := correctiveWorkerAdmission(w); err != nil {
			return "", nil, err
		}
	}
	return role, adopted, nil
}

func (s *Service) validateManagedSessionSelection(ctx context.Context, w domain.InstructorWork, id domain.SessionID, role string, session *domain.SessionRecord) error {
	if session == nil {
		sessions, ok := s.deps.Store.(interface {
			GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
		})
		if !ok {
			return invalid("managed native session identity reader unavailable")
		}
		record, found, err := sessions.GetSession(ctx, id)
		if err != nil {
			return err
		}
		if !found || record.ProjectID != w.ProjectID {
			return invalid("managed native session identity is missing or belongs to another project")
		}
		session = &record
	}
	// A historical requested/sent launch never authorizes today's persisted
	// selection under a replacement policy. The launch remains immutable audit.
	return s.validateRoleSelection(ctx, w, role, string(session.Harness), session.Metadata.Model, session.Metadata.Effort)
}

func (s *Service) sessionRole(ctx context.Context, w domain.InstructorWork, id domain.SessionID) (string, *domain.SessionRecord, error) {
	instance := ""
	if reader, ok := s.deps.Store.(sessionInstanceReader); ok {
		var found bool
		var err error
		instance, found, err = reader.InstructorSessionInstance(ctx, id)
		if err != nil {
			return "", nil, err
		}
		if !found {
			return "", nil, invalid("session instance missing")
		}
	}
	for i := len(w.Launches) - 1; i >= 0; i-- {
		if w.Launches[i].SessionID == id && (instance == "" || w.Launches[i].SessionInstanceID == instance) {
			role := w.Launches[i].Role
			if _, declared := w.Policy.Roles[role]; !declared {
				return "", nil, invalid("managed session launch role is not declared in pinned policy")
			}
			return role, nil, nil
		}
	}
	sessions, ok := s.deps.Store.(interface {
		GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	})
	if !ok {
		return "", nil, invalid("adopted session identity reader unavailable")
	}
	session, found, err := sessions.GetSession(ctx, id)
	if err != nil {
		return "", nil, err
	}
	if !found || session.ProjectID != w.ProjectID {
		return "", nil, invalid("adopted session identity is missing or belongs to another project")
	}
	role := string(session.Kind)
	if _, declared := w.Policy.Roles[role]; !declared || role == "" {
		return "", nil, invalid("adopted session kind is not declared in pinned policy")
	}
	return role, &session, nil
}

// A declared corrective human gate applies at admission, including the interval
// before the reducer has opened its visible gate. Control and review roles may
// still analyze triage while corrective workers await the exact current grant.
func correctiveWorkerAdmission(w domain.InstructorWork) error {
	for _, transition := range w.Policy.Transitions {
		if transition.From != w.Phase || transition.To != "fix" || !transition.HumanGate || transition.Role != "worker" {
			continue
		}
		if w.FixRounds >= w.Policy.MaxFixRounds {
			return invalid("corrective worker admission exceeds the pinned fix budget")
		}
		granted := false
		for _, grant := range w.ApprovedTransitions {
			if grant == transition.From+"->"+transition.To {
				granted = true
				break
			}
		}
		if granted {
			for _, evidence := range w.Evidence {
				if evidence.Verified && !evidence.Revoked && evidence.WorkID == w.ID && evidence.PolicyVersion == w.PolicyVersion && evidence.SHA == w.SHA && evidence.Kind == "triage" && evidence.Producer == "operator" && evidence.Role == "instructor" && strings.HasPrefix(evidence.Ref, "gate:") {
					return nil
				}
			}
		}
		return invalid("corrective worker admission requires the matching human grant for the current commit")
	}
	return nil
}

// SpawnFailed records a refused native launch as a visible blocked work.
func (s *Service) SpawnFailed(ctx context.Context, id string, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, e := s.get(ctx, id)
	if e != nil {
		return e
	}
	if w.Cancellation == nil && w.State != "done" && w.State != "cancelled" {
		w.State = "blocked"
		w.Summary = "Start abgelehnt"
		if cause != nil {
			w.WaitCondition = cause.Error()
		}
	}
	detail := w.WaitCondition
	if cause != nil {
		detail = cause.Error()
	}
	_, e = s.save(ctx, w, w.Revision, "launch_failed", detail)
	return e
}

// RecordLaunch records requested, sent and confirmed native selections separately.
func (s *Service) RecordLaunch(ctx context.Context, id string, launch domain.InstructorLaunch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, e := s.get(ctx, id)
	if e != nil {
		return e
	}
	if launch.SessionID == "" && launch.ReviewID == "" {
		return invalid("launch needs session or review identity")
	}
	if _, ok := w.Policy.Roles[launch.Role]; !ok {
		return invalid("launch role is undeclared")
	}
	if launch.SessionID != "" {
		reader, ok := s.deps.Store.(sessionAdmissionStore)
		if !ok {
			return invalid("session instance reader unavailable")
		}
		rec, instance, found, err := reader.InstructorSessionForMembership(ctx, launch.SessionID)
		if err != nil {
			return err
		}
		if !found {
			return invalid("session instance missing")
		}
		bound, managed, err := s.WorkForSession(ctx, launch.SessionID)
		if err != nil {
			return err
		}
		if !managed || bound.ID != w.ID {
			return invalid("session instance is not admitted to this work")
		}
		if launch.SessionInstanceID != "" && launch.SessionInstanceID != instance {
			return invalid("launch session instance changed")
		}
		launch.SessionInstanceID = instance
		launch.RuntimeGeneration = sessionRuntimeGeneration(rec)
	} else if reviews, ok := s.deps.Store.(memberReviewReader); ok {
		review, found, err := reviews.GetReviewByID(ctx, launch.ReviewID)
		if err != nil {
			return err
		}
		if !found || review.ProjectID != w.ProjectID {
			return invalid("review identity missing or belongs to another project")
		}
		// Restoring the retained reviewer conversation may have no active run.
		// That launch is recorded without inventing a completed review pass.
		if launch.ReviewRunID != "" {
			run, found, err := reviews.GetReviewRun(ctx, launch.ReviewRunID)
			if err != nil {
				return err
			}
			if !found || run.ReviewID != review.ID || run.SessionID != review.SessionID {
				return invalid("review run identity mismatch")
			}
		}
		bound, managed, err := s.WorkForSession(ctx, review.SessionID)
		if err != nil {
			return err
		}
		if !managed || bound.ID != w.ID {
			return invalid("review worker is not admitted to this work")
		}
		reader, ok := s.deps.Store.(sessionInstanceReader)
		if !ok {
			return invalid("review worker instance reader unavailable")
		}
		instance, found, err := reader.InstructorSessionInstance(ctx, review.SessionID)
		if err != nil {
			return err
		}
		if !found {
			return invalid("review worker instance missing")
		}
		launch.OwnerSessionID = review.SessionID
		launch.SessionInstanceID = instance
		launch.ReviewCreatedAt = &review.CreatedAt
		// The real launch callback can precede the engine's persisted review
		// update. Preserve its exact launch token instead of capturing the old
		// retained reviewer's generation during replacement.
		if launch.RuntimeGeneration == "" {
			launch.RuntimeGeneration = review.ReviewerLaunchID
			if launch.RuntimeGeneration == "" {
				launch.RuntimeGeneration = review.ControllerGeneration
			}
		}
	}
	if launch.LaunchedAt.IsZero() {
		launch.LaunchedAt = s.now()
	}
	launch.Phase = w.Phase
	launch.PhaseSequence = w.PhaseSequence
	w.Launches = append(w.Launches, launch)
	_, e = s.save(ctx, w, w.Revision, "launch_recorded", launch.Role)
	if e == nil && w.Cancellation != nil {
		return invalid("work cancellation was admitted during launch; captured native effect must stop")
	}
	return e
}

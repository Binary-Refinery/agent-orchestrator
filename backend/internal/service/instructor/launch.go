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
	if w.State == "done" || w.State == "cancelled" || w.State == "parked" || w.State == "blocked" || w.State == "waiting_human" {
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
	if role == "orchestrator" || role == "instructor" {
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
	if err := validateWorkExceptions(w, s.now()); err != nil {
		return err
	}
	if e := ValidatePolicy(w.Policy); e != nil {
		return e
	}
	if w.PolicyVersion != PolicyVersion(w.Policy) {
		return invalid("pinned work policy hash mismatch")
	}
	if w.State == "done" || w.State == "cancelled" || w.State == "parked" || w.State == "blocked" || w.State == "waiting_human" {
		return invalid("work may not resume in current state")
	}
	role, adopted, err := s.sessionRole(ctx, w, id)
	if err != nil {
		return err
	}
	if role == "worker" {
		if err := correctiveWorkerAdmission(w); err != nil {
			return err
		}
	}
	if adopted != nil {
		// Adoption establishes an assignment, not requested/sent/confirmed launch
		// evidence. Validate the existing persisted native selection directly.
		return s.validateRoleSelection(ctx, w, role, string(adopted.Harness), adopted.Metadata.Model, adopted.Metadata.Effort)
	}
	return nil
}

func (s *Service) sessionRole(ctx context.Context, w domain.InstructorWork, id domain.SessionID) (string, *domain.SessionRecord, error) {
	for i := len(w.Launches) - 1; i >= 0; i-- {
		if w.Launches[i].SessionID == id {
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
	w.State = "blocked"
	w.Summary = "Start abgelehnt"
	if cause != nil {
		w.WaitCondition = cause.Error()
	}
	_, e = s.save(ctx, w, w.Revision, "launch_failed", w.WaitCondition)
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
	if launch.LaunchedAt.IsZero() {
		launch.LaunchedAt = s.now()
	}
	launch.Phase = w.Phase
	launch.PhaseSequence = w.PhaseSequence
	w.Launches = append(w.Launches, launch)
	_, e = s.save(ctx, w, w.Revision, "launch_recorded", launch.Role)
	return e
}

package instructor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

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
	if in.WorkID == "" && in.ParentSessionID != "" {
		parent, ok, e := s.WorkForSession(ctx, in.ParentSessionID)
		if e != nil {
			return "", e
		}
		if ok {
			for _, l := range parent.Launches {
				if l.SessionID == in.ParentSessionID && l.Role != "orchestrator" && l.Role != "instructor" {
					in.WorkID = parent.ID
				}
			}
		}
	}
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
		if in.Name == "" {
			if in.SourceRef == "" {
				in.SourceRef = s.deps.NewID()
			}
			digest := sha256.Sum256([]byte(string(in.ProjectID) + "\x00" + in.Source + "\x00" + in.SourceRef))
			in.Name = "Auftrag " + hex.EncodeToString(digest[:])[:10]
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
	role, ok := w.Policy.Roles[in.Role]
	if !ok {
		return w.ID, invalid(fmt.Sprintf("role %s is not declared in pinned policy", in.Role))
	}
	if role.Harness != "" && role.Harness != in.Harness {
		return w.ID, invalid(fmt.Sprintf("role %s requires harness %s", in.Role, role.Harness))
	}
	if role.Model != "" && role.Model != in.Model {
		return w.ID, invalid("launch model does not match pinned role")
	}
	if in.Model == "" || in.Effort == "" {
		return w.ID, invalid("managed role needs explicit model and native effort")
	}
	if role.Effort == "maximum" {
		expected := role.EffortByModel[in.Model]
		if expected == "" {
			return w.ID, invalid("maximum requires explicit approved native effort mapping for this model")
		}
		if expected != in.Effort {
			return w.ID, invalid("launch effort does not match approved maximum mapping")
		}
	} else if role.Effort != "configured" && role.Effort != in.Effort {
		return w.ID, invalid("launch effort does not match pinned role")
	}
	if s.deps.RoleValidator == nil {
		return w.ID, invalid("native model capability validator unavailable")
	}
	if err := s.deps.RoleValidator.ValidateInstructorAgentConfig(ctx, in.ProjectID, role, in.Harness, in.Model, in.Effort); err != nil {
		return w.ID, err
	}
	return w.ID, nil
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
	if sessionRole(w, id) == "worker" {
		return correctiveWorkerAdmission(w)
	}
	return nil
}

func sessionRole(w domain.InstructorWork, id domain.SessionID) string {
	for i := len(w.Launches) - 1; i >= 0; i-- {
		if w.Launches[i].SessionID == id {
			return w.Launches[i].Role
		}
	}
	return ""
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

package instructor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/gitread"
)

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 16384 {
		remaining := 16384 - b.Len()
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
func gitFact(ctx context.Context, path string, args ...string) (string, error) {
	b, e := gitread.Run(ctx, path, args...)
	return strings.TrimSpace(string(b)), e
}

// RunChecks produces daemon-owned evidence only after executing approved argv
// against the attached producer's clean, immutable commit. Agent assertions
// never become verified check evidence.
func (s *Service) RunChecks(ctx context.Context, id string, producer domain.SessionID, profiles []string) (domain.InstructorWork, error) {
	w, err := s.get(ctx, id)
	if err != nil {
		return w, err
	}
	checkStore, ok := s.deps.Store.(interface {
		CaptureInstructorCheckProducer(context.Context, string, domain.SessionID) (domain.SessionRecord, domain.InstructorReportAdmission, bool, error)
		ValidateInstructorCheckProducer(context.Context, domain.InstructorReportAdmission, domain.SessionRecord) error
	})
	if !ok {
		return w, invalid("atomic native check producer admission unavailable")
	}
	producerRecord, admission, managed, err := checkStore.CaptureInstructorCheckProducer(ctx, w.ID, producer)
	if err != nil {
		return w, err
	}
	if !managed || producerRecord.Kind != domain.KindWorker {
		return w, invalid("check producer is not attached")
	}
	if w.State == "done" || w.State == "cancelled" || w.State == "parked" || w.State == "blocked" || w.State == "waiting_human" {
		return w, invalid("checks cannot execute while work is paused or terminal")
	}
	if err := validateWorkExceptions(w, s.now()); err != nil {
		return w, err
	}
	instance := admission.InstanceID
	selection := domain.InstructorAgentSelection{}
	var latest *domain.InstructorLaunch
	for _, launch := range w.Launches {
		if launch.SessionID == producer && (instance == "" || launch.SessionInstanceID == instance) {
			cloned := launch
			latest = &cloned
		}
	}
	if latest != nil && latest.Role == "worker" {
		if latest.Confirmed != nil && *latest.Confirmed == latest.Requested && latest.Status == "native-config-reported" {
			selection = *latest.Confirmed
		} else if latest.Sent != nil && *latest.Sent == latest.Requested && (latest.Status == "launched" || latest.Status == "sent") {
			selection = *latest.Sent
		}
	}
	role := w.Policy.Roles["worker"]
	expectedEffort := role.Effort
	if expectedEffort == "maximum" {
		expectedEffort = role.EffortByModel[selection.Model]
	}
	if selection.Model == "" || selection.Effort == "" || selection.Harness != role.Harness || role.Model != "" && role.Model != selection.Model || expectedEffort == "" || expectedEffort != "configured" && expectedEffort != selection.Effort {
		return w, invalid("checks require a successful worker launch using the pinned native model and effort")
	}
	if s.deps.Workspace == nil {
		return w, invalid("workspace resolver unavailable")
	}
	if leaser, ok := s.deps.Workspace.(interface {
		LeaseInstructorProducer(context.Context, domain.SessionID, string) (func(), error)
	}); ok {
		release, err := leaser.LeaseInstructorProducer(ctx, producer, admission.InstanceID)
		if err != nil {
			return w, err
		}
		defer release()
	}
	var path string
	if captured, ok := s.deps.Workspace.(interface {
		WorkspaceForInstructorProducer(context.Context, domain.SessionRecord) (string, error)
	}); ok {
		path, err = captured.WorkspaceForInstructorProducer(ctx, producerRecord)
	} else {
		path, err = s.deps.Workspace.WorkspaceForSession(ctx, producer)
	}
	if err == nil {
		err = checkStore.ValidateInstructorCheckProducer(ctx, admission, producerRecord)
	}
	if err != nil {
		return w, err
	}
	sha, err := gitFact(ctx, path, "rev-parse", "HEAD")
	if err != nil || !fullSHA(sha) {
		return w, invalid("checks require a committed Git workspace")
	}
	dirty, err := gitFact(ctx, path, "status", "--porcelain")
	if err != nil || dirty != "" {
		return w, invalid("checks require a clean worktree to bind evidence to SHA")
	}
	if w.SHA != "" && w.SHA != sha {
		if w.Phase != "implement" && w.Phase != "fix" {
			return w, invalid("workspace SHA differs from current work SHA")
		}
		s.mu.Lock()
		current, readErr := s.get(ctx, w.ID)
		if readErr == nil && (current.PhaseSequence != w.PhaseSequence || current.PolicyVersion != w.PolicyVersion || current.SHA != w.SHA || current.State != "ready" && current.State != "running") {
			readErr = ErrConflict
		}
		if readErr == nil {
			current.SHA, current.ProgressAt = sha, s.now()
			w, readErr = s.saveProducer(ctx, current, current.Revision, "worker_commit_observed", string(producer)+" runtime-bound clean HEAD "+sha, admission, "")
		}
		s.mu.Unlock()
		if readErr != nil {
			return w, readErr
		}
	}
	if len(profiles) == 0 {
		profiles = []string{"push", "full"}
	}
	evidence := []domain.InstructorEvidence{}
	for _, name := range profiles {
		pf, ok := w.Policy.Profiles[name]
		if !ok || len(pf.Command) == 0 {
			return w, invalid(fmt.Sprintf("profile %s has no approved check command", name))
		}
		if err := checkStore.ValidateInstructorCheckProducer(ctx, admission, producerRecord); err != nil {
			return w, err
		}
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(pf.TimeoutSeconds)*time.Second)
		cmd := exec.CommandContext(runCtx, pf.Command[0], pf.Command[1:]...)
		cmd.Dir = path
		var output boundedOutput
		cmd.Stdout = &output
		cmd.Stderr = &output
		runErr := cmd.Run()
		cancel()
		if actorErr := checkStore.ValidateInstructorCheckProducer(ctx, admission, producerRecord); actorErr != nil {
			s.mu.Lock()
			current, readErr := s.get(ctx, id)
			if readErr == nil && current.State != "done" && current.State != "cancelled" && current.State != "parked" {
				current.State, current.Summary, current.WaitCondition = "blocked", "Check-Wirkung nach Controllerwechsel unklar", actorErr.Error()
				_, _ = s.save(ctx, current, current.Revision, "check_effect_uncertain", string(producer)+" capturedRuntime="+admission.RuntimeID)
			}
			s.mu.Unlock()
			return w, actorErr
		}
		if runErr != nil {
			s.mu.Lock()
			current, e := s.get(ctx, id)
			if e == nil {
				for i := range current.Evidence {
					prior := &current.Evidence[i]
					if prior.Verified && !prior.Revoked && prior.Kind == "check" && prior.Profile == name && prior.SHA == sha && prior.PolicyVersion == w.PolicyVersion {
						prior.Revoked = true
						prior.RevocationReason = "approved native profile failed on the same commit: " + runErr.Error()
					}
				}
				current.Summary = "Prüfprofil " + name + " fehlgeschlagen"
				_, e = s.saveProducer(ctx, current, current.Revision, "check_failed", name+": "+runErr.Error()+"\n"+output.String(), admission, "")
			}
			s.mu.Unlock()
			return w, errors.Join(runErr, e)
		}
		after, e := gitFact(ctx, path, "rev-parse", "HEAD")
		if e != nil || after != sha {
			return w, invalid("commit changed during check execution")
		}
		dirty, e = gitFact(ctx, path, "status", "--porcelain")
		if e != nil || dirty != "" {
			return w, invalid("worktree changed during check execution")
		}
		eventID := s.deps.NewID()
		evidence = append(evidence, domain.InstructorEvidence{ID: eventID, WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: sha, Kind: "check", Phase: w.Phase, PhaseSequence: w.PhaseSequence, Profile: name, Producer: producer, Role: "worker", Harness: selection.Harness, Model: selection.Model, Effort: selection.Effort, Ref: "instructor-check:" + eventID, Verified: true, CreatedAt: s.now()})
		s.mu.Lock()
		current, e := s.get(ctx, id)
		if e == nil {
			_, e = s.saveProducer(ctx, current, current.Revision, "check_executed", name+" "+sha+"\n"+output.String(), admission, "")
		}
		s.mu.Unlock()
		if e != nil {
			return w, e
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(ctx, id)
	if err != nil {
		return current, err
	}
	if current.PolicyVersion != w.PolicyVersion || current.PhaseSequence != w.PhaseSequence || (current.SHA != "" && current.SHA != sha) {
		return current, ErrConflict
	}
	if current.State == "done" || current.State == "cancelled" || current.State == "parked" || current.State == "blocked" || current.State == "waiting_human" {
		return current, ErrConflict
	}
	current.SHA = sha
	current.Evidence = append(current.Evidence, evidence...)
	current.ProgressAt = s.now()
	return s.saveProducer(ctx, current, current.Revision, "checks_verified", strings.Join(profiles, ","), admission, "")
}

// VerifyEvidence ignores a client's verified flag. The injected verifier reads actual
// persisted review/CI facts and independently attest the exact current SHA.
func (s *Service) VerifyEvidence(ctx context.Context, id string, e domain.InstructorEvidence) (domain.InstructorWork, error) {
	w, err := s.get(ctx, id)
	if err != nil {
		return w, err
	}
	if s.deps.Verifier == nil {
		return w, invalid("trusted evidence verifier unavailable")
	}
	if e.SHA != w.SHA || !fullSHA(e.SHA) || e.PolicyVersion != w.PolicyVersion || e.WorkID != w.ID {
		return w, invalid("evidence must bind work, policy and full current SHA")
	}
	role, ok := w.Policy.Roles[e.Role]
	if !ok {
		return w, invalid("evidence role is undeclared")
	}
	if role.Independent {
		for _, l := range w.Launches {
			if l.Role == "worker" && string(l.SessionID) == string(e.Producer) {
				return w, invalid("review evidence must have an independent producer")
			}
		}
	}
	e.Verified = false
	e.Revoked = false
	e.RevocationReason = ""
	e.Phase = w.Phase
	e.PhaseSequence = w.PhaseSequence
	if e.Kind == "attribution" || e.Kind == "task-packet" || e.Kind == "ci" || e.Kind == "ci-failed" || e.Kind == "traceability" {
		e.Phase, e.PhaseSequence = "", 0
	}
	e.Outcome = ""
	e.CriticalFindings = 0
	if assessor, ok := s.deps.Verifier.(EvidenceAssessor); ok {
		assessed, err := assessor.Assess(ctx, w, e)
		if err != nil {
			return w, err
		}
		if assessed.WorkID != e.WorkID || assessed.PolicyVersion != e.PolicyVersion || assessed.SHA != e.SHA || assessed.Kind != e.Kind || assessed.Ref != e.Ref || assessed.Producer != e.Producer || assessed.CriticalFindings < 0 {
			return w, invalid("trusted assessment changed immutable evidence binding")
		}
		e = assessed
	} else if err := s.deps.Verifier.Verify(ctx, w, e); err != nil {
		return w, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(ctx, id)
	if err != nil {
		return current, err
	}
	if current.SHA != w.SHA || current.PolicyVersion != w.PolicyVersion {
		return current, ErrConflict
	}
	// Inspection revalidates mutable sources, but a repeated identical trusted
	// fact is availability, not new work progress or a new human-visible revision.
	for _, prior := range current.Evidence {
		if prior.Verified && !prior.Revoked && sameEvidenceFact(prior, e) {
			return current, nil
		}
	}
	e.ID = s.deps.NewID()
	e.CreatedAt = s.now()
	e.Verified = true
	e.Revoked = false
	e.RevocationReason = ""
	current.Evidence = append(current.Evidence, e)
	if (e.Kind == "review-report" || e.Kind == "triage") && e.CriticalFindings > current.OpenCriticalFindings {
		current.OpenCriticalFindings = e.CriticalFindings
	}
	if e.Kind == "delta" && e.Outcome == "approved" && e.CriticalFindings == 0 && e.Phase == "delta" && e.PhaseSequence == current.DeltaPhaseSequence {
		current.OpenCriticalFindings = 0
	}
	current.ProgressAt = s.now()
	return s.save(ctx, current, current.Revision, "evidence_verified", e.Kind+":"+e.Ref)
}

func sameEvidenceFact(a, b domain.InstructorEvidence) bool {
	return a.WorkID == b.WorkID && a.PolicyVersion == b.PolicyVersion && a.SHA == b.SHA &&
		a.Kind == b.Kind && a.Phase == b.Phase && a.PhaseSequence == b.PhaseSequence && a.Profile == b.Profile &&
		a.Producer == b.Producer && a.Role == b.Role && a.Harness == b.Harness && a.Model == b.Model &&
		a.Effort == b.Effort && a.Ref == b.Ref && a.Outcome == b.Outcome && a.CriticalFindings == b.CriticalFindings
}

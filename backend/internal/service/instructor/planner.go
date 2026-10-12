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

// Planner performs native strategic analysis without executing project work.
// The callback records sent choices separately from actual provider readback.
type Planner interface {
	Run(context.Context, domain.InstructorPlanJob, domain.InstructorSnapshot, []domain.InstructorPlanJob, func(domain.InstructorAgentSelection, *domain.InstructorAgentSelection) error) (domain.InstructorPlanOutput, error)
}

type planningStore interface {
	CreateInstructorPlan(context.Context, domain.InstructorPlanJob) (domain.InstructorPlanJob, error)
	GetInstructorPlan(context.Context, string) (domain.InstructorPlanJob, bool, error)
	ListInstructorPlans(context.Context) ([]domain.InstructorPlanJob, error)
	ClaimInstructorPlan(context.Context, time.Time) (domain.InstructorPlanJob, bool, error)
	SaveInstructorPlan(context.Context, domain.InstructorPlanJob) error
}

// SetPlanner installs the native adapter before the daemon starts scheduling.
func (s *Service) SetPlanner(planner Planner) { s.deps.Planner = planner }

// Plan queues one durable strategic request and immediately returns its identity.
func (s *Service) Plan(ctx context.Context, in domain.InstructorPlanInput) (domain.InstructorPlanJob, error) {
	var job domain.InstructorPlanJob
	in.Request = strings.TrimSpace(in.Request)
	if in.Request == "" || len(in.Request) > 16384 || in.SnapshotID == "" || in.IdempotencyKey == "" || len(in.IdempotencyKey) > 256 {
		return job, invalid("planning requires bounded request, snapshotId and idempotencyKey")
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 300
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 900 {
		return job, invalid("planning timeout must be between 1 and 900 seconds")
	}
	store, ok := s.deps.Store.(planningStore)
	if !ok || s.deps.Planner == nil {
		return job, invalid("native Instructor planning unavailable")
	}
	digest := sha256.Sum256([]byte(in.IdempotencyKey))
	job.ID = "plan-" + hex.EncodeToString(digest[:])
	data, _ := json.Marshal(in)
	requestHash := sha256.Sum256(data)
	job.RequestHash = hex.EncodeToString(requestHash[:])
	existing, found, err := store.GetInstructorPlan(ctx, job.ID)
	if err != nil {
		return job, err
	}
	if found {
		if existing.RequestHash != job.RequestHash {
			return job, ErrConflict
		}
		return existing, nil
	}
	snapshot, found, err := s.deps.Store.GetInstructorSnapshot(ctx, in.SnapshotID)
	if err != nil {
		return job, err
	}
	if !found {
		return job, ErrNotFound
	}
	if snapshot.BaseID != "" {
		return job, invalid("planning requires a full portfolio snapshot")
	}
	policy, err := s.GetPolicy(ctx)
	if err != nil {
		return job, err
	}
	role, ok := policy.Roles["instructor"]
	if !ok {
		return job, invalid("Instructor role is not configured")
	}
	effort := role.EffortByModel[role.Model]
	if effort == "" && role.Effort != "maximum" && role.Effort != "configured" {
		effort = role.Effort
	}
	if role.Harness == "" || role.Model == "" || effort == "" || s.deps.RoleValidator == nil {
		return job, invalid("Instructor requires explicit native model, effort and capability validation")
	}
	if err := s.deps.RoleValidator.ValidateInstructorAgentConfig(ctx, "", role, role.Harness, role.Model, effort); err != nil {
		return job, err
	}
	job.Input = in
	job.PolicyVersion = policy.Version
	job.Requested = domain.InstructorAgentSelection{Harness: role.Harness, Model: role.Model, Effort: effort}
	job.State = "queued"
	job.CreatedAt = s.now()
	job.UpdatedAt = job.CreatedAt
	saved, err := store.CreateInstructorPlan(ctx, job)
	if err == nil && saved.RequestHash != job.RequestHash {
		return saved, ErrConflict
	}
	return saved, err
}

// GetPlan reads one global strategic job without waiting for model inference.
func (s *Service) GetPlan(ctx context.Context, id string) (domain.InstructorPlanJob, error) {
	store, ok := s.deps.Store.(planningStore)
	if !ok {
		return domain.InstructorPlanJob{}, invalid("planning storage unavailable")
	}
	job, found, err := store.GetInstructorPlan(ctx, id)
	if err != nil {
		return job, err
	}
	if !found {
		return job, ErrNotFound
	}
	return job, nil
}

// ListPlans returns the durable dialogue journal without provider calls.
func (s *Service) ListPlans(ctx context.Context) ([]domain.InstructorPlanJob, error) {
	store, ok := s.deps.Store.(planningStore)
	if !ok {
		return nil, invalid("planning storage unavailable")
	}
	return store.ListInstructorPlans(ctx)
}

func (s *Service) schedulePlanning(ctx context.Context) error {
	store, ok := s.deps.Store.(planningStore)
	if !ok || s.deps.Planner == nil {
		return nil
	}
	s.verificationMu.Lock()
	defer s.verificationMu.Unlock()
	if s.verificationStopped || s.planningRunning {
		return nil
	}
	job, claimed, err := store.ClaimInstructorPlan(ctx, s.now())
	if err != nil || !claimed {
		return err
	}
	s.planningRunning = true
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(job.Input.TimeoutSeconds)*time.Second)
	s.verificationCancels[job.ID] = cancel
	s.verificationWG.Add(1)
	go func() {
		defer s.verificationWG.Done()
		defer cancel()
		defer func() {
			s.verificationMu.Lock()
			delete(s.verificationCancels, job.ID)
			s.planningRunning = false
			s.verificationMu.Unlock()
		}()
		s.runPlanning(runCtx, store, job)
	}()
	return nil
}

func (s *Service) runPlanning(ctx context.Context, store planningStore, job domain.InstructorPlanJob) {
	finish := func(err error) {
		job.UpdatedAt = s.now()
		if err != nil {
			job.State = "failed"
			job.Error = err.Error()
			if ctx.Err() != nil {
				job.State = "interrupted"
			}
		} else {
			job.State = "completed"
		}
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = store.SaveInstructorPlan(writeCtx, job)
	}
	policy, err := s.GetPolicy(ctx)
	if err != nil {
		finish(err)
		return
	}
	if policy.Version != job.PolicyVersion {
		finish(ErrConflict)
		return
	}
	role := policy.Roles["instructor"]
	if s.deps.RoleValidator == nil {
		finish(invalid("native capability validator unavailable"))
		return
	}
	if err = s.deps.RoleValidator.ValidateInstructorAgentConfig(ctx, "", role, job.Requested.Harness, job.Requested.Model, job.Requested.Effort); err != nil {
		finish(err)
		return
	}
	snapshot, found, err := s.deps.Store.GetInstructorSnapshot(ctx, job.Input.SnapshotID)
	if err != nil {
		finish(err)
		return
	}
	if !found {
		finish(ErrNotFound)
		return
	}
	history, err := store.ListInstructorPlans(ctx)
	if err != nil {
		finish(err)
		return
	}
	output, err := s.deps.Planner.Run(ctx, job, snapshot, history, func(sent domain.InstructorAgentSelection, confirmed *domain.InstructorAgentSelection) error {
		if sent != job.Requested || (confirmed != nil && *confirmed != job.Requested) {
			return invalid("native Instructor selection differs from the approved request")
		}
		job.Sent = &sent
		job.Confirmed = confirmed
		job.UpdatedAt = s.now()
		return store.SaveInstructorPlan(ctx, job)
	})
	if err == nil {
		err = s.validatePlanOutput(ctx, snapshot, output)
	}
	if err == nil {
		job.Output = &output
	}
	finish(err)
}

func (s *Service) validatePlanOutput(ctx context.Context, snapshot domain.InstructorSnapshot, out domain.InstructorPlanOutput) error {
	if strings.TrimSpace(out.Summary) == "" || len(out.Summary) > 32768 || len(out.Questions) > 20 || len(out.Proposals) > 50 || len(out.Intake) > 20 {
		return invalid("native Instructor output exceeds its advisory contract")
	}
	for _, q := range out.Questions {
		if len(q) > 4096 {
			return invalid("Instructor question is too long")
		}
	}
	works := map[string]bool{}
	for _, w := range snapshot.Works {
		works[w.ID] = true
	}
	for _, proposal := range out.Proposals {
		if strings.TrimSpace(proposal.Reason) == "" || len(proposal.Reason) > 4096 || len(proposal.Command) > 256 || len(proposal.WorkIDs) > 500 {
			return invalid("invalid Instructor proposal")
		}
		registry, conflicts := s.registryForWorks(snapshot.Works)
		if conflictingCommand(proposal.Command, conflicts) {
			return invalid("Instructor proposal has conflicting project policies")
		}
		spec, selector, err := resolveCommandLine(proposal.Command, registry)
		if err != nil || selector != "" {
			return invalid("Instructor proposals require a registry command and typed workIds")
		}
		if spec.Action == "neu" {
			return invalid("use typed intake drafts for new work")
		}
		seen := map[string]bool{}
		for _, id := range proposal.WorkIDs {
			if !works[id] || seen[id] {
				return invalid("Instructor proposal names an unknown or duplicate work")
			}
			seen[id] = true
		}
	}
	for _, draft := range out.Intake {
		if draft.ProjectID == "" || strings.TrimSpace(draft.Name) == "" || strings.TrimSpace(draft.Goal) == "" || len(draft.Name) > 120 || len(draft.Goal) > 8192 {
			return invalid("invalid Instructor intake draft")
		}
		projects, ok := s.deps.Store.(interface {
			GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
		})
		if !ok {
			return invalid("project catalog unavailable")
		}
		_, found, err := projects.GetProject(ctx, string(draft.ProjectID))
		if err != nil {
			return err
		}
		if !found {
			return invalid(fmt.Sprintf("unknown intake project %s", draft.ProjectID))
		}
	}
	return nil
}

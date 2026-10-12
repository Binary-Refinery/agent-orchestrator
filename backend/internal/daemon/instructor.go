package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

type instructorRuntimeStore interface {
	instructorsvc.Store
	ListProjects(context.Context) ([]domain.ProjectRecord, error)
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	ListAllSessions(context.Context) ([]domain.SessionRecord, error)
	ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error)
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
}

type instructorDispatchBinder interface {
	BindDispatchTarget(context.Context, string, domain.SessionID) error
}

type instructorDelivery struct {
	store interface {
		ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error)
		GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	}
	sessions reportSemanticSession
	binder   instructorDispatchBinder
}

// Deliver targets the real project orchestrator conversation. A durable dispatch
// key fences retries; transport acceptance is distinct from its work receipt.
func (d instructorDelivery) Deliver(ctx context.Context, dispatch domain.InstructorDispatch) error {
	if dispatch.ID == "" || dispatch.ProjectID == "" || dispatch.WorkID == "" {
		return errors.New("instructor dispatch identity is incomplete")
	}
	if dispatch.Action == "stopp" {
		return d.stopManagedWork(ctx, dispatch)
	}
	var target domain.SessionRecord
	if dispatch.TargetSessionID != "" {
		record, found, err := d.store.GetSession(ctx, dispatch.TargetSessionID)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("bound project orchestrator no longer exists")
		}
		target = record
	} else {
		records, err := d.store.ListSessions(ctx, dispatch.ProjectID)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.Kind != domain.KindOrchestrator || record.ProjectID != dispatch.ProjectID || record.IsTerminated || record.Activity.State == domain.ActivityExited {
				continue
			}
			if target.ID == "" || record.CreatedAt.After(target.CreatedAt) || (record.CreatedAt.Equal(target.CreatedAt) && record.ID > target.ID) {
				target = record
			}
		}
	}
	if target.ID == "" {
		return fmt.Errorf("%w: no live project orchestrator; start one explicitly", instructorsvc.ErrDeliveryRetryable)
	}
	if target.ProjectID != dispatch.ProjectID || target.Kind != domain.KindOrchestrator || target.IsTerminated || target.Activity.State == domain.ActivityExited {
		return errors.New("dispatch target is not a live orchestrator for this project")
	}
	if target.Activity.State == domain.ActivityBlocked {
		return fmt.Errorf("%w: project orchestrator is blocked on a human decision", instructorsvc.ErrDeliveryRetryable)
	}
	if d.binder == nil || d.sessions == nil {
		return errors.New("instructor delivery is unavailable")
	}
	if err := d.binder.BindDispatchTarget(ctx, dispatch.ID, target.ID); err != nil {
		return err
	}
	if binder, ok := d.binder.(interface {
		BindDispatchRuntime(context.Context, string, domain.SessionRecord) error
	}); ok {
		if err := binder.BindDispatchRuntime(ctx, dispatch.ID, target); err != nil {
			return fmt.Errorf("%w: %w", instructorsvc.ErrDeliveryUncertain, err)
		}
	}
	reader, ok := d.store.(interface {
		GetInstructorWork(context.Context, string) (domain.InstructorWork, bool, error)
	})
	if !ok {
		return errors.New("instructor work lookup is unavailable")
	}
	work, found, err := reader.GetInstructorWork(ctx, dispatch.WorkID)
	if err != nil {
		return err
	}
	if !found || work.ProjectID != dispatch.ProjectID {
		return errors.New("dispatch work does not exist in its project")
	}
	message := fmt.Sprintf("%s\n\nInstructor dispatch: %s; work: %s; target session: %s.\nReport actual acceptance after reading this directive with:\nao instructor report --work %q --session %q --kind accepted --dispatch %q\nReport meaningful execution progress separately with --kind progress and the same --dispatch. Submission alone is not acceptance. Delegate execution under the pinned work policy using ao spawn --work %q; never change its approval scope.\n", dispatch.Message, dispatch.ID, dispatch.WorkID, target.ID, dispatch.WorkID, target.ID, dispatch.ID, dispatch.WorkID)
	message += "\n" + instructorWorkInstructions(work)
	governed, ok := d.sessions.(interface {
		SendGovernedSemantic(context.Context, domain.SessionID, string, string) error
	})
	if !ok {
		return errors.New("governed semantic delivery is unavailable")
	}
	if err := governed.SendGovernedSemantic(ctx, target.ID, message, dispatch.ID); err != nil {
		if errors.Is(err, ports.ErrSemanticDeliveryRejected) {
			return fmt.Errorf("%w: %w", instructorsvc.ErrDeliveryRejected, err)
		}
		if errors.Is(err, ports.ErrSemanticDeliveryQueued) {
			return fmt.Errorf("%w: %w", instructorsvc.ErrDeliveryQueued, err)
		}
		if errors.Is(err, ports.ErrSemanticDeliveryUncertain) {
			return fmt.Errorf("%w: %w", instructorsvc.ErrDeliveryUncertain, err)
		}
		return err
	}
	return nil
}

type instructorInventory struct {
	store instructorRuntimeStore
	cache *instructorInventoryCache
}

func (i instructorInventory) collectInventoryMetadata(ctx context.Context) (controllers.InstructorInventoryResponse, error) {
	projects, err := i.store.ListProjects(ctx)
	if err != nil {
		return controllers.InstructorInventoryResponse{}, err
	}
	sessions, err := i.store.ListAllSessions(ctx)
	if err != nil {
		return controllers.InstructorInventoryResponse{}, err
	}
	links := map[domain.SessionID][]string{}
	for _, session := range sessions {
		work, assigned, e := i.store.InstructorWorkForSession(ctx, session.ID)
		if e != nil {
			return controllers.InstructorInventoryResponse{}, e
		}
		if assigned {
			links[session.ID] = []string{work.ID}
		}
	}

	result := controllers.InstructorInventoryResponse{CapturedAt: time.Now().UTC(), Atomic: false, Projects: []controllers.InstructorProjectInventory{}, Sessions: []controllers.InstructorSessionInventory{}}
	for _, project := range projects {
		result.Projects = append(result.Projects, controllers.InstructorProjectInventory{ID: project.ID, Name: project.DisplayName, Path: project.Path, Kind: string(project.Kind.WithDefault())})
	}
	for _, session := range sessions {
		workIDs := links[session.ID]
		assignment := "linked"
		if len(workIDs) == 0 {
			assignment = "unassigned"
			workIDs = []string{}
			result.UnassignedCount++
		}
		sort.Strings(workIDs)
		result.Sessions = append(result.Sessions, controllers.InstructorSessionInventory{ID: string(session.ID), ProjectID: string(session.ProjectID), Kind: string(session.Kind), Harness: string(session.Harness), Mode: string(session.Mode), DisplayName: session.DisplayName, ActivityState: string(session.Activity.State), IsTerminated: session.IsTerminated, IssueID: string(session.IssueID), Branch: session.Metadata.Branch, WorkspacePath: session.Metadata.WorkspacePath, WorkspaceRepoPath: session.Metadata.WorkspaceRepoPath, WorkIDs: workIDs, Assignment: assignment, CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt})
	}
	sort.Slice(result.Projects, func(a, b int) bool { return result.Projects[a].ID < result.Projects[b].ID })
	sort.Slice(result.Sessions, func(a, b int) bool { return result.Sessions[a].ID < result.Sessions[b].ID })
	return result, nil
}

type instructorWorkspace struct {
	store interface {
		GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	}
	sessions interface {
		AcquireSessionInput(domain.SessionID) (func(), bool)
	}
}

func (w instructorWorkspace) LeaseInstructorProducer(ctx context.Context, id domain.SessionID, instance string) (func(), error) {
	if w.sessions == nil {
		return nil, errors.New("native check session operation lease unavailable")
	}
	release, ok := w.sessions.AcquireSessionInput(id)
	if !ok {
		return nil, errors.New("native check producer has an exclusive runtime operation")
	}
	reader, ok := w.store.(interface {
		InstructorSessionInstance(context.Context, domain.SessionID) (string, bool, error)
	})
	if !ok {
		release()
		return nil, errors.New("native check incarnation lookup unavailable")
	}
	current, found, err := reader.InstructorSessionInstance(ctx, id)
	if err != nil || !found || current != instance {
		release()
		if err != nil {
			return nil, err
		}
		return nil, domain.ErrInstructorProducerChanged
	}
	return release, nil
}

func (w instructorWorkspace) WorkspaceForSession(ctx context.Context, id domain.SessionID) (string, error) {
	session, found, err := w.store.GetSession(ctx, id)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("session workspace does not exist")
	}
	return w.WorkspaceForInstructorProducer(ctx, session)
}

func (w instructorWorkspace) WorkspaceForInstructorProducer(_ context.Context, session domain.SessionRecord) (string, error) {
	path := session.Metadata.WorkspaceRepoPath
	if path == "" {
		path = session.Metadata.WorkspacePath
	}
	if path == "" {
		return "", errors.New("session has no recorded workspace")
	}
	return path, nil
}

type instructorGovernance struct {
	svc       *instructorsvc.Service
	store     instructorRuntimeStore
	validator instructorsvc.RoleValidator
}

func (g instructorGovernance) effectiveProjectPolicy(ctx context.Context, project domain.ProjectRecord) (domain.InstructorPolicy, error) {
	policy, err := g.svc.GetPolicy(ctx)
	if err != nil {
		return policy, err
	}
	if project.Config.GovernanceFile != "" {
		return instructorsvc.LoadPolicy(project.Path, project.Config.GovernanceFile)
	}
	repositoryPolicy, err := instructorsvc.LoadPolicy(project.Path, ".ao/governance.json")
	if err == nil {
		return repositoryPolicy, nil
	}
	if !os.IsNotExist(err) {
		return policy, err
	}
	return policy, nil
}

func (g instructorGovernance) PrepareLaunch(ctx context.Context, cfg ports.SpawnConfig, project domain.ProjectRecord) (string, error) {
	role := cfg.WorkRole
	if role == "" {
		role = string(cfg.Kind)
	}
	if cfg.Kind == domain.KindOrchestrator && role != string(domain.KindOrchestrator) {
		return "", fmt.Errorf("%w: a project orchestrator requires the orchestrator role", instructorsvc.ErrValidation)
	}
	workID, err := g.svc.ResolveSpawnWork(ctx, cfg.ParentSessionID, cfg.WorkID, role)
	if err != nil {
		return workID, err
	}
	cfg.WorkID = workID
	if cfg.Kind == domain.KindOrchestrator && cfg.WorkID == "" && cfg.AutomationRunID == nil {
		policy, err := g.effectiveProjectPolicy(ctx, project)
		if err != nil {
			return "", err
		}
		selection, found := policy.Roles[role]
		if !found {
			return "", errors.New("project orchestrator role is not declared in governance policy")
		}
		return "", g.validator.ValidateInstructorAgentConfig(ctx, cfg.ProjectID, selection, string(cfg.Harness), cfg.AgentConfig.Model, cfg.AgentConfig.Effort)
	}
	source, reference := "manual", ""
	if cfg.IssueID != "" {
		source, reference = "issue", string(cfg.IssueID)
	}
	if cfg.AutomationRunID != nil {
		source, reference = "automation", string(*cfg.AutomationRunID)
	}
	return g.svc.PrepareSpawn(ctx, instructorsvc.SpawnInput{ProjectID: cfg.ProjectID, ProjectPath: project.Path, GovernanceFile: project.Config.GovernanceFile, WorkID: cfg.WorkID, ParentSessionID: cfg.ParentSessionID, IssueID: cfg.IssueID, Source: source, SourceRef: reference, Name: cfg.DisplayName, Goal: cfg.Prompt, Role: role, Harness: string(cfg.Harness), Model: cfg.AgentConfig.Model, Effort: cfg.AgentConfig.Effort})
}

func (g instructorGovernance) AttachSession(ctx context.Context, workID string, id domain.SessionID, cfg ports.SpawnConfig) error {
	if err := g.svc.AttachLaunchingSession(ctx, workID, id, cfg.ParentSessionID); err != nil {
		return err
	}
	// The manager attaches before controller creation; this is requested launch
	// configuration, not proof of what the native provider finally selected.
	selection := domain.InstructorAgentSelection{Harness: string(cfg.Harness), Model: cfg.AgentConfig.Model, Effort: cfg.AgentConfig.Effort}
	role := cfg.WorkRole
	if role == "" {
		role = string(cfg.Kind)
	}
	return g.svc.RecordLaunch(ctx, workID, domain.InstructorLaunch{SessionID: id, Role: role, Requested: selection, Status: "requested"})
}

func (g instructorGovernance) ControllerStarted(ctx context.Context, workID string, id domain.SessionID, cfg ports.SpawnConfig) error {
	selection := domain.InstructorAgentSelection{Harness: string(cfg.Harness), Model: cfg.AgentConfig.Model, Effort: cfg.AgentConfig.Effort}
	role := cfg.WorkRole
	if role == "" {
		role = string(cfg.Kind)
	}
	return g.svc.RecordLaunch(ctx, workID, domain.InstructorLaunch{SessionID: id, Role: role, Requested: selection, Sent: &selection, Status: "launched"})
}

func (g instructorGovernance) ValidateResume(ctx context.Context, id domain.SessionID) error {
	work, managed, err := g.svc.WorkForSession(ctx, id)
	if err != nil {
		return err
	}
	if !managed {
		session, found, err := g.store.GetSession(ctx, id)
		if err != nil {
			return err
		}
		if !found || session.Kind != domain.KindOrchestrator {
			return nil
		}
		return g.ValidateAgentChange(ctx, id, session.Harness, ports.AgentConfig{Model: session.Metadata.Model, Effort: session.Metadata.Effort})
	} // Existing sessions remain explicitly unassigned.
	if err := g.svc.ValidateResume(ctx, id); err != nil {
		return err
	}
	session, roleName, _, err := g.managedSessionRole(ctx, work, id)
	if err != nil {
		return err
	}
	role, found := work.Policy.Roles[roleName]
	if !found {
		return errors.New("managed session role is not in its pinned policy")
	}
	return g.validator.ValidateInstructorAgentConfig(ctx, work.ProjectID, role, string(session.Harness), session.Metadata.Model, session.Metadata.Effort)
}

// managedSessionRole only uses launch authority captured for the current
// inserted row. Legacy or readmitted sessions fall back to their real kind,
// never to a deleted predecessor with the same display ID.
func (g instructorGovernance) managedSessionRole(ctx context.Context, work domain.InstructorWork, id domain.SessionID) (domain.SessionRecord, string, string, error) {
	reader, ok := g.store.(interface {
		InstructorSessionForMembership(context.Context, domain.SessionID) (domain.SessionRecord, string, bool, error)
	})
	if !ok {
		return domain.SessionRecord{}, "", "", errors.New("managed session incarnation lookup is unavailable")
	}
	session, instance, found, err := reader.InstructorSessionForMembership(ctx, id)
	if err != nil {
		return domain.SessionRecord{}, "", "", err
	}
	if !found || instance == "" || session.ProjectID != work.ProjectID {
		return domain.SessionRecord{}, "", "", fmt.Errorf("%w: managed session instance changed or is unavailable", instructorsvc.ErrConflict)
	}
	bindings, ok := g.store.(interface {
		InstructorWorkForSessionInstance(context.Context, domain.SessionID, string) (domain.InstructorWork, bool, error)
	})
	if !ok {
		return domain.SessionRecord{}, "", "", errors.New("managed session instance assignment lookup is unavailable")
	}
	bound, managed, err := bindings.InstructorWorkForSessionInstance(ctx, id, instance)
	if err != nil {
		return domain.SessionRecord{}, "", "", err
	}
	if !managed || bound.ID != work.ID || bound.PolicyVersion != work.PolicyVersion {
		return domain.SessionRecord{}, "", "", fmt.Errorf("%w: managed session assignment or pinned policy changed", instructorsvc.ErrConflict)
	}
	roleName := string(session.Kind)
	for _, launch := range bound.Launches {
		if launch.SessionID == id && launch.SessionInstanceID == instance {
			roleName = launch.Role
		}
	}
	return session, roleName, instance, nil
}

func (g instructorGovernance) SpawnFailed(ctx context.Context, id string, cause error) error {
	return g.svc.SpawnFailed(ctx, id, cause)
}

func (g instructorGovernance) SessionLaunchFailed(ctx context.Context, id domain.SessionID, cause error) error {
	work, managed, err := g.svc.WorkForSession(ctx, id)
	if err != nil || !managed {
		return err
	}
	return g.svc.SpawnFailed(ctx, work.ID, cause)
}

func (g instructorGovernance) ValidateAgentChange(ctx context.Context, id domain.SessionID, harness domain.AgentHarness, cfg ports.AgentConfig) error {
	work, managed, err := g.svc.WorkForSession(ctx, id)
	if err != nil {
		return err
	}
	if !managed {
		session, found, err := g.store.GetSession(ctx, id)
		if err != nil {
			return err
		}
		if !found || session.Kind != domain.KindOrchestrator {
			return nil
		}
		project, found, err := g.store.GetProject(ctx, string(session.ProjectID))
		if err != nil {
			return err
		}
		if !found {
			return errors.New("project orchestrator project no longer exists")
		}
		policy, err := g.effectiveProjectPolicy(ctx, project)
		if err != nil {
			return err
		}
		role, found := policy.Roles["orchestrator"]
		if !found {
			return errors.New("project orchestrator role is not in governance policy")
		}
		return g.validator.ValidateInstructorAgentConfig(ctx, session.ProjectID, role, string(harness), cfg.Model, cfg.Effort)
	}
	if err := g.svc.ValidateResume(ctx, id); err != nil {
		return err
	}
	_, roleName, _, err := g.managedSessionRole(ctx, work, id)
	if err != nil {
		return err
	}
	role, found := work.Policy.Roles[roleName]
	if !found {
		return errors.New("managed session role is not in its pinned policy")
	}
	return g.validator.ValidateInstructorAgentConfig(ctx, work.ProjectID, role, string(harness), cfg.Model, cfg.Effort)
}

func (g instructorGovernance) ValidateChatAgentConfig(ctx context.Context, id domain.SessionID, model, effort string) error {
	session, found, err := g.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if found {
		return g.ValidateAgentChange(ctx, id, session.Harness, ports.AgentConfig{Model: model, Effort: effort})
	}
	reader, ok := g.store.(interface {
		GetReviewByID(context.Context, string) (domain.Review, bool, error)
	})
	if !ok {
		return errors.New("reviewer ownership lookup is unavailable")
	}
	review, found, err := reader.GetReviewByID(ctx, string(id))
	if err != nil {
		return err
	}
	if !found {
		return errors.New("chat owner does not exist")
	}
	work, managed, err := g.svc.WorkForSession(ctx, review.SessionID)
	if err != nil || !managed {
		return err
	}
	if err := g.svc.ValidateResume(ctx, review.SessionID); err != nil {
		return err
	}
	role, found := work.Policy.Roles["reviewer"]
	if !found {
		return errors.New("reviewer role is not in the pinned policy")
	}
	return g.validator.ValidateInstructorAgentConfig(ctx, work.ProjectID, role, string(review.Harness), model, effort)
}

// A requested next-turn choice and a provider-reported configuration are
// separate facts. Neither attests what reasoning an individual inference used.
func (g instructorGovernance) ObserveChatAgentConfig(ctx context.Context, id domain.SessionID, model, effort, stage string) error {
	_, found, err := g.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	var work domain.InstructorWork
	var managed bool
	roleName := "reviewer"
	var harness string
	launch := domain.InstructorLaunch{Status: stage}
	if found {
		work, managed, err = g.svc.WorkForSession(ctx, id)
		if err != nil || !managed {
			return err
		}
		current, role, instance, roleErr := g.managedSessionRole(ctx, work, id)
		if roleErr != nil {
			return roleErr
		}
		roleName, harness = role, string(current.Harness)
		launch.SessionID = id
		launch.SessionInstanceID = instance
	} else {
		reader, ok := g.store.(interface {
			GetReviewByID(context.Context, string) (domain.Review, bool, error)
		})
		if !ok {
			return errors.New("reviewer ownership lookup is unavailable")
		}
		review, exists, lookupErr := reader.GetReviewByID(ctx, string(id))
		if lookupErr != nil {
			return lookupErr
		}
		if !exists {
			return errors.New("chat owner does not exist")
		}
		work, managed, err = g.svc.WorkForSession(ctx, review.SessionID)
		harness, launch.ReviewID = string(review.Harness), review.ID
	}
	if err != nil || !managed {
		return err
	}
	launch.Role = roleName
	launch.Requested = domain.InstructorAgentSelection{Harness: harness, Model: model, Effort: effort}
	if stage == "native-config-reported" {
		selection := launch.Requested
		launch.Confirmed = &selection
	}
	return g.svc.RecordLaunch(ctx, work.ID, launch)
}

func (g instructorGovernance) ValidateReviewerLaunch(ctx context.Context, spec reviewcore.LaunchSpec) error {
	work, managed, err := g.svc.WorkForSession(ctx, spec.WorkerID)
	if err != nil {
		return err
	}
	if !managed {
		if spec.ScopeKey != "" {
			return fmt.Errorf("%w: governed reviewer scope no longer has an admitted worker instance", instructorsvc.ErrConflict)
		}
		return nil
	}
	if err := g.svc.ValidateResume(ctx, spec.WorkerID); err != nil {
		return err
	}
	if spec.ScopeKey != instructorReviewScope(work) {
		return fmt.Errorf("%w: review phase scope changed", instructorsvc.ErrConflict)
	}
	role, found := work.Policy.Roles["reviewer"]
	if !found {
		return errors.New("reviewer role is not in the pinned policy")
	}
	return g.validator.ValidateInstructorAgentConfig(ctx, work.ProjectID, role, string(spec.Harness), spec.AgentConfig.Model, spec.AgentConfig.Effort)
}

func (g instructorGovernance) ValidateReviewerReuse(ctx context.Context, spec reviewcore.LaunchSpec) error {
	work, managed, err := g.svc.WorkForSession(ctx, spec.WorkerID)
	if err != nil {
		return err
	}
	if !managed {
		if spec.ScopeKey != "" {
			return fmt.Errorf("%w: governed reviewer scope no longer has an admitted worker instance", instructorsvc.ErrConflict)
		}
		return nil
	}
	requested := domain.InstructorAgentSelection{Harness: string(spec.Harness), Model: spec.AgentConfig.Model, Effort: spec.AgentConfig.Effort}
	for index := len(work.Launches) - 1; index >= 0; index-- {
		launch := work.Launches[index]
		if launch.ReviewID != spec.ReviewSessionID || launch.Status != "launched" {
			continue
		}
		if launch.Sent == nil || *launch.Sent != requested {
			return errors.New("existing reviewer uses a different native configuration; restart the reviewer")
		}
		return g.ValidateReviewerLaunch(ctx, spec)
	}
	return errors.New("existing reviewer has no successful managed configuration; restart the reviewer")
}

func (g instructorGovernance) ReviewerRunScope(ctx context.Context, id domain.SessionID) (string, error) {
	work, managed, err := g.svc.WorkForSession(ctx, id)
	if err != nil || !managed {
		return "", err
	}
	return instructorReviewScope(work), nil
}

func instructorReviewScope(work domain.InstructorWork) string {
	if work.Phase == "delta" {
		return fmt.Sprintf("%s/delta/%d", work.ID, work.DeltaPhaseSequence)
	}
	// A managed initial pass must not reuse an unassigned worker's historical
	// approval. Keep retries idempotent within the same durable work phase.
	return fmt.Sprintf("%s/%s/%d", work.ID, work.Phase, work.PhaseSequence)
}

func (g instructorGovernance) RecordReviewerLaunch(ctx context.Context, spec reviewcore.LaunchSpec) error {
	work, managed, err := g.svc.WorkForSession(ctx, spec.WorkerID)
	if err != nil {
		return err
	}
	if !managed {
		if spec.ScopeKey != "" {
			return fmt.Errorf("%w: governed reviewer scope no longer has an admitted worker instance", instructorsvc.ErrConflict)
		}
		return nil
	}
	if spec.ScopeKey != instructorReviewScope(work) {
		return fmt.Errorf("%w: review phase scope changed before launch confirmation", instructorsvc.ErrConflict)
	}
	id := spec.ReviewSessionID
	if id == "" {
		id = spec.RunID
	}
	selection := domain.InstructorAgentSelection{Harness: string(spec.Harness), Model: spec.AgentConfig.Model, Effort: spec.AgentConfig.Effort}
	return g.svc.RecordLaunch(ctx, work.ID, domain.InstructorLaunch{ReviewID: id, ReviewRunID: spec.RunID, RuntimeGeneration: spec.LaunchID, SHA: spec.TargetSHA, Role: "reviewer", Requested: selection, Sent: &selection, Status: "launched"})
}

func newInstructorRuntime(store instructorRuntimeStore, manager *sessionmanager.Manager, verifier instructorsvc.EvidenceVerifier) (*instructorsvc.Service, instructorGovernance, instructorInventory) {
	delivery := &instructorDelivery{store: store, sessions: manager}
	service := instructorsvc.New(instructorsvc.Deps{Store: store, Delivery: delivery, Workspace: instructorWorkspace{store: store, sessions: manager}, Verifier: verifier, RoleValidator: manager, PolicyPath: os.Getenv("AO_INSTRUCTOR_POLICY"), Publication: hostPublicationBoundary{}})
	delivery.binder = service
	inventory := instructorInventory{store: store}
	inventory.cache = newInstructorInventoryCache(inventory.collectInventory, store)
	return service, instructorGovernance{svc: service, store: store, validator: manager}, inventory
}

// A bounded loop operates only when explicitly enabled. Cancellation stops it
// before the durable store closes; Tick owns retry, progress and stall facts.
func startInstructor(ctx context.Context, svc *instructorsvc.Service, log *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if err := svc.Tick(ctx); err != nil && ctx.Err() == nil {
				log.Warn("Instructor tick failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

func (d instructorDelivery) Acknowledge(ctx context.Context, dispatch domain.InstructorDispatch) error {
	if resolver, ok := d.store.(interface {
		ResolveInstructorSemanticDelivery(context.Context, domain.InstructorDispatch, bool) error
	}); ok {
		return resolver.ResolveInstructorSemanticDelivery(ctx, dispatch, true)
	}
	ledger, ok := d.store.(ports.SemanticDeliveryLedger)
	if !ok {
		return errors.New("native delivery ledger is unavailable")
	}
	return ledger.ResolveSemanticDelivery(ctx, dispatch.TargetSessionID, dispatch.ID, true)
}
func (d instructorDelivery) Reconcile(ctx context.Context, dispatch domain.InstructorDispatch, accepted bool, _ string) error {
	if leaser, ok := d.sessions.(interface {
		AcquireSessionInput(domain.SessionID) (func(), bool)
	}); ok {
		release, admitted := leaser.AcquireSessionInput(dispatch.TargetSessionID)
		if !admitted {
			return fmt.Errorf("%w: target runtime has an exclusive operation", instructorsvc.ErrDeliveryUncertain)
		}
		defer release()
	}
	if reader, ok := d.store.(interface {
		AdmitInstructorReport(context.Context, string, domain.SessionID, string) (domain.InstructorReportAdmission, bool, error)
	}); ok {
		a, found, err := reader.AdmitInstructorReport(ctx, dispatch.WorkID, dispatch.TargetSessionID, dispatch.ID)
		if err != nil {
			return err
		}
		if !found || a.InstanceID != dispatch.TargetSessionInstanceID {
			return domain.ErrInstructorProducerChanged
		}
	}
	ledger, ok := d.store.(ports.SemanticDeliveryLedger)
	if !ok {
		return errors.New("native delivery ledger is unavailable")
	}
	if !accepted {
		reconciler, ok := d.sessions.(interface {
			ReconcileSemanticDelivery(context.Context, domain.SessionID, string, bool) error
		})
		if !ok {
			return fmt.Errorf("%w: native queue reconciliation is unavailable", instructorsvc.ErrDeliveryUncertain)
		}
		if err := reconciler.ReconcileSemanticDelivery(ctx, dispatch.TargetSessionID, dispatch.ID, accepted); err != nil {
			return err
		}
	}
	if resolver, ok := d.store.(interface {
		ResolveInstructorSemanticDelivery(context.Context, domain.InstructorDispatch, bool) error
	}); ok {
		return resolver.ResolveInstructorSemanticDelivery(ctx, dispatch, accepted)
	}
	return ledger.ResolveSemanticDelivery(ctx, dispatch.TargetSessionID, dispatch.ID, accepted)
}

func (d instructorDelivery) Cancel(ctx context.Context, dispatch domain.InstructorDispatch) error {
	if dispatch.TargetSessionID == "" {
		return nil
	}
	target, found, err := d.store.GetSession(ctx, dispatch.TargetSessionID)
	if err != nil {
		return err
	}
	if !found || domain.NormalizeSessionMode(target.Mode) != domain.SessionModeChat {
		return fmt.Errorf("%w: native pane submission cannot be proved cancelled", instructorsvc.ErrDeliveryUncertain)
	}
	return d.Reconcile(ctx, dispatch, false, "")
}

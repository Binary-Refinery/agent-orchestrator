// Package instructor owns durable project-wide work and operator decisions.
// It delegates execution to project orchestrators, never directly to workers.
package instructor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrConflict indicates that a frozen operator snapshot no longer matches live work.
var ErrConflict = errors.New("instructor state changed; obtain a new snapshot")

// ErrNotFound indicates a missing work, snapshot or persisted command.
var ErrNotFound = errors.New("instructor work or snapshot not found")

// ErrValidation indicates an invalid policy, report or operator request.
var ErrValidation = errors.New("invalid instructor request")

// ErrDeliveryUncertain fences a native submission whose outcome cannot be established.
var ErrDeliveryUncertain = errors.New("native submission outcome is uncertain; reconcile before continuing")

// ErrDeliveryQueued distinguishes a durable native queue from an accepted message.
var ErrDeliveryQueued = errors.New("native submission is durably queued and not yet accepted")

// ErrDeliveryRejected confirms cancellation before native provider admission.
var ErrDeliveryRejected = errors.New("native queued submission was definitively cancelled before provider admission")

// ErrEvidencePending holds execution until current source facts have been inspected.
var ErrEvidencePending = errors.New("current persisted source facts need bounded inspection before execution")

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrValidation, message) }

// Store persists durable business facts, immutable decisions and the frozen dispatch queue.
type Store interface {
	CreateInstructorWork(context.Context, domain.InstructorWork, domain.InstructorEvent) (domain.InstructorWork, error)
	GetInstructorWork(context.Context, string) (domain.InstructorWork, bool, error)
	ListInstructorWorks(context.Context) ([]domain.InstructorWork, error)
	InstructorWorkForSession(context.Context, domain.SessionID) (domain.InstructorWork, bool, error)
	SaveInstructorWork(context.Context, domain.InstructorWork, int64, domain.InstructorEvent) (bool, error)
	PutInstructorSnapshot(context.Context, domain.InstructorSnapshot) error
	GetInstructorSnapshot(context.Context, string) (domain.InstructorSnapshot, bool, error)
	ListInstructorEvents(context.Context, string) ([]domain.InstructorEvent, error)
	EnqueueInstructorDispatches(context.Context, []domain.InstructorDispatch) error
	CommitInstructorBatch(context.Context, []domain.InstructorDispatch, []domain.InstructorWork, []domain.InstructorEvent, []string) error
	ListInstructorDispatches(context.Context, string, string) ([]domain.InstructorDispatch, error)
	ClaimInstructorDispatch(context.Context, time.Time) (domain.InstructorDispatch, bool, error)
	SetInstructorDispatch(context.Context, string, string, string, time.Time) error
	BindInstructorDispatchTarget(context.Context, string, domain.SessionID) error
	ReconcileInstructorDispatch(context.Context, string, string, string, time.Time) error
	QueueInstructorDispatch(context.Context, domain.InstructorDispatch, domain.InstructorWork, int64, domain.InstructorEvent, time.Time) error
	DeferInstructorDispatch(context.Context, string, string, time.Time) error
}

// Delivery sends semantic directives to the selected project orchestrator.
type Delivery interface {
	Deliver(context.Context, domain.InstructorDispatch) error
}

// DeliveryAcknowledger records an actual receipt from the immutable target session.
type DeliveryAcknowledger interface {
	Acknowledge(context.Context, domain.InstructorDispatch) error
}

// DeliveryReconciler resolves one uncertain native attempt from operator observation.
type DeliveryReconciler interface {
	Reconcile(context.Context, domain.InstructorDispatch, bool, string) error
}

// DeliveryCanceller must fence the actual native queue before returning nil.
// Updating the Instructor outbox or a deduplication ledger alone is insufficient.
type DeliveryCanceller interface {
	Cancel(context.Context, domain.InstructorDispatch) error
}

// Workspace resolves the actual attached producer worktree used for native checks.
type Workspace interface {
	WorkspaceForSession(context.Context, domain.SessionID) (string, error)
}

// EvidenceVerifier resolves evidence claims against trusted source facts.
type EvidenceVerifier interface {
	Verify(context.Context, domain.InstructorWork, domain.InstructorEvidence) error
}

// EvidenceAssessor supplies outcome and finding facts from trusted persistence.
// A client cannot attest these fields through a report or verify request.
type EvidenceAssessor interface {
	Assess(context.Context, domain.InstructorWork, domain.InstructorEvidence) (domain.InstructorEvidence, error)
}

// Deps supplies persistence, native capability validation and trusted execution adapters.
type Deps struct {
	Store         Store
	Delivery      Delivery
	Workspace     Workspace
	Verifier      EvidenceVerifier
	Planner       Planner
	RoleValidator RoleValidator
	PolicyPath    string
	Clock         func() time.Time
	NewID         func() string
}

// Service owns the global Instructor control plane above project orchestrators.
type Service struct {
	deps                Deps
	mu                  sync.Mutex
	policy              domain.InstructorPolicy
	verificationMu      sync.Mutex
	verificationRunning map[string]bool
	verificationSlots   chan struct{}
	verificationStopped bool
	verificationCancels map[string]context.CancelFunc
	verificationWG      sync.WaitGroup
	planningRunning     bool
}

// New constructs an Instructor service with bounded background inspection.
func New(deps Deps) *Service {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	return &Service{deps: deps, policy: DefaultPolicy(), verificationRunning: map[string]bool{}, verificationSlots: make(chan struct{}, 2), verificationCancels: map[string]context.CancelFunc{}}
}

// RegisterInput identifies one durable work and its immutable intake origin.
type RegisterInput struct {
	ProjectID domain.ProjectID        `json:"projectId"`
	Name      string                  `json:"name"`
	Goal      string                  `json:"goal"`
	Source    string                  `json:"source"`
	SourceRef string                  `json:"sourceRef"`
	IssueID   domain.IssueID          `json:"issueId,omitempty"`
	Branch    string                  `json:"branch,omitempty"`
	Policy    domain.InstructorPolicy `json:"policy,omitempty"`
}

// ReportInput carries an attached producer claim or an exact bound-target receipt.
type ReportInput struct {
	WorkID               string                      `json:"workId"`
	SessionID            domain.SessionID            `json:"sessionId"`
	Kind                 string                      `json:"kind"`
	Summary              string                      `json:"summary"`
	Next                 string                      `json:"next"`
	SHA                  string                      `json:"sha,omitempty"`
	Branch               string                      `json:"branch,omitempty"`
	Phase                string                      `json:"phase,omitempty"`
	WaitCondition        string                      `json:"waitCondition,omitempty"`
	WaitUntil            *time.Time                  `json:"waitUntil,omitempty"`
	Gate                 *domain.InstructorGate      `json:"gate,omitempty"`
	Evidence             []domain.InstructorEvidence `json:"evidence"`
	OpenCriticalFindings *int                        `json:"openCriticalFindings,omitempty"`
	DispatchID           string                      `json:"dispatchId,omitempty"`
}

// CommandInput binds an extensible operator command to a frozen work snapshot.
type CommandInput struct {
	Command        string         `json:"command"`
	SnapshotID     string         `json:"snapshotId,omitempty"`
	WorkIDs        []string       `json:"workIds"`
	Approval       string         `json:"approval,omitempty"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
	New            *RegisterInput `json:"new,omitempty"`
}

func (s *Service) now() time.Time { return s.deps.Clock().UTC() }
func (s *Service) event(w domain.InstructorWork, kind, detail string) domain.InstructorEvent {
	return domain.InstructorEvent{ID: s.deps.NewID(), WorkID: w.ID, Kind: kind, Detail: detail, At: s.now()}
}

// Register creates a durable work or returns the existing stable intake identity.
func (s *Service) Register(ctx context.Context, in RegisterInput) (domain.InstructorWork, error) {
	if s.deps.Store == nil {
		return domain.InstructorWork{}, invalid("instructor storage unavailable")
	}
	if in.ProjectID == "" || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Goal) == "" {
		return domain.InstructorWork{}, invalid("projectId, name and goal are required")
	}
	if len(in.Name) > 120 || len(in.Goal) > 8192 {
		return domain.InstructorWork{}, invalid("work name or goal is too long")
	}
	if projects, ok := s.deps.Store.(interface {
		GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	}); ok {
		_, found, err := projects.GetProject(ctx, string(in.ProjectID))
		if err != nil {
			return domain.InstructorWork{}, err
		}
		if !found {
			return domain.InstructorWork{}, fmt.Errorf("%w: project not found", ErrValidation)
		}
	}
	p := in.Policy
	if p.Schema == 0 {
		var err error
		p, err = s.GetPolicy(ctx)
		if err != nil {
			return domain.InstructorWork{}, err
		}
	}
	if err := ValidatePolicy(p); err != nil {
		return domain.InstructorWork{}, err
	}
	if err := ValidateLockedPolicy(p); err != nil {
		return domain.InstructorWork{}, err
	}
	p.Version = PolicyVersion(p)
	if in.Source == "" {
		in.Source = "manual"
	}
	if in.SourceRef == "" {
		in.SourceRef = s.deps.NewID()
	}
	if origins, ok := s.deps.Store.(interface {
		InstructorWorkByOrigin(context.Context, domain.ProjectID, string, string) (domain.InstructorWork, bool, error)
	}); ok {
		existing, found, err := origins.InstructorWorkByOrigin(ctx, in.ProjectID, in.Source, in.SourceRef)
		if err != nil {
			return domain.InstructorWork{}, err
		}
		if found {
			return existing, nil
		}
	}
	if err := s.uniqueWorkName(ctx, in.Name); err != nil {
		return domain.InstructorWork{}, err
	}
	now := s.now()
	w := domain.InstructorWork{ID: "work-" + s.deps.NewID(), ProjectID: in.ProjectID, Name: strings.TrimSpace(in.Name), Goal: strings.TrimSpace(in.Goal), Source: in.Source, SourceRef: in.SourceRef, IssueID: in.IssueID, Branch: in.Branch, State: "ready", Phase: p.InitialPhase, Summary: "Auftrag registriert", Policy: p, PolicyVersion: p.Version, SessionIDs: []domain.SessionID{}, Evidence: []domain.InstructorEvidence{}, Revision: 1, CreatedAt: now, UpdatedAt: now, ProgressAt: now, Actions: []string{}}
	w.PhaseStartedAt = now
	w.PhaseSequence = 1
	if w.Phase == "fix" {
		w.FixPhaseSequence = 1
	}
	if w.Phase == "delta" {
		w.DeltaPhaseSequence = 1
	}
	created, err := s.deps.Store.CreateInstructorWork(ctx, w, s.event(w, "registered", in.Source+":"+in.SourceRef))
	if err != nil {
		if nameErr := s.uniqueWorkName(ctx, in.Name); nameErr != nil {
			return w, nameErr
		}
	}
	return created, err
}

func (s *Service) uniqueWorkName(ctx context.Context, name string) error {
	works, err := s.deps.Store.ListInstructorWorks(ctx)
	if err != nil {
		return err
	}
	for _, w := range works {
		if strings.EqualFold(strings.TrimSpace(w.Name), strings.TrimSpace(name)) {
			return invalid("work name is already in use; choose a distinct short name: " + name)
		}
	}
	return nil
}
func (s *Service) get(ctx context.Context, id string) (domain.InstructorWork, error) {
	w, ok, err := s.deps.Store.GetInstructorWork(ctx, id)
	if err != nil {
		return w, err
	}
	if !ok {
		return w, ErrNotFound
	}
	return w, nil
}
func (s *Service) save(ctx context.Context, w domain.InstructorWork, expected int64, kind, detail string) (domain.InstructorWork, error) {
	w.Revision = expected + 1
	w.UpdatedAt = s.now()
	w.Actions = nil
	ok, err := s.deps.Store.SaveInstructorWork(ctx, w, expected, s.event(w, kind, detail))
	if err != nil {
		return w, err
	}
	if !ok {
		return w, ErrConflict
	}
	return w, nil
}
func containsSession(w domain.InstructorWork, id domain.SessionID) bool {
	for _, v := range w.SessionIDs {
		if v == id {
			return true
		}
	}
	return false
}

// AttachSession associates an existing same-project session with one durable work.
func (s *Service) AttachSession(ctx context.Context, workID string, id domain.SessionID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.get(ctx, workID)
	if err != nil {
		return err
	}
	if id == "" {
		return invalid("session id required")
	}
	if containsSession(w, id) {
		return nil
	}
	if sessions, ok := s.deps.Store.(interface {
		GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	}); ok {
		rec, found, e := sessions.GetSession(ctx, id)
		if e != nil {
			return e
		}
		if !found || rec.ProjectID != w.ProjectID {
			return invalid("session is missing or belongs to another project")
		}
	}
	w.SessionIDs = append(w.SessionIDs, id)
	_, err = s.save(ctx, w, w.Revision, "session_attached", string(id))
	return err
}

// WorkForSession resolves the durable work associated with a worker session.
func (s *Service) WorkForSession(ctx context.Context, id domain.SessionID) (domain.InstructorWork, bool, error) {
	return s.deps.Store.InstructorWorkForSession(ctx, id)
}

// BindDispatchTarget permanently binds a directive to its actual orchestrator session.
func (s *Service) BindDispatchTarget(ctx context.Context, id string, target domain.SessionID) error {
	return s.deps.Store.BindInstructorDispatchTarget(ctx, id, target)
}

// Details returns business facts, immutable events and durable delivery history.
func (s *Service) Details(ctx context.Context, id string) (domain.InstructorDetails, error) {
	w, err := s.get(ctx, id)
	if err != nil {
		return domain.InstructorDetails{}, err
	}
	s.actions(&w)
	if err := s.DecorateVerification(ctx, &w); err != nil {
		return domain.InstructorDetails{}, err
	}
	if err := s.DecorateHeartbeat(ctx, &w); err != nil {
		return domain.InstructorDetails{}, err
	}
	events, err := s.deps.Store.ListInstructorEvents(ctx, id)
	if err != nil {
		return domain.InstructorDetails{}, err
	}
	dispatches, err := s.deps.Store.ListInstructorDispatches(ctx, "", id)
	return domain.InstructorDetails{Work: w, Events: events, Dispatches: dispatches}, err
}

// Snapshot persists an immutable portfolio view, optionally restricted to changed works.
func (s *Service) Snapshot(ctx context.Context, baseID string) (domain.InstructorSnapshot, error) {
	works, err := s.deps.Store.ListInstructorWorks(ctx)
	if err != nil {
		return domain.InstructorSnapshot{}, err
	}
	metrics, err := s.snapshotMetrics(ctx, works)
	if err != nil {
		return domain.InstructorSnapshot{}, err
	}
	if baseID != "" {
		base, ok, e := s.deps.Store.GetInstructorSnapshot(ctx, baseID)
		if e != nil {
			return domain.InstructorSnapshot{}, e
		}
		if !ok {
			return domain.InstructorSnapshot{}, ErrNotFound
		}
		revisions := map[string]int64{}
		for _, w := range base.Works {
			revisions[w.ID] = w.Revision
		}
		delta := make([]domain.InstructorWork, 0)
		for _, w := range works {
			if revisions[w.ID] != w.Revision {
				delta = append(delta, w)
			}
		}
		works = delta
	}
	for i := range works {
		s.actions(&works[i])
		if err := s.DecorateVerification(ctx, &works[i]); err != nil {
			return domain.InstructorSnapshot{}, err
		}
		if err := s.DecorateHeartbeat(ctx, &works[i]); err != nil {
			return domain.InstructorSnapshot{}, err
		}
	}
	commands, conflicts := s.registryForWorks(works)
	result := domain.InstructorSnapshot{ID: "snapshot-" + s.deps.NewID(), BaseID: baseID, CreatedAt: s.now(), Works: works, Commands: commands, Metrics: metrics, CommandConflicts: conflicts}
	return result, s.deps.Store.PutInstructorSnapshot(ctx, result)
}
func (s *Service) actions(w *domain.InstructorWork) {
	w.Actions = []string{"details"}
	if w.State == "done" || w.State == "cancelled" {
		return
	}
	w.Actions = append(w.Actions, "pruefen", "stopp")
	if validateWorkExceptions(*w, s.now()) != nil {
		return
	}
	if w.Gate != nil && w.State == "waiting_human" && w.Gate.Kind != "delivery_uncertain" && w.Gate.Kind != "publication_violation" {
		if w.Gate.Kind != "publication" || publicationReadiness(*w) == nil {
			w.Actions = append(w.Actions, "go")
		}
	}
	if w.State == "ready" || w.State == "running" || w.State == "parked" && (w.Gate == nil || w.Gate.Kind != "delivery_uncertain" && w.Gate.Kind != "publication_violation") {
		w.Actions = append(w.Actions, "weiter")
	}
}

// Commands describes built-in operator actions and their actual authority constraints.
func (s *Service) Commands() []domain.InstructorCommandSpec {
	commands := []domain.InstructorCommandSpec{
		{Name: "Status", Aliases: []string{"status", "status alle", "status all"}, Action: "status", Description: "Vollständiger eingefrorener Arbeitsstand"},
		{Name: "Status Delta", Aliases: []string{"delta"}, Action: "delta", Description: "Änderungen seit einem Snapshot"},
		{Name: "Tore", Aliases: []string{"tore"}, Action: "tore", Description: "Offene menschliche Entscheidungen"},
		{Name: "Weiter", Aliases: []string{"weiter", "weiter alle"}, Action: "weiter", Description: "Autorisierte Arbeit fortsetzen", Mutates: true, SnapshotRequired: true},
		{Name: "Go", Aliases: []string{"go", "go all"}, Action: "go", Description: "Angezeigtes Gate freigeben", Mutates: true, SnapshotRequired: true},
		{Name: "Details", Aliases: []string{"details"}, Action: "details", Description: "Belege und Ereignisse"},
		{Name: "Prüfen", Aliases: []string{"pruefen", "prüfen"}, Action: "pruefen", Description: "Projekt-Orchestrator zur Prüfung beauftragen", Mutates: true, SnapshotRequired: true},
		{Name: "Stopp", Aliases: []string{"stopp", "stop"}, Action: "stopp", Description: "Weitere geplante Schritte anhalten", Mutates: true, SnapshotRequired: true},
		{Name: "Neu", Aliases: []string{"neu"}, Action: "neu", Description: "Neuen Auftrag registrieren", Mutates: true},
	}
	for i := range commands {
		commands[i] = commandContract(commands[i])
	}
	return commands
}
func resolveCommand(text string, commands []domain.InstructorCommandSpec) (domain.InstructorCommandSpec, bool) {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, c := range commands {
		if strings.EqualFold(c.Name, text) || c.Action == text {
			return c, true
		}
		for _, a := range c.Aliases {
			if strings.EqualFold(a, text) {
				return c, true
			}
		}
	}
	return domain.InstructorCommandSpec{}, false
}

// Command validates a frozen cohort and atomically records its authorized directives.
func (s *Service) Command(ctx context.Context, in CommandInput) (domain.InstructorCommandResult, error) {
	registry := s.Commands()
	var snap domain.InstructorSnapshot
	if in.SnapshotID != "" {
		v, ok, err := s.deps.Store.GetInstructorSnapshot(ctx, in.SnapshotID)
		if err != nil {
			return domain.InstructorCommandResult{}, err
		}
		if !ok {
			return domain.InstructorCommandResult{}, ErrNotFound
		}
		snap = v
		registry = snap.Commands
	}
	conflicts := snap.CommandConflicts
	if len(in.WorkIDs) > 0 && snap.ID != "" {
		resolved, err := resolveWorkReferences(in.WorkIDs, snap.Works)
		if err != nil {
			return domain.InstructorCommandResult{}, err
		}
		in.WorkIDs = resolved
		selectedWorks := []domain.InstructorWork{}
		for _, w := range snap.Works {
			for _, id := range resolved {
				if w.ID == id {
					selectedWorks = append(selectedWorks, w)
				}
			}
		}
		registry, conflicts = s.registryForWorks(selectedWorks)
	}
	if conflictingCommand(in.Command, conflicts) {
		return domain.InstructorCommandResult{}, invalid("command has different effects across project policies; select one work or choose a built-in command: " + strings.Join(conflicts, ", "))
	}
	spec, selector, err := resolveCommandLine(in.Command, registry)
	if err != nil {
		return domain.InstructorCommandResult{}, err
	}
	if selector != "" && !strings.EqualFold(selector, "all") && !strings.EqualFold(selector, "alle") {
		if len(in.WorkIDs) != 0 {
			return domain.InstructorCommandResult{}, invalid("specify work either in command text or workIds")
		}
		in.WorkIDs = []string{selector}
	}
	if len(in.WorkIDs) > 0 {
		works := snap.Works
		if snap.ID == "" {
			works, err = s.deps.Store.ListInstructorWorks(ctx)
			if err != nil {
				return domain.InstructorCommandResult{}, err
			}
		}
		in.WorkIDs, err = resolveWorkReferences(in.WorkIDs, works)
		if err != nil {
			return domain.InstructorCommandResult{}, err
		}
	}
	if spec.SnapshotRequired && snap.ID == "" {
		return domain.InstructorCommandResult{}, invalid("snapshotId is required")
	}
	if !spec.Mutates {
		if spec.Action == "details" {
			if len(in.WorkIDs) != 1 {
				return domain.InstructorCommandResult{}, invalid("Details requires one unambiguous work")
			}
			detail, err := s.Details(ctx, in.WorkIDs[0])
			return domain.InstructorCommandResult{Details: []domain.InstructorDetails{detail}, Dispatches: []domain.InstructorDispatch{}, Skipped: []string{}}, err
		}
		v, err := s.Snapshot(ctx, func() string {
			if spec.Action == "delta" {
				return in.SnapshotID
			}
			return ""
		}())
		if spec.Action == "tore" && err == nil {
			out := []domain.InstructorWork{}
			for _, w := range v.Works {
				if w.Gate != nil {
					out = append(out, w)
				}
			}
			v.ID = "snapshot-" + s.deps.NewID()
			v.Works = out
			err = s.deps.Store.PutInstructorSnapshot(ctx, v)
		}
		return domain.InstructorCommandResult{Snapshot: &v, Dispatches: []domain.InstructorDispatch{}, Skipped: []string{}}, err
	}
	if spec.Action == "neu" {
		if in.New == nil {
			return domain.InstructorCommandResult{}, invalid("new work is required")
		}
		_, err := s.Register(ctx, *in.New)
		if err != nil {
			return domain.InstructorCommandResult{}, err
		}
		v, err := s.Snapshot(ctx, "")
		return domain.InstructorCommandResult{Snapshot: &v, Dispatches: []domain.InstructorDispatch{}, Skipped: []string{}}, err
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return domain.InstructorCommandResult{}, invalid("idempotencyKey is required")
	}
	h := sha256.Sum256([]byte(in.IdempotencyKey))
	batchID := "batch-" + hex.EncodeToString(h[:])
	requestBytes, _ := json.Marshal(in)
	requestHash := sha256.Sum256(requestBytes)
	commandDigest := hex.EncodeToString(requestHash[:])
	existing, err := s.deps.Store.ListInstructorDispatches(ctx, batchID, "")
	if err != nil {
		return domain.InstructorCommandResult{}, err
	}
	if len(existing) > 0 {
		for _, d := range existing {
			if d.CommandDigest != commandDigest {
				return domain.InstructorCommandResult{}, invalid("idempotencyKey already used for a different command")
			}
		}
		return domain.InstructorCommandResult{BatchID: batchID, Dispatches: existing, Skipped: []string{}}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Recheck under the mutation lock: simultaneous retries can both observe the
	// earlier read before the first frozen batch is committed.
	existing, err = s.deps.Store.ListInstructorDispatches(ctx, batchID, "")
	if err != nil {
		return domain.InstructorCommandResult{}, err
	}
	if len(existing) > 0 {
		for _, d := range existing {
			if d.CommandDigest != commandDigest {
				return domain.InstructorCommandResult{}, invalid("idempotencyKey already used for a different command")
			}
		}
		return domain.InstructorCommandResult{BatchID: batchID, Dispatches: existing, Skipped: []string{}}, nil
	}
	selected := map[string]bool{}
	for _, id := range in.WorkIDs {
		selected[id] = true
	}
	all := len(in.WorkIDs) == 0
	dispatches := []domain.InstructorDispatch{}
	skipped := []string{}
	updates := []domain.InstructorWork{}
	events := []domain.InstructorEvent{}
	cancelWorks := []string{}
	for _, captured := range snap.Works {
		if !all && !selected[captured.ID] {
			continue
		}
		delete(selected, captured.ID)
		w, err := s.get(ctx, captured.ID)
		if err != nil {
			return domain.InstructorCommandResult{}, err
		}
		if w.Revision != captured.Revision {
			return domain.InstructorCommandResult{}, ErrConflict
		}
		action := spec.Action
		if action == "go" && all && w.Gate == nil {
			action = "weiter"
		}
		s.actions(&w)
		allowed := false
		for _, a := range w.Actions {
			if a == action {
				allowed = true
			}
		}
		if !allowed {
			skipped = append(skipped, w.ID+": action unavailable in "+w.State)
			continue
		}
		if action == "go" && w.Gate != nil && w.Gate.Kind == "publication" && in.Approval != w.Gate.ExactApproval {
			if all {
				skipped = append(skipped, w.ID+": exact publication approval required")
				continue
			}
			return domain.InstructorCommandResult{}, fmt.Errorf("%w: exact human approval required: %s", ErrValidation, w.Gate.ExactApproval)
		}
		if action == "go" && w.Gate != nil && w.Gate.Kind == "publication" && !HasPublicationGrant(w) {
			if observer, ok := s.deps.Verifier.(PublicationObserver); ok {
				ref, observed, err := observer.ObservedPublication(ctx, w)
				if err != nil {
					return domain.InstructorCommandResult{}, err
				}
				if observed {
					return domain.InstructorCommandResult{}, invalid("publication already observed without a matching grant; a later Go cannot authorize the earlier effect: " + ref)
				}
			}
		}
		if action == "go" && w.Gate != nil && (w.Gate.Kind == "publication" || w.Gate.TransitionTo != "") {
			if err := s.CurrentEvidence(ctx, w); err != nil {
				return domain.InstructorCommandResult{}, err
			}
		}
		message := fmt.Sprintf("Instructor %s for work %s (snapshot %s, policy %s). Goal: %s. Continue only within the pinned policy; report acceptance and progress separately.", action, w.ID, snap.ID, w.PolicyVersion, w.Goal)
		if action == "go" && w.Gate != nil {
			message += " Human approved gate " + w.Gate.ID + ". " + in.Approval
		}
		if action == "go" && w.Gate != nil {
			gate := *w.Gate
			if gate.Kind == "publication" {
				w.PublicationGrants = append(w.PublicationGrants, domain.InstructorPublicationGrant{
					GateID: gate.ID, WorkID: w.ID, PolicyVersion: w.PolicyVersion,
					SHA: w.SHA, Branch: w.Branch, SnapshotID: snap.ID,
					WorkRevision: captured.Revision, Approval: in.Approval, ApprovedAt: s.now(),
				})
			}
			if gate.TransitionTo != "" {
				w.ApprovedTransitions = append(w.ApprovedTransitions, w.Phase+"->"+gate.TransitionTo)
				w.Evidence = append(w.Evidence, domain.InstructorEvidence{ID: s.deps.NewID(), WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: "triage", Producer: "operator", Role: "instructor", Ref: "gate:" + gate.ID, Verified: true, CreatedAt: s.now()})
			}
			w.Gate = nil
			w.State = "ready"
			w.Revision++
			w.UpdatedAt = s.now()
			updates = append(updates, w)
			events = append(events, s.event(w, "human_approved", gate.ID+" snapshot="+snap.ID))
		}
		if action == "stopp" {
			w.State = "parked"
			w.WaitCondition = "Operator-Stopp; Weiter nach erneuter Entscheidung"
			w.Revision++
			w.UpdatedAt = s.now()
			updates = append(updates, w)
			events = append(events, s.event(w, "operator_stopped", "snapshot="+snap.ID))
			cancelWorks = append(cancelWorks, w.ID)
		}
		if action == "weiter" && w.State == "parked" {
			w.State = "ready"
			if w.Gate != nil {
				w.State = "waiting_human"
			}
			w.WaitCondition = ""
			w.WaitUntil = nil
			w.NudgedAt = nil
			w.ProgressAt = s.now()
			w.Revision++
			w.UpdatedAt = s.now()
			updates = append(updates, w)
			events = append(events, s.event(w, "operator_resumed", "snapshot="+snap.ID))
		}
		now := s.now()
		dispatches = append(dispatches, domain.InstructorDispatch{ID: batchID + fmt.Sprintf("-%d", len(dispatches)), BatchID: batchID, WorkID: w.ID, ProjectID: w.ProjectID, SnapshotID: snap.ID, WorkRevision: w.Revision, Position: len(dispatches), Action: action, Message: message, CommandDigest: commandDigest, State: "pending", CreatedAt: now, UpdatedAt: now})
	}
	if len(selected) > 0 {
		return domain.InstructorCommandResult{}, invalid("requested work is absent from this snapshot")
	}
	if len(dispatches) == 0 {
		return domain.InstructorCommandResult{Skipped: skipped, Dispatches: dispatches}, invalid("no eligible work")
	}
	// A Stop must cancel already queued native messages as well as the local
	// outbox. Validate the whole frozen cohort before crossing that boundary.
	for i := range updates {
		if updates[i].State != "parked" || spec.Action != "stopp" {
			continue
		}
		if err := s.cancelNativePending(ctx, &updates[i], &events); err != nil {
			return domain.InstructorCommandResult{}, err
		}
	}
	if err := s.deps.Store.CommitInstructorBatch(ctx, dispatches, updates, events, cancelWorks); err != nil {
		return domain.InstructorCommandResult{}, err
	}
	return domain.InstructorCommandResult{BatchID: batchID, Dispatches: dispatches, Skipped: skipped}, nil
}

// Report accepts attributed progress while keeping client assertions unverified.
func (s *Service) Report(ctx context.Context, in ReportInput) (domain.InstructorWork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.get(ctx, in.WorkID)
	if err != nil {
		return w, err
	}
	boundProducer := containsSession(w, in.SessionID)
	var uncertainReceipt *domain.InstructorDispatch
	var relatedDispatches []domain.InstructorDispatch
	if in.DispatchID != "" {
		ds, e := s.deps.Store.ListInstructorDispatches(ctx, "", w.ID)
		if e != nil {
			return w, e
		}
		relatedDispatches = ds
		boundDispatch := false
		for _, d := range ds {
			isReconcileReceipt := uncertainDispatch(w, d.ID) && (in.Kind == "accepted" || in.Kind == "progress")
			queuedReceipt := d.State == "pending" && d.QueuedAt != nil && (in.Kind == "accepted" || in.Kind == "progress")
			uncertainAvailability := uncertainDispatch(w, d.ID) && in.Kind == "heartbeat"
			if d.ID == in.DispatchID && d.TargetSessionID == in.SessionID && (d.State != "pending" && d.State != "failed" && d.State != "cancelled" || isReconcileReceipt || queuedReceipt || uncertainAvailability) {
				boundDispatch = true
				if isReconcileReceipt {
					cloned := d
					uncertainReceipt = &cloned
				}
			}
		}
		if !boundDispatch {
			return w, invalid("dispatch report must come from its bound target session")
		}
		boundProducer = true
	}
	if !boundProducer {
		return w, invalid("report producer is not attached to work or target dispatch")
	}
	if uncertainReceipt != nil {
		if observer, ok := s.deps.Delivery.(DeliveryAcknowledger); ok {
			if err := observer.Acknowledge(ctx, *uncertainReceipt); err != nil {
				return w, err
			}
		}
		if err := s.deps.Store.ReconcileInstructorDispatch(ctx, uncertainReceipt.ID, in.Kind, "bound target receipt", s.now()); err != nil {
			return w, err
		}
		s.clearUncertain(&w, uncertainReceipt.ID, relatedDispatches)
		if w.State != "parked" {
			w.State = "running"
			if w.Gate != nil {
				w.State = "waiting_human"
			}
			w.WaitCondition = ""
		}
	}
	if w.State == "done" || w.State == "cancelled" {
		return w, invalid("work is terminal")
	}
	if w.Gate != nil && w.Gate.Kind == "delivery_uncertain" && in.Kind != "accepted" && in.Kind != "progress" && in.Kind != "checkpoint" && in.Kind != "evidence" && in.Kind != "heartbeat" {
		return w, invalid("uncertain native submission requires its bound receipt or explicit reconciliation")
	}
	if (w.State == "parked" || w.State == "waiting_human" || w.State == "blocked") &&
		(in.Phase != "" && in.Phase != w.Phase || in.SHA != "" && in.SHA != w.SHA || in.Branch != "" && in.Branch != w.Branch || in.Kind == "waiting" || in.Kind == "done") {
		return w, invalid("paused work requires an operator command before execution can continue")
	}
	if in.SHA != "" && !fullSHA(in.SHA) {
		return w, invalid("full immutable commit SHA required")
	}
	if in.OpenCriticalFindings != nil && *in.OpenCriticalFindings < 0 {
		return w, invalid("invalid finding count")
	}
	actorRole := ""
	for _, l := range w.Launches {
		if l.SessionID == in.SessionID {
			actorRole = l.Role
		}
	}
	if !containsSession(w, in.SessionID) {
		actorRole = "orchestrator"
	}
	if in.Kind == "heartbeat" {
		return s.heartbeat(ctx, w, in, actorRole)
	}
	if in.Phase != "" && in.Phase != w.Phase {
		if err := validateWorkExceptions(w, s.now()); err != nil {
			return w, err
		}
		if err := s.CurrentEvidence(ctx, w); err != nil {
			return w, err
		}
		if err := canAdvance(w, in.Phase, actorRole); err != nil {
			return w, err
		}
		if in.Phase == "fix" {
			if w.FixRounds >= w.Policy.MaxFixRounds {
				return w, invalid("fix round budget exhausted")
			}
			w.FixRounds++
		}
		w.Phase = in.Phase
		w.PhaseStartedAt = s.now()
		w.PhaseSequence++
		if in.Phase == "fix" {
			w.FixPhaseSequence = w.PhaseSequence
		}
		if in.Phase == "delta" {
			w.DeltaPhaseSequence = w.PhaseSequence
		}
		w.ApprovedTransitions = nil
	}
	if in.SHA != "" && w.SHA != in.SHA {
		w.SHA = in.SHA
	}
	if in.Branch != "" {
		w.Branch = in.Branch
	}
	for _, e := range in.Evidence {
		if e.WorkID != "" && e.WorkID != w.ID || e.PolicyVersion != "" && e.PolicyVersion != w.PolicyVersion || e.SHA != "" && e.SHA != w.SHA {
			return w, invalid("foreign evidence binding")
		}
		e.ID = s.deps.NewID()
		e.WorkID = w.ID
		e.PolicyVersion = w.PolicyVersion
		if e.Producer == "" {
			e.Producer = in.SessionID
		}
		e.SHA = w.SHA
		e.Verified = false
		e.Revoked = false
		e.RevocationReason = ""
		e.Outcome = ""
		e.CriticalFindings = 0
		e.CreatedAt = s.now()
		e.Phase = w.Phase
		e.PhaseSequence = w.PhaseSequence
		w.Evidence = append(w.Evidence, e)
	}
	if in.OpenCriticalFindings != nil && *in.OpenCriticalFindings > w.OpenCriticalFindings {
		w.OpenCriticalFindings = *in.OpenCriticalFindings
	}
	if in.Summary != "" {
		w.Summary = in.Summary
	}
	if in.Next != "" {
		w.Next = in.Next
	}
	if in.DispatchID != "" {
		if in.Kind == "accepted" || in.Kind == "progress" {
			if err := s.deps.Store.SetInstructorDispatch(ctx, in.DispatchID, in.Kind, "", s.now()); err != nil {
				return w, err
			}
		}
	}
	switch in.Kind {
	case "accepted": // Receipt is deliberately not progress.
		if in.DispatchID != "" && w.State == "waiting_external" && strings.HasPrefix(w.WaitCondition, "Native Nachricht") {
			w.State = "ready"
			w.WaitCondition = ""
			w.WaitUntil = nil
		}
	case "checkpoint", "progress":
		if w.State != "parked" && w.State != "waiting_human" && w.State != "blocked" {
			w.State = "running"
			w.WaitUntil = nil
			w.WaitCondition = ""
		}
		w.ProgressAt = s.now()
		w.NudgedAt = nil
	case "needs_input":
		if in.Gate == nil {
			return w, invalid("human wait requires an explicit gate")
		}
		if w.Gate != nil && (in.Gate.ID != w.Gate.ID || in.Gate.Kind != w.Gate.Kind || in.Gate.TransitionTo != w.Gate.TransitionTo) {
			return w, invalid("an existing human gate cannot be replaced by a report")
		}
		g := *in.Gate
		if g.ID == "" {
			g.ID = s.deps.NewID()
		}
		g.OpenedAt = s.now()
		if g.Kind == "publication" {
			if w.Branch == "" {
				return w, invalid("publication gate requires branch")
			}
			g.ExactApproval = "PUBLISH-GO " + w.Branch
		}
		w.Gate = &g
		if w.State != "parked" {
			w.State = "waiting_human"
		}
	case "waiting":
		if in.WaitCondition == "" || in.WaitUntil == nil {
			return w, invalid("external wait requires condition and deadline")
		}
		w.State = "waiting_external"
		w.WaitCondition = in.WaitCondition
		w.WaitUntil = in.WaitUntil
	case "stuck":
		w.State = "blocked"
		w.WaitCondition = in.WaitCondition
	case "done":
		if err := validateWorkExceptions(w, s.now()); err != nil {
			return w, err
		}
		if err := s.CurrentEvidence(ctx, w); err != nil {
			return w, err
		}
		if err := canClose(w); err != nil {
			return w, err
		}
		w.State = "done"
		w.ProgressAt = s.now()
	case "evidence":
	default:
		return w, invalid("unknown report kind")
	}
	return s.save(ctx, w, w.Revision, "report:"+in.Kind, in.Summary)
}
func fullSHA(sha string) bool {
	if len(sha) != 40 {
		return false
	}
	_, err := hex.DecodeString(sha)
	return err == nil
}
func reservedEvidenceKind(kind string) bool {
	switch kind {
	case "task-packet", "ci", "ci-failed", "review-report", "triage", "delta", "delta-changes", "attribution", "traceability":
		return true
	}
	return false
}
func hasEvidence(w domain.InstructorWork, kind string) bool {
	for _, e := range w.Evidence {
		matches := e.Kind == kind
		if kind == "delta-changes" {
			matches = e.Kind == "review-report" && e.Outcome == "changes_requested" && e.Phase == "delta" && e.PhaseSequence == w.DeltaPhaseSequence
		}
		if kind == "delta" {
			matches = matches && e.Outcome == "approved" && e.CriticalFindings == 0 && e.Phase == "delta" && e.PhaseSequence == w.DeltaPhaseSequence
		}
		if _, profile := w.Policy.Profiles[kind]; profile && !reservedEvidenceKind(kind) {
			matches = e.Kind == "check" && e.Profile == kind
			if w.FixPhaseSequence > 0 && e.PhaseSequence < w.FixPhaseSequence {
				matches = false
			}
		}
		if e.Verified && !e.Revoked && e.WorkID == w.ID && e.PolicyVersion == w.PolicyVersion && e.SHA == w.SHA && matches {
			return true
		}
	}
	return false
}
func canAdvance(w domain.InstructorWork, next, actorRole string) error {
	for _, t := range w.Policy.Transitions {
		if t.From != w.Phase || t.To != next {
			continue
		}
		if next == "closeout" && w.OpenCriticalFindings > 0 {
			return invalid("critical findings prevent entering closeout")
		}
		if actorRole != "instructor-engine" && actorRole != "orchestrator" && actorRole != t.Role {
			return invalid("producer role cannot drive this transition")
		}
		for _, e := range t.Requires {
			if !hasEvidence(w, e) {
				return invalid(fmt.Sprintf("transition requires verified %s for current commit", e))
			}
		}
		if t.HumanGate {
			allowed := false
			for _, grant := range w.ApprovedTransitions {
				if grant == t.From+"->"+t.To {
					allowed = true
				}
			}
			if !allowed {
				return invalid("transition requires a matching recorded human grant")
			}
		}
		return nil
	}
	return invalid("transition is not in pinned policy")
}
func closeoutEvidence(w domain.InstructorWork) error {
	if hasEvidence(w, "ci-failed") {
		return invalid("a verified remote CI failure blocks closeout and requires the governed correction loop")
	}
	if w.OpenCriticalFindings > 0 {
		return invalid("P0/P1 findings block closeout")
	}
	if !fullSHA(w.SHA) {
		return invalid("closeout requires full commit SHA")
	}
	for _, e := range w.Policy.CloseoutRequires {
		if !hasEvidence(w, e) {
			return invalid(fmt.Sprintf("closeout requires verified %s", e))
		}
	}
	approvedDelta := false
	for _, e := range w.Evidence {
		if e.Verified && !e.Revoked && e.Kind == "delta" && e.WorkID == w.ID && e.PolicyVersion == w.PolicyVersion && e.SHA == w.SHA && e.Outcome == "approved" && e.CriticalFindings == 0 && e.Phase == "delta" && e.PhaseSequence == w.DeltaPhaseSequence {
			approvedDelta = true
		}
	}
	if !approvedDelta {
		return invalid("closeout requires an approved independent delta review")
	}
	return nil
}
func canClose(w domain.InstructorWork) error {
	if w.Phase != "closeout" {
		return invalid("closeout phase has not been reached")
	}
	if w.Gate != nil {
		return invalid("open gate blocks closeout")
	}
	if !HasPublicationGrant(w) {
		return invalid("closeout requires a recorded publication grant for this work, policy, branch and commit")
	}
	return closeoutEvidence(w)
}

// Tick schedules bounded inspection, guarded workflow progression and durable delivery.
func (s *Service) Tick(ctx context.Context) error {
	if err := s.schedulePlanning(ctx); err != nil {
		return err
	}
	if s.deps.Delivery == nil {
		return invalid("instructor delivery unavailable")
	}
	for i := 0; i < 20; i++ {
		d, ok, err := s.deps.Store.ClaimInstructorDispatch(ctx, s.now())
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		w, err := s.get(ctx, d.WorkID)
		if err == nil && w.Revision != d.WorkRevision {
			err = ErrConflict
		}
		if err == nil && d.Action != "pruefen" && d.Action != "stopp" {
			err = validateWorkExceptions(w, s.now())
			if err == nil {
				err = s.CurrentEvidence(ctx, w)
			}
		}
		if errors.Is(err, ErrEvidencePending) {
			if deferErr := s.deps.Store.DeferInstructorDispatch(ctx, d.ID, err.Error(), s.now().Add(5*time.Second)); deferErr != nil {
				return deferErr
			}
			s.scheduleVerification(ctx, w)
			break
		}
		if err == nil {
			err = s.deps.Delivery.Deliver(ctx, d)
		}
		if errors.Is(err, ErrDeliveryQueued) {
			if d.QueuedAt == nil || s.now().Before(d.QueuedAt.Add(2*time.Minute)) {
				queueErr := s.deliveryQueued(ctx, d)
				if queueErr == nil {
					continue
				}
				err = queueErr
			} else {
				err = fmt.Errorf("%w: native queue did not confirm acceptance within 120 seconds", ErrDeliveryUncertain)
			}
		}
		if err != nil && d.QueuedAt != nil && !errors.Is(err, ErrDeliveryUncertain) && !errors.Is(err, ErrDeliveryRejected) {
			err = fmt.Errorf("%w: %w", ErrDeliveryUncertain, err)
		}
		state := "delivered"
		detail := ""
		if err != nil {
			state = "failed"
			detail = err.Error()
		}
		if e := s.deps.Store.SetInstructorDispatch(ctx, d.ID, state, detail, s.now()); e != nil {
			return e
		}
		if err != nil {
			if e := s.deliveryFailed(ctx, d, err); e != nil {
				return e
			}
		}
		if err != nil {
			break
		}
	}
	works, err := s.deps.Store.ListInstructorWorks(ctx)
	if err != nil {
		return err
	}
	for _, w := range works {
		s.scheduleVerification(ctx, w)
		if err := s.advanceWork(ctx, w); err != nil {
			return err
		}
		if w.State == "waiting_human" || w.State == "parked" || w.State == "blocked" || w.State == "done" || w.State == "cancelled" {
			continue
		}
		if w.State == "waiting_external" && w.WaitUntil != nil && s.now().Before(*w.WaitUntil) {
			continue
		}
		quiet := s.now().Sub(w.ProgressAt)
		if quiet < time.Duration(w.Policy.NudgeAfterSeconds)*time.Second {
			continue
		}
		s.mu.Lock()
		current, e := s.get(ctx, w.ID)
		if e != nil {
			s.mu.Unlock()
			return e
		}
		if current.Revision != w.Revision {
			s.mu.Unlock()
			continue
		}
		if current.NudgedAt == nil {
			now := s.now()
			current.NudgedAt = &now
			current.Summary = "Fortschritt angefragt"
			updated, e := s.save(ctx, current, current.Revision, "nudge", "one bounded nudge")
			if e == nil {
				d := domain.InstructorDispatch{ID: "nudge-" + s.deps.NewID(), BatchID: "nudge-" + s.deps.NewID(), WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: updated.Revision, Action: "pruefen", Message: "Report progress or precise wait condition for work " + w.ID, State: "pending", CreatedAt: now, UpdatedAt: now}
				e = s.deps.Store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{d})
			}
			s.mu.Unlock()
			if e != nil {
				return e
			}
			continue
		}
		if quiet >= time.Duration(w.Policy.EscalateAfterSeconds)*time.Second {
			current.State = "blocked"
			current.Summary = "Kein Fortschritt; Entscheidung nötig"
			_, e = s.save(ctx, current, current.Revision, "escalated", "progress timeout")
		}
		s.mu.Unlock()
		if e != nil {
			return e
		}
	}
	return nil
}

// EncodePolicy is shared by adapters that store the effective pinned policy.
func EncodePolicy(p domain.InstructorPolicy) string { b, _ := json.Marshal(p); return string(b) }

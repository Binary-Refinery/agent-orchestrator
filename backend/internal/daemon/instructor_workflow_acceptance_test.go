package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	reviewsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/review"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// These tests exercise the actual state machine, chat projection, local Git and
// check execution, review engine, evidence verifier and SQLite. Only native
// inference, SCM reads and the protected publisher are simulated. They cannot
// certify vendor judgment or claim that the ordinary host has publisher isolation.
type workflowNative struct {
	mu       sync.Mutex
	id       string
	messages []ports.ChatUserMessage
	events   chan ports.ChatEvent
	once     sync.Once
}

func (n *workflowNative) ProviderConversationID() string { return n.id }
func (*workflowNative) Capabilities() ports.ChatCapabilities {
	return ports.ChatCapabilities{ports.ChatCapabilityStreaming: true, ports.ChatCapabilityApprovals: true, ports.ChatCapabilityInterrupt: true, ports.ChatCapabilityResume: true}
}
func (n *workflowNative) SendTurn(_ context.Context, message ports.ChatUserMessage) (ports.ChatTurnRef, error) {
	n.mu.Lock()
	n.messages = append(n.messages, message)
	id := fmt.Sprintf("%s-turn-%d", n.id, len(n.messages))
	n.mu.Unlock()
	return ports.ChatTurnRef{ProviderTurnID: id}, nil
}
func (n *workflowNative) StartDeferredTurn(id string) error {
	n.events <- ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderConversationID: n.id, ProviderTurnID: id}
	n.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderConversationID: n.id, ProviderTurnID: id, TurnState: domain.TurnStateCompleted}
	return nil
}
func (*workflowNative) DiscardDeferredTurn(string)                                       {}
func (*workflowNative) Interrupt(context.Context, string) error                          { return nil }
func (*workflowNative) ResolveRequest(context.Context, string, ports.ChatDecision) error { return nil }
func (n *workflowNative) Events() <-chan ports.ChatEvent                                 { return n.events }
func (n *workflowNative) Close() error                                                   { n.once.Do(func() { close(n.events) }); return nil }
func (n *workflowNative) texts() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	result := make([]string, len(n.messages))
	for i, message := range n.messages {
		result[i] = message.Text
	}
	return result
}

type workflowDrivers struct {
	native map[domain.SessionID]*workflowNative
}
type workflowDriver struct {
	owner   *workflowDrivers
	harness domain.AgentHarness
}

func (d *workflowDrivers) Driver(h domain.AgentHarness) (ports.ChatDriver, error) {
	return workflowDriver{d, h}, nil
}
func (*workflowDrivers) SupportsChat(domain.AgentHarness) bool { return true }
func (d workflowDriver) Harness() domain.AgentHarness          { return d.harness }
func (d workflowDriver) Probe(context.Context) (ports.ChatCapabilities, error) {
	return (&workflowNative{}).Capabilities(), nil
}
func (d workflowDriver) Start(_ context.Context, cfg ports.ChatStartConfig) (ports.ChatConversation, error) {
	n := &workflowNative{id: "native-" + string(cfg.SessionID), events: make(chan ports.ChatEvent, 32)}
	d.owner.native[cfg.SessionID] = n
	return n, nil
}
func (workflowDriver) Resume(context.Context, ports.ChatResumeConfig) (ports.ChatConversation, error) {
	return nil, ports.ErrChatResumeFailed
}

type workflowTransport struct {
	store *sqlite.Store
	chat  *chatsvc.Service
}

func (s workflowTransport) InstructorControllerReady(rec domain.SessionRecord) bool {
	return s.chat.HasLiveChatController(rec.ID)
}
func (s workflowTransport) InterruptTUI(context.Context, domain.SessionID) error {
	return errors.New("unexpected terminal interrupt")
}
func (s workflowTransport) SendGovernedSemantic(ctx context.Context, id domain.SessionID, text, key string) error {
	return s.SendSemantic(ctx, id, text, key)
}
func (s workflowTransport) SendSemantic(ctx context.Context, id domain.SessionID, text, key string) error {
	rec, found, err := s.store.GetSession(ctx, id)
	if err != nil || !found {
		return fmt.Errorf("read native target: found=%t: %w", found, err)
	}
	if _, _, err := s.store.BeginSemanticDelivery(ctx, id, key, "chat:"+rec.Metadata.ControllerGeneration); err != nil {
		return err
	}
	_, err = s.chat.RelaySemanticChatTurn(ctx, id, text, key)
	return err
}

type workflowEvidence struct {
	instructorEvidenceVerifier
	reader instructorTraceabilityReader
	mu     *sync.Mutex
}

func (v workflowEvidence) Verify(ctx context.Context, w domain.InstructorWork, e domain.InstructorEvidence) error {
	if e.Kind == "traceability" {
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.verifyTraceabilityWith(ctx, w, e, v.reader)
	}
	return v.instructorEvidenceVerifier.Verify(ctx, w, e)
}
func (v workflowEvidence) Assess(ctx context.Context, w domain.InstructorWork, e domain.InstructorEvidence) (domain.InstructorEvidence, error) {
	if e.Kind == "traceability" {
		return e, v.Verify(ctx, w, e)
	}
	return v.instructorEvidenceVerifier.Assess(ctx, w, e)
}

type workflowPublisher struct{ calls int }

func (*workflowPublisher) Capabilities(context.Context, domain.InstructorWork) (domain.InstructorPublicationCapabilities, error) {
	return domain.InstructorPublicationCapabilities{Available: true, WorkerIsolation: true, ProtectedOperator: true, IsolatedCredentials: true}, nil
}
func (*workflowPublisher) Authorize(_ context.Context, d instructorsvc.PublicationDecision) (instructorsvc.PublicationAuthorization, error) {
	return instructorsvc.PublicationAuthorization{OperatorID: "isolated-test-human", AuthorizationID: "isolated-test-decision"}, nil
}
func (p *workflowPublisher) Execute(context.Context, instructorsvc.PublicationRequest) (instructorsvc.PublicationReceipt, error) {
	p.calls++
	return instructorsvc.PublicationReceipt{Ref: "https://github.com/fork/repo/pull/1"}, nil
}

type workflowFixture struct {
	t            *testing.T
	store        *sqlite.Store
	service      *instructorsvc.Service
	guard        instructorGovernance
	chat         *chatsvc.Service
	drivers      *workflowDrivers
	transport    workflowTransport
	reviews      *reviewsvc.Service
	launcher     *instructorScopeLauncher
	publisher    *workflowPublisher
	reader       *instructorTraceabilityFakeReader
	project      domain.ProjectRecord
	orch, worker domain.SessionRecord
	workID       string
	acks         map[string]bool
	now          atomic.Int64
	readerMu     sync.Mutex
}

func workflowGit(t *testing.T, path string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = path
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Fixture Operator", "GIT_AUTHOR_EMAIL=operator@example.test", "GIT_COMMITTER_NAME=Fixture Operator", "GIT_COMMITTER_EMAIL=operator@example.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newWorkflowFixture(t *testing.T, workerHarness domain.AgentHarness, reviewerHarness domain.ReviewerHarness) *workflowFixture {
	t.Helper()
	f := &workflowFixture{t: t, store: sqlitetest.MustOpen(t), publisher: &workflowPublisher{}, reader: &instructorTraceabilityFakeReader{responses: map[string]any{}}, acks: map[string]bool{}}
	f.now.Store(time.Now().UTC().UnixNano())
	path := t.TempDir()
	workflowGit(t, path, "init", "-b", "main")
	workflowGit(t, path, "commit", "--allow-empty", "-m", "Initial fixture")
	workflowGit(t, path, "checkout", "-b", "feature/workflow")
	orchHarness := domain.HarnessCodex
	if workerHarness == domain.HarnessCodex {
		orchHarness = domain.HarnessOpenCode
	}
	f.project = domain.ProjectRecord{ID: "workflow", Path: path, RegisteredAt: f.clock(), Config: domain.ProjectConfig{
		Worker:       domain.RoleOverride{Harness: workerHarness, AgentConfig: domain.AgentConfig{Model: "provider/worker", Effort: "high"}},
		Orchestrator: domain.RoleOverride{Harness: orchHarness, AgentConfig: domain.AgentConfig{Model: "provider/orchestrator", Effort: "high"}},
		Reviewers:    []domain.ReviewerConfig{{Harness: reviewerHarness, AgentConfig: domain.AgentConfig{Model: "provider/reviewer", Effort: "high"}}},
	}}
	if err := f.store.UpsertProject(context.Background(), f.project); err != nil {
		t.Fatal(err)
	}
	p := instructorsvc.DefaultProjectPolicy()
	p.Transitions[0].HumanGate = true
	for name, model := range map[string]string{"worker": "provider/worker", "reviewer": "provider/reviewer", "orchestrator": "provider/orchestrator", "instructor": "provider/instructor"} {
		role := p.Roles[name]
		role.Model, role.EffortByModel = model, map[string]string{model: "high"}
		if role.Effort == "configured" {
			role.Effort = "high"
		}
		p.Roles[name] = role
	}
	p.Profiles["push"] = domain.InstructorProfile{Command: []string{"git", "diff", "--exit-code", "HEAD"}, TimeoutSeconds: 10}
	p.Profiles["full"] = domain.InstructorProfile{Command: []string{"git", "show", "HEAD:result.txt"}, TimeoutSeconds: 10}
	p.Attribution.AuthorName, p.Attribution.AuthorEmail = "Fixture Operator", "operator@example.test"
	p.Attribution.CoAuthorEmail = "worker@example.test"
	p.Traceability.AllowedRepositories, p.Traceability.ProjectNumber = []string{"fork/repo"}, 3
	p.Traceability.BoardStatuses = map[string]string{"closeout": "Done"}
	p.Version = instructorsvc.PolicyVersion(p)
	if err := f.store.PutInstructorPolicy(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.drivers = &workflowDrivers{native: map[domain.SessionID]*workflowNative{}}
	f.chat = chatsvc.New(chatsvc.Options{Store: f.store, Sessions: f.store, Drivers: f.drivers, NewID: uuid.NewString, Now: f.clock})
	f.transport = workflowTransport{f.store, f.chat}
	f.rebuild()
	f.chat.SetSemanticDispatchValidator(func(ctx context.Context, id domain.SessionID, key string) error {
		return instructorChatAdmissionError(f.service.ValidateDispatchAdmission(ctx, id, key))
	})
	f.chat.SetAgentConfigValidator(func(ctx context.Context, id domain.SessionID, model, effort string) error {
		return f.guard.ValidateChatAgentConfig(ctx, id, model, effort)
	})
	f.launcher = &instructorScopeLauncher{}
	engine := reviewcore.New(reviewcore.Deps{Store: f.store, Sessions: f.store, PRs: f.store, Projects: f.store, Launcher: f.launcher, Clock: f.clock})
	f.reviews = reviewsvc.New(engine, f.store, reviewsvc.WithClock(f.clock))
	f.reviews.SetInstructorGovernance(f.guard)
	f.orch = f.openSession(ports.SpawnConfig{ProjectID: "workflow", Kind: domain.KindOrchestrator, Harness: orchHarness, AgentConfig: ports.AgentConfig{Model: "provider/orchestrator", Effort: "high"}})
	w, err := f.service.Register(context.Background(), instructorsvc.RegisterInput{ProjectID: "workflow", Name: "Workflow acceptance", Goal: "Implement the scoped fixture, fix independent findings, and return verified closeout", Source: "manual", SourceRef: "workflow-acceptance", IssueID: "#12", Branch: "feature/workflow"})
	if err != nil {
		t.Fatal(err)
	}
	f.workID = w.ID
	t.Cleanup(func() { _ = f.service.Shutdown(context.Background()); f.chat.StopAll(context.Background()) })
	return f
}

func (f *workflowFixture) clock() time.Time { return time.Unix(0, f.now.Load()).UTC() }

func (f *workflowFixture) rebuild() {
	delivery := &instructorDelivery{store: f.store, sessions: f.transport}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: delivery, Workspace: instructorWorkspace{store: f.store, sessions: sessionmanager.New(sessionmanager.Deps{})}, Verifier: workflowEvidence{instructorEvidenceVerifier{store: f.store}, f.reader, &f.readerMu}, RoleValidator: instructorScopeValidator{}, Publication: f.publisher, Clock: f.clock})
	delivery.binder = f.service
	f.guard = instructorGovernance{svc: f.service, store: f.store, validator: instructorScopeValidator{}}
	f.chat.SetExecutionBudgetGuard(f.guard)
	f.chat.SetAgentConfigObserver(f.guard.ObserveChatAgentConfig)
}

// Runtime creation is a provider boundary in this fixture. No Work phase,
// evidence, approval, launch or dispatch row is seeded: normal governance and
// lifecycle callbacks perform every business mutation.
func (f *workflowFixture) openSession(cfg ports.SpawnConfig) domain.SessionRecord {
	f.t.Helper()
	ctx := context.Background()
	workID, err := f.guard.PrepareLaunch(ctx, cfg, f.project)
	if err != nil {
		f.t.Fatalf("prepare native launch: %v; work=%+v", err, f.work())
	}
	rec, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: cfg.ProjectID, Kind: cfg.Kind, Harness: cfg.Harness, Mode: domain.SessionModeChat,
		Metadata: domain.SessionMetadata{WorkspacePath: f.project.Path, WorkspaceRepoPath: f.project.Path, Branch: "feature/workflow", Model: cfg.AgentConfig.Model, Effort: cfg.AgentConfig.Effort, Prompt: cfg.Prompt}, Activity: domain.Activity{State: domain.ActivityIdle}, CreatedAt: f.clock(), UpdatedAt: f.clock()})
	if err != nil {
		f.t.Fatal(err)
	}
	if workID != "" {
		if err := f.guard.AttachSession(ctx, workID, rec.ID, cfg); err != nil {
			f.t.Fatal(err)
		}
	}
	lcm := lifecycle.New(f.store, nil)
	_, err = f.chat.Start(ctx, chatsvc.StartConfig{SessionID: rec.ID, ProjectID: rec.ProjectID, Kind: rec.Kind, Harness: rec.Harness, Model: cfg.AgentConfig.Model, Effort: cfg.AgentConfig.Effort, WorkspacePath: f.project.Path,
		ControllerReady: func(r chatsvc.StartResult) (chatsvc.ControllerCommit, error) {
			metadata := rec.Metadata
			metadata.ProviderConversationID, metadata.ControllerGeneration = r.ProviderConversationID, r.ControllerGeneration
			if err := lcm.MarkSpawned(ctx, rec.ID, metadata); err != nil {
				return chatsvc.ControllerCommit{}, err
			}
			if workID != "" {
				if err := f.guard.ControllerStarted(ctx, workID, rec.ID, cfg); err != nil {
					return chatsvc.ControllerCommit{}, err
				}
			}
			return chatsvc.ControllerCommit{}, nil
		}})
	if err != nil {
		f.t.Fatal(err)
	}
	if cfg.Prompt != "" {
		f.send(rec.ID, cfg.Prompt, "initial-"+string(rec.ID))
	}
	rec, _, err = f.store.GetSession(ctx, rec.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return rec
}

func (f *workflowFixture) send(id domain.SessionID, text, key string) {
	f.t.Helper()
	turn, err := f.chat.Send(context.Background(), id, ports.ChatUserMessage{Text: text, ClientMessageID: key})
	if err != nil {
		f.t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := f.store.TurnByID(context.Background(), turn.ID)
		if err != nil {
			f.t.Fatal(err)
		}
		if current.State == domain.TurnStateCompleted {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("native turn did not finish: %+v", current)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *workflowFixture) work() domain.InstructorWork {
	f.t.Helper()
	w, found, err := f.store.GetInstructorWork(context.Background(), f.workID)
	if err != nil || !found {
		f.t.Fatalf("work: found=%t err=%v", found, err)
	}
	return w
}
func (f *workflowFixture) snapshot() domain.InstructorSnapshot {
	f.t.Helper()
	s, err := f.service.Snapshot(context.Background(), "")
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}
func (f *workflowFixture) tick() {
	f.t.Helper()
	ctx := context.Background()
	// Honor the real persistent verification delay without spending wall-clock time.
	cache, _, err := f.store.GetInstructorVerification(ctx, f.workID)
	if err != nil {
		f.t.Fatal(err)
	}
	if cache.Status != "running" {
		f.now.Add(int64(31 * time.Second))
	}
	if err := f.service.VerifyWork(ctx, f.workID); err != nil {
		f.t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		f.t.Fatal(err)
	}
	rows, err := f.store.ListInstructorDispatches(ctx, "", f.workID)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, row := range rows {
		if row.NativeReceipt == nil || row.NativeReceipt.StartedAt == nil || f.acks[row.ID] {
			continue
		}
		if _, err := f.service.Report(ctx, instructorsvc.ReportInput{WorkID: f.workID, SessionID: f.orch.ID, DispatchID: row.ID, Kind: "accepted"}); err != nil {
			f.t.Fatal(err)
		}
		f.acks[row.ID] = true
	}
}
func (f *workflowFixture) reach(phase, state string) domain.InstructorWork {
	f.t.Helper()
	for range 30 {
		f.tick()
		w := f.work()
		if w.Phase == phase && (state == "" || w.State == state) {
			return w
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.failProgress("did not reach " + phase + "/" + state)
	return domain.InstructorWork{}
}

func (f *workflowFixture) failProgress(reason string) {
	f.t.Helper()
	w := f.work()
	verification, _, _ := f.store.GetInstructorVerification(context.Background(), f.workID)
	events, _ := f.store.ListInstructorEvents(context.Background(), f.workID)
	if len(events) > 12 {
		events = events[len(events)-12:]
	}
	f.t.Fatalf("%s: phase=%s state=%s revision=%d gate=%+v wait=%s verification=%+v recent events=%+v", reason, w.Phase, w.State, w.Revision, w.Gate, w.WaitCondition, verification, events)
}

func (f *workflowFixture) awaitPhaseOrder() {
	f.t.Helper()
	w := f.work()
	prefix := fmt.Sprintf("Instructor workflow work=%s phase=%s policy=", w.ID, w.Phase)
	for range 20 {
		f.tick()
		rows, err := f.store.ListInstructorDispatches(context.Background(), "", f.workID)
		if err != nil {
			f.t.Fatal(err)
		}
		for _, row := range rows {
			if strings.HasPrefix(row.Message, prefix) && row.NativeReceipt != nil && row.NativeReceipt.StartedAt != nil {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	f.failProgress("scripted orchestrator has not received its phase order")
}

func (f *workflowFixture) approve(approval string) {
	f.t.Helper()
	in := instructorsvc.CommandInput{Command: "Go", SnapshotID: f.snapshot().ID, WorkIDs: []string{f.workID}, Approval: approval, IdempotencyKey: "human-" + f.work().Gate.ID}
	first, err := f.service.Command(context.Background(), in)
	if err != nil {
		f.t.Fatal(err)
	}
	retry, err := f.service.Command(context.Background(), in)
	if err != nil || first.BatchID != retry.BatchID {
		f.t.Fatalf("operator retry duplicated intent: %v", err)
	}
}

func (f *workflowFixture) commitResult(text string) string {
	f.t.Helper()
	packet := filepath.Join(f.project.Path, ".ao", "task-packets", f.workID+".md")
	if err := os.MkdirAll(filepath.Dir(packet), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(packet, []byte("Work "+f.workID+"\nImplement only result.txt. Preserve the fixture. Independent review and explicit human correction/publication approvals are required.\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.project.Path, "result.txt"), []byte(text+"\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
	workflowGit(f.t, f.project.Path, "add", "result.txt", ".ao/task-packets")
	workflowGit(f.t, f.project.Path, "commit", "-m", "Implement scoped fixture\n\nCo-authored-by: AO Worker <worker@example.test>\nAO-session: "+string(f.worker.ID)+"\nIssue: #12")
	return workflowGit(f.t, f.project.Path, "rev-parse", "HEAD")
}

func (f *workflowFixture) checkAndVerifyLocal() {
	f.t.Helper()
	ctx := context.Background()
	w, err := f.service.RunChecks(ctx, f.workID, f.worker.ID, []string{"push", "full"})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, kind := range []string{"task-packet", "attribution"} {
		ref := w.SHA
		if kind == "task-packet" {
			ref = ".ao/task-packets/" + w.ID + ".md"
		}
		e := domain.InstructorEvidence{WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: kind, Producer: f.worker.ID, Role: "worker", Ref: ref, Harness: string(f.worker.Harness), Model: f.worker.Metadata.Model, Effort: f.worker.Metadata.Effort}
		if _, err := f.service.VerifyEvidence(ctx, w.ID, e); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *workflowFixture) review(verdict domain.ReviewVerdict) domain.ReviewRun {
	f.t.Helper()
	ctx := context.Background()
	w := f.work()
	role := w.Policy.Roles["reviewer"]
	result, err := f.reviews.TriggerManagedLocal(ctx, f.worker.ID, domain.ReviewerHarness(role.Harness), domain.AgentConfig{Model: role.Model, Effort: role.EffortByModel[role.Model]}, w.SHA)
	if err != nil || !result.Created {
		f.t.Fatalf("independent native review: %+v %v", result, err)
	}
	duplicate, err := f.reviews.TriggerManagedLocal(ctx, f.worker.ID, domain.ReviewerHarness(role.Harness), domain.AgentConfig{Model: role.Model, Effort: role.EffortByModel[role.Model]}, w.SHA)
	if err != nil || duplicate.Created || duplicate.Run.ID != result.Run.ID {
		f.t.Fatalf("duplicate review launched: %+v %v", duplicate, err)
	}
	if result.Run.ReviewID == string(f.worker.ID) || result.Run.ScopeKey != fmt.Sprintf("%s/%s/%d", w.ID, w.Phase, w.PhaseSequence) {
		f.t.Fatalf("review lost independent exact-phase ownership: %+v", result.Run)
	}
	if _, err := f.reviews.Submit(ctx, f.orch.ID, result.Run.ID, verdict, "wrong producer", ""); err == nil {
		f.t.Fatal("foreign worker identity submitted review")
	}
	body := "Independent finding: result.txt must say corrected; preserve the rest of the fixture."
	if verdict == domain.VerdictApproved {
		body = "Independent delta confirms the authorized correction at the requested exact commit."
	}
	run, err := f.reviews.Submit(ctx, f.worker.ID, result.Run.ID, verdict, body, "")
	if err != nil {
		f.t.Fatal(err)
	}
	return run
}

func (f *workflowFixture) remoteEvidence(firstSHA string) {
	f.t.Helper()
	w := f.work()
	url := "https://github.com/fork/repo/pull/1"
	comments := make([]instructorGitHubComment, 0, len(w.Policy.Traceability.RequiredPhases))
	for i, phase := range []string{"start", "review", "fix", "commit", "push", "pr", "ci", "delta", "closeout"} {
		sha := w.SHA
		if phase == "start" || phase == "review" || phase == "fix" {
			sha = firstSHA
		}
		data, err := json.Marshal(instructorPhaseComment{WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: sha, Phase: phase, SessionID: f.worker.ID})
		if err != nil {
			f.t.Fatal(err)
		}
		comments = append(comments, instructorGitHubComment{Body: "<!-- ao-phase:v1 -->\n```json\n" + string(data) + "\n```", CreatedAt: w.CreatedAt.Add(time.Duration(i+1) * time.Second).Format(time.RFC3339Nano)})
	}
	f.readerMu.Lock()
	f.reader.responses = map[string]any{
		"repos/fork/repo/pulls/1":                                map[string]any{"html_url": url, "base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "fork/repo"}}, "head": map[string]any{"sha": w.SHA, "ref": w.Branch}},
		"repos/fork/repo/issues/12":                              map[string]any{"number": 12, "html_url": "https://github.com/fork/repo/issues/12", "assignees": []any{map[string]any{"login": "fixture-human"}}},
		"repos/fork/repo/issues/12/comments?per_page=100&page=1": comments,
		"graphql": map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{"projectItems": map[string]any{"nodes": []any{map[string]any{"project": map[string]any{"number": 3, "url": "https://github.com/users/fork/projects/3"}, "status": map[string]any{"name": "Done"}}}, "pageInfo": map[string]any{"hasNextPage": false}}}}}},
	}
	f.readerMu.Unlock()
	if err := f.store.WriteSCMObservation(context.Background(), domain.PullRequest{SessionID: f.worker.ID, URL: url, Number: 1, Repo: "fork/repo", HeadSHA: w.SHA, UpdatedAt: f.clock()}, []domain.PullRequestCheck{{Name: "ci", Status: domain.PRCheckPassed, Conclusion: "success", CommitHash: w.SHA}}, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		f.t.Fatal(err)
	}
}

func TestInstructorOrganicWorkflowRequiresHumanGatesAndCompleteNativeEvidence(t *testing.T) {
	for _, choice := range []struct {
		worker   domain.AgentHarness
		reviewer domain.ReviewerHarness
	}{
		{domain.HarnessOpenCode, domain.ReviewerCodex},
		{domain.HarnessCodex, domain.ReviewerOpenCode},
	} {
		t.Run(string(choice.worker)+"-"+string(choice.reviewer), func(t *testing.T) {
			f := newWorkflowFixture(t, choice.worker, choice.reviewer)
			ctx := context.Background()
			f.reach("intake", "waiting_human")
			brief := "Concrete subtask " + f.workID + ": write result.txt with initial, commit the task packet, run configured profiles and report evidence; do not publish or self-approve review."
			cfg := ports.SpawnConfig{ProjectID: "workflow", ParentSessionID: f.orch.ID, WorkID: f.workID, WorkRole: "worker", Kind: domain.KindWorker, Harness: choice.worker, AgentConfig: ports.AgentConfig{Model: "provider/worker", Effort: "high"}, Prompt: brief}
			if _, err := f.guard.PrepareLaunch(ctx, cfg, f.project); err == nil {
				t.Fatal("worker started before captured human intake approval")
			}
			f.approve("")
			f.reach("implement", "")
			f.awaitPhaseOrder() // Observe the real native order before acting on it.
			beforeLaunch := f.work()
			missingBrief := cfg
			missingBrief.Prompt = " \n\t"
			if _, err := f.guard.PrepareLaunch(ctx, missingBrief, f.project); !errors.Is(err, ports.ErrGovernedTaskRequired) {
				t.Fatalf("empty worker briefing was not rejected before native launch: %v", err)
			}
			wrongRole := cfg
			wrongRole.WorkRole = "orchestrator"
			if _, err := f.guard.PrepareLaunch(ctx, wrongRole, f.project); err == nil {
				t.Fatal("worker inherited control authority through role override")
			}
			if f.work().Revision != beforeLaunch.Revision || len(f.drivers.native) != 1 {
				t.Fatal("rejected worker task mutated Work or started a native conversation")
			}
			f.worker = f.openSession(cfg)
			if got := f.drivers.native[f.worker.ID].texts(); len(got) != 1 || got[0] != brief {
				t.Fatalf("worker did not receive exact concrete subtask once: %q", got)
			}
			orders := f.drivers.native[f.orch.ID].texts()
			if len(orders) == 0 || !strings.Contains(orders[0], "ao spawn --work \""+f.workID+"\"") || strings.Contains(orders[0], brief) {
				t.Fatalf("orchestrator delegation was replaced by worker prompt: %q", orders)
			}
			firstSHA := f.commitResult("initial")
			if _, err := f.service.Report(ctx, instructorsvc.ReportInput{WorkID: f.workID, SessionID: f.worker.ID, Kind: "done"}); err == nil {
				t.Fatal("worker done claim skipped verification chain")
			}
			report, err := reportsvc.New(reportsvc.Deps{Store: f.store, ManagedDone: f.service, Now: f.clock}).Create(ctx, reportsvc.CreateInput{SessionID: f.worker.ID, State: domain.ReportDone, Note: "Scoped implementation committed; independent review is still required"})
			if err != nil || report.DeliveryState != domain.ReportClaimed {
				t.Fatalf("worker result not routed through durable orchestrator handoff: %+v %v", report, err)
			}
			f.tick()
			f.checkAndVerifyLocal()
			f.reach("review", "")
			f.awaitPhaseOrder()
			initialReview := f.review(domain.VerdictChangesRequested)
			f.reach("triage", "waiting_human")
			before := f.work()
			if before.OpenCriticalFindings != 1 {
				t.Fatalf("independent finding not retained: %+v", before)
			}
			if _, err := f.service.RunChecks(ctx, f.workID, f.worker.ID, []string{"push"}); err == nil {
				t.Fatal("human triage gate allowed worker execution")
			}
			if _, err := f.service.Report(ctx, instructorsvc.ReportInput{WorkID: f.workID, SessionID: f.worker.ID, Kind: "progress", Phase: "fix"}); err == nil {
				t.Fatal("worker self-approved correction")
			}
			if _, err := f.chat.Send(ctx, f.worker.ID, ports.ChatUserMessage{Text: "unapproved fix", ClientMessageID: "unapproved-correction"}); err == nil {
				t.Fatal("native worker chat bypassed human correction gate")
			}
			beforeCount := len(f.drivers.native[f.orch.ID].texts())
			if err := f.service.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			f.rebuild()
			f.reviews.SetInstructorGovernance(f.guard)
			f.tick()
			if len(f.drivers.native[f.orch.ID].texts()) != beforeCount || f.work().Gate.ID != before.Gate.ID {
				t.Fatal("restart replayed a delivered command or replaced the human gate")
			}
			f.approve("")
			f.reach("fix", "")
			f.awaitPhaseOrder()
			f.send(f.worker.ID, "Approved correction only: change result.txt from initial to corrected and preserve other files.", "approved-correction")
			finalSHA := f.commitResult("corrected")
			if finalSHA == firstSHA {
				t.Fatal("fixture did not create a distinct corrective commit")
			}
			f.checkAndVerifyLocal()
			f.reach("delta", "")
			f.awaitPhaseOrder()
			delta := f.review(domain.VerdictApproved)
			if delta.ID == initialReview.ID || delta.TargetSHA != finalSHA {
				t.Fatal("delta reused old review or old commit")
			}
			w := f.reach("closeout", "waiting_human")
			if w.Gate == nil || w.Gate.Kind != "publication" || w.OpenCriticalFindings != 0 {
				t.Fatalf("local verification did not end at publication gate: %+v", w)
			}
			wrong := instructorsvc.CommandInput{Command: "Go", SnapshotID: f.snapshot().ID, WorkIDs: []string{w.ID}, Approval: "Go", IdempotencyKey: "wrong-publication-text"}
			if _, err := f.service.Command(ctx, wrong); err == nil {
				t.Fatal("generic Go authorized publication")
			}
			if caps, err := (hostPublicationBoundary{}).Capabilities(ctx, w); err != nil || caps.Available || caps.WorkerIsolation || caps.ProtectedOperator || caps.IsolatedCredentials {
				t.Fatal("ordinary host claimed protected publication")
			}
			if _, err := (hostPublicationBoundary{}).Authorize(ctx, instructorsvc.PublicationDecision{Approval: w.Gate.ExactApproval}); !errors.Is(err, instructorsvc.ErrPublicationUnavailable) {
				t.Fatalf("ordinary host accepted human phrase without isolation: %v", err)
			}
			f.approve(w.Gate.ExactApproval)
			f.tick()
			w = f.work()
			if w.State == "done" {
				t.Fatal("local approvals fabricated remote CI and traceability")
			}
			grant := w.PublicationGrants[len(w.PublicationGrants)-1]
			publication := instructorsvc.PublicationInput{WorkID: w.ID, GrantID: grant.ID, SnapshotID: grant.SnapshotID, WorkRevision: w.Revision, SHA: w.SHA, Branch: w.Branch, Operation: grant.Operation}
			if _, err := f.service.ExecutePublication(ctx, publication); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.ExecutePublication(ctx, publication); err == nil || f.publisher.calls != 1 {
				t.Fatalf("publication replay crossed external boundary: %v calls=%d", err, f.publisher.calls)
			}
			if _, err := f.service.Report(ctx, instructorsvc.ReportInput{WorkID: f.workID, SessionID: f.worker.ID, Kind: "done"}); err == nil {
				t.Fatal("publication receipt substituted for remote evidence")
			}
			f.remoteEvidence(firstSHA)
			closed := f.reach("closeout", "done")
			if closed.FixRounds != 1 || closed.SHA != finalSHA || closed.PolicyVersion != before.PolicyVersion {
				t.Fatalf("closeout lost work/policy/fix lineage: %+v", closed)
			}
			snapshot := f.snapshot()
			if len(snapshot.Works) != 1 || snapshot.Works[0].State != "done" || snapshot.Works[0].ID != f.workID {
				t.Fatalf("Instructor view did not retain completed work: %+v", snapshot.Works)
			}
		})
	}
}

func TestInstructorOrganicLateAcceptancePreservesQueuedPhase(t *testing.T) {
	f := newWorkflowFixture(t, domain.HarnessOpenCode, domain.ReviewerCodex)
	ctx := context.Background()
	f.reach("intake", "waiting_human")
	f.approve("")
	// The native Go turn starts; the engine queues the next phase before the
	// orchestrator posts its legitimate asynchronous acceptance of that turn.
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.ListInstructorDispatches(ctx, "", f.workID)
	if err != nil {
		t.Fatal(err)
	}
	var started, pending domain.InstructorDispatch
	for _, row := range rows {
		if row.NativeReceipt != nil && row.NativeReceipt.StartedAt != nil {
			started = row
		}
		if row.State == "pending" && strings.HasPrefix(row.ID, "phase-") {
			pending = row
		}
	}
	if started.ID == "" || pending.ID == "" || pending.WorkRevision != f.work().Revision {
		t.Fatalf("fixture did not reach asynchronous receipt boundary: %+v", rows)
	}
	if _, err := f.service.Report(ctx, instructorsvc.ReportInput{WorkID: f.workID, SessionID: f.orch.ID, DispatchID: started.ID, Kind: "accepted"}); err != nil {
		t.Fatal(err)
	}
	rows, err = f.store.ListInstructorDispatches(ctx, "", f.workID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == pending.ID && (row.WorkRevision != f.work().Revision || row.Message != pending.Message || row.State != "pending") {
			t.Fatalf("informational receipt invalidated or rewrote queued phase: %+v", row)
		}
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	w := f.work()
	if w.Phase != "implement" || w.State == "waiting_human" || w.Gate != nil {
		t.Fatalf("late native acceptance caused a delivery approval loop: phase=%s state=%s gate=%+v", w.Phase, w.State, w.Gate)
	}
	count := 0
	for _, message := range f.drivers.native[f.orch.ID].texts() {
		if strings.HasPrefix(message, pending.Message+"\n\nInstructor dispatch: "+pending.ID) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("phase directive reached native orchestrator %d times", count)
	}
}

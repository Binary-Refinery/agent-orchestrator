package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type planningConversation struct {
	events                                      chan ports.ChatEvent
	options                                     []ports.ChatConfigOption
	message                                     ports.ChatUserMessage
	sends, starts, resolves, terminates, closes int
	isolationErr                                error
}

func (*planningConversation) ProviderConversationID() string { return "native-plan" }
func (*planningConversation) Capabilities() ports.ChatCapabilities {
	return ports.ChatCapabilities{ports.ChatCapabilityStreaming: true}
}
func (c *planningConversation) SendTurn(_ context.Context, msg ports.ChatUserMessage) (ports.ChatTurnRef, error) {
	c.sends++
	c.message = msg
	return ports.ChatTurnRef{ProviderTurnID: "plan-turn"}, nil
}
func (*planningConversation) Interrupt(context.Context, string) error { return nil }
func (c *planningConversation) ResolveRequest(context.Context, string, ports.ChatDecision) error {
	c.resolves++
	return nil
}
func (c *planningConversation) Events() <-chan ports.ChatEvent { return c.events }
func (c *planningConversation) Close() error                   { c.closes++; return nil }
func (c *planningConversation) Terminate() error               { c.terminates++; return nil }
func (*planningConversation) PreservesProviderOnClose() bool   { return true }
func (c *planningConversation) StartDeferredTurn(string) error { c.starts++; return nil }
func (*planningConversation) DiscardDeferredTurn(string)       {}
func (c *planningConversation) ListConfigOptions(context.Context) ([]ports.ChatConfigOption, error) {
	return c.options, nil
}
func (*planningConversation) SetConfigOption(context.Context, string, ports.ChatConfigOptionValue) ([]ports.ChatConfigOption, error) {
	return nil, errors.New("planner must not tune native config")
}
func (c *planningConversation) ValidateNoExternalTools(context.Context) error { return c.isolationErr }

type planningDriver struct {
	conv     *planningConversation
	cfg      ports.ChatStartConfig
	startErr error
	starts   int
}

func (*planningDriver) Harness() domain.AgentHarness { return domain.HarnessCodex }
func (*planningDriver) Probe(context.Context) (ports.ChatCapabilities, error) {
	return ports.ChatCapabilities{ports.ChatCapabilityStreaming: true}, nil
}
func (d *planningDriver) Start(_ context.Context, cfg ports.ChatStartConfig) (ports.ChatConversation, error) {
	d.cfg = cfg
	d.starts++
	return d.conv, d.startErr
}
func (*planningDriver) Resume(context.Context, ports.ChatResumeConfig) (ports.ChatConversation, error) {
	return nil, errors.New("planning must never resume a worker")
}

type planningRegistry struct{ driver *planningDriver }

func (r planningRegistry) Driver(domain.AgentHarness) (ports.ChatDriver, error) { return r.driver, nil }
func (planningRegistry) SupportsChat(domain.AgentHarness) bool                  { return true }

type planningProjects struct{ rows []domain.ProjectRecord }

func (p planningProjects) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	return p.rows, nil
}

func planningFixture(t *testing.T) (instructorNativePlanner, *planningDriver, domain.InstructorPlanJob, domain.InstructorSnapshot) {
	t.Helper()
	c := &planningConversation{events: make(chan ports.ChatEvent, 4)}
	c.events <- ports.ChatEvent{Kind: ports.ChatEventMessageCompleted, ProviderTurnID: "plan-turn", Text: `{"summary":"One work waits for operator review.","questions":[],"proposals":[{"command":"DETAILS","workIds":["work"],"reason":"Inspect the observed gate"}],"intake":[]}`}
	c.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: "plan-turn", TurnState: domain.TurnStateCompleted}
	d := &planningDriver{conv: c}
	p := instructorNativePlanner{drivers: planningRegistry{d}, dataDir: t.TempDir(), projects: planningProjects{[]domain.ProjectRecord{{ID: "project", DisplayName: "Project", Path: "sensitive-project-path", Config: domain.ProjectConfig{}}}}}
	job := domain.InstructorPlanJob{ID: "job", Requested: domain.InstructorAgentSelection{Harness: "codex", Model: "native-model", Effort: "native-ultra"}, Input: domain.InstructorPlanInput{Request: "What needs attention?", SnapshotID: "frozen", TimeoutSeconds: 900}}
	return p, d, job, domain.InstructorSnapshot{ID: "frozen", Works: []domain.InstructorWork{{ID: "work"}}}
}

func TestNativeInstructorPlanningUsesPinnedIsolatedAdviceAndHonestReadback(t *testing.T) {
	for _, kind := range []string{"unknown", "partial", "synthetic", "reported"} {
		t.Run(kind, func(t *testing.T) {
			p, d, job, snapshot := planningFixture(t)
			snapshot.Works[0].Gate = &domain.InstructorGate{ExactApproval: "existing-human-signature"}
			snapshot.Works[0].PublicationGrants = []domain.InstructorPublicationGrant{{Approval: "existing-human-signature"}}
			snapshot.Works[0].Exceptions = []domain.InstructorException{{Approval: "existing-human-signature"}}
			model := ports.ChatConfigOption{ID: "model", Category: "model", CurrentReported: true, Current: ports.ChatConfigOptionValue{Select: job.Requested.Model}}
			effort := ports.ChatConfigOption{ID: "effort", Category: "thought_level", CurrentReported: true, Current: ports.ChatConfigOptionValue{Select: job.Requested.Effort}}
			switch kind {
			case "partial":
				d.conv.options = []ports.ChatConfigOption{effort}
			case "synthetic":
				effort.CurrentReported = false
				d.conv.options = []ports.ChatConfigOption{model, effort}
			case "reported":
				d.conv.options = []ports.ChatConfigOption{model, effort}
			}
			t.Setenv("AO_API_TOKEN", "authority-secret")
			calls, confirmed := 0, 0
			output, err := p.Run(context.Background(), job, snapshot, nil, func(sent domain.InstructorAgentSelection, native *domain.InstructorAgentSelection) error {
				calls++
				if sent != job.Requested {
					t.Fatalf("changed sent config: %+v", sent)
				}
				if native != nil {
					confirmed++
					if *native != job.Requested {
						t.Fatal("fabricated native selection")
					}
				}
				return nil
			})
			if err != nil || output.Summary == "" {
				t.Fatalf("planning failed: %+v %v", output, err)
			}
			if d.cfg.Model != job.Requested.Model || d.cfg.Effort != job.Requested.Effort || !d.cfg.ReadOnly || !d.cfg.Ephemeral || len(d.cfg.MCPServers) != 0 || len(d.cfg.AdditionalDirectories) != 0 || d.cfg.PrepareEnv != nil || d.cfg.Env["AO_API_TOKEN"] != "" {
				t.Fatalf("native planner leaked authority/config: %+v", d.cfg)
			}
			if !strings.Contains(d.cfg.WorkspacePath, "/instructor/planning/job/workspace") || !strings.Contains(d.cfg.DataDir, "/instructor/planning/job/state") {
				t.Fatalf("planner adopted project state: %+v", d.cfg)
			}
			if strings.Contains(d.conv.message.Text, "sensitive-project-path") || strings.Contains(d.conv.message.Text, "authority-secret") || strings.Contains(d.conv.message.Text, "existing-human-signature") || !strings.Contains(d.conv.message.Text, "frozen") {
				t.Fatal("planner prompt omitted frozen portfolio or leaked native configuration")
			}
			if snapshot.Works[0].Gate.ExactApproval != "existing-human-signature" || snapshot.Works[0].PublicationGrants[0].Approval != "existing-human-signature" || snapshot.Works[0].Exceptions[0].Approval != "existing-human-signature" {
				t.Fatal("planner mutated its frozen authority snapshot")
			}
			if d.conv.sends != 1 || d.conv.starts != 1 || d.conv.resolves != 0 || d.conv.terminates != 1 || d.conv.closes != 1 {
				t.Fatalf("native lifecycle: %+v", d.conv)
			}
			if kind == "reported" {
				if calls != 2 || confirmed != 1 {
					t.Fatal("native confirmation missing")
				}
			} else if calls != 1 || confirmed != 0 {
				t.Fatal("requested choice became native confirmation")
			}
		})
	}
}

func TestNativeInstructorPlanningRefusesAuthorityAndUncertainNativeChoice(t *testing.T) {
	for _, kind := range []string{"unsupported-read-only", "clamped", "external-tools", "record-failure", "approval", "input", "signature", "extra-json", "rerouted", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			p, d, job, snapshot := planningFixture(t)
			callbackErr := error(nil)
			switch kind {
			case "unsupported-read-only":
				d.startErr = ports.ErrChatConfigOptionInvalid
			case "clamped":
				d.conv.options = []ports.ChatConfigOption{{ID: "effort", Category: "thought_level", CurrentReported: true, Current: ports.ChatConfigOptionValue{Select: "low"}}}
			case "external-tools":
				d.conv.isolationErr = errors.New("native MCP configured")
			case "record-failure":
				callbackErr = errors.New("durable sent metadata unavailable")
			default:
				d.conv.events = make(chan ports.ChatEvent, 3)
				switch kind {
				case "approval":
					d.conv.events <- ports.ChatEvent{Kind: ports.ChatEventApprovalRequested}
				case "input":
					d.conv.events <- ports.ChatEvent{Kind: ports.ChatEventInputRequested}
				case "rerouted":
					d.conv.events <- ports.ChatEvent{Kind: ports.ChatEventModelRerouted, Reroute: &ports.ChatModelReroute{FromModel: job.Requested.Model, ToModel: "provider-fallback"}}
				case "signature":
					d.conv.events <- ports.ChatEvent{Kind: ports.ChatEventMessageCompleted, Text: `{"summary":"ready","approval":"GO"}`}
					d.conv.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, TurnState: domain.TurnStateCompleted}
				case "extra-json":
					d.conv.events <- ports.ChatEvent{Kind: ports.ChatEventMessageCompleted, Text: `{"summary":"ready"} {"summary":"extra"}`}
					d.conv.events <- ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, TurnState: domain.TurnStateCompleted}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			calls := 0
			_, err := p.Run(ctx, job, snapshot, nil, func(domain.InstructorAgentSelection, *domain.InstructorAgentSelection) error {
				calls++
				return callbackErr
			})
			if err == nil {
				t.Fatalf("unsafe planning case %s was accepted", kind)
			}
			if d.conv.resolves != 0 {
				t.Fatal("planner granted authority")
			}
			if kind == "unsupported-read-only" {
				if calls != 0 || d.conv.sends != 0 {
					t.Fatal("failed native start was marked sent")
				}
				return
			}
			if d.conv.terminates != 1 || d.conv.closes != 1 {
				t.Fatal("planning provider was orphaned")
			}
			if kind == "clamped" || kind == "external-tools" || kind == "record-failure" {
				if d.conv.sends != 0 {
					t.Fatal("unverified native provider received planning prompt")
				}
			}
		})
	}
}

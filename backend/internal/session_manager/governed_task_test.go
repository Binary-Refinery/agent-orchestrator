package sessionmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	browsersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/browser"
)

type taskGovernance struct{ task, goal string }

func (g *taskGovernance) PrepareLaunch(_ context.Context, cfg ports.SpawnConfig, _ domain.ProjectRecord) (string, error) {
	g.task, g.goal = cfg.ResolvedTaskPrompt, cfg.Prompt
	if strings.TrimSpace(cfg.ResolvedTaskPrompt) == "" {
		return "", ports.ErrGovernedTaskRequired
	}
	return "work-with-a-larger-goal", nil
}

func (*taskGovernance) AttachSession(context.Context, string, domain.SessionID, ports.SpawnConfig) error {
	return nil
}
func (*taskGovernance) ValidateResume(context.Context, domain.SessionID) error { return nil }
func (*taskGovernance) SpawnFailed(context.Context, string, error) error       { return nil }

func TestGovernedTaskRejectionPrecedesSessionWorkspaceAndController(t *testing.T) {
	for _, mode := range []string{"tui", "chat", "async-chat"} {
		t.Run(mode, func(t *testing.T) {
			launcher := &recordingLauncher{}
			m, store, runtime := newChatManager(launcher)
			m.SetInstructorGovernance(&taskGovernance{})
			deferred := deferredBackground(m)
			cfg := asyncChatSpawnConfig(" \t\n")
			cfg.WorkID = "existing-work-with-a-goal"
			cfg.Attachments = []ports.SpawnAttachment{{Ext: ".txt", Data: []byte("context only")}}
			cfg.Async = mode == "async-chat"
			if mode == "tui" {
				cfg.Harness, cfg.RequestedMode = domain.HarnessClaudeCode, domain.SessionModeTUI
			}
			_, _, _, err := m.Spawn(context.Background(), cfg)
			if !errors.Is(err, ports.ErrGovernedTaskRequired) {
				t.Fatalf("missing task not rejected: %v", err)
			}
			if len(store.sessions) != 0 || m.workspace.(*fakeWorkspace).createCount != 0 || runtime.created != 0 || len(launcher.started) != 0 || len(launcher.queued) != 0 || len(*deferred) != 0 {
				t.Fatal("rejected task allocated a session, workspace, controller, or background queue")
			}
		})
	}
}

func TestGovernedSpawnAdmitsAndDeliversResolvedTaskExactlyOnce(t *testing.T) {
	for _, async := range []bool{false, true} {
		for _, taskSource := range []string{"explicit", "issue", "whitespace-with-issue", "large-issue"} {
			t.Run(taskSource+map[bool]string{false: "/sync", true: "/async"}[async], func(t *testing.T) {
				launcher := &recordingLauncher{}
				m, store, _ := newChatManager(launcher)
				m.browserCapabilities = browsersvc.NewAuthority()
				guard := &taskGovernance{}
				m.SetInstructorGovernance(guard)
				deferred := deferredBackground(m)
				cfg := asyncChatSpawnConfig("Fix only the parser boundary test")
				cfg.Async = async
				if taskSource != "explicit" {
					cfg.Prompt, cfg.IssueID = "", "117"
					cfg.IssueContext = "Title: Fix the parser boundary test"
					if taskSource == "whitespace-with-issue" {
						cfg.Prompt = " \t\n"
					}
					if taskSource == "large-issue" {
						cfg.IssueContext += strings.Repeat(" issue details", 1000)
					}
				}
				rec, promptBytes, _, err := m.Spawn(context.Background(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				want := buildPrompt(cfg)
				if want == "" || guard.task != want || promptBytes != len(want) {
					t.Fatalf("actual task not used for admission: task=%q bytes=%d want=%q", guard.task, promptBytes, want)
				}
				if guard.goal != cfg.Prompt {
					t.Fatal("resolved issue context replaced the Work goal input")
				}
				if async {
					if len(launcher.started) != 0 || len(launcher.queued) != 1 || launcher.queued[0] != want || len(*deferred) != 1 {
						t.Fatalf("async result did not retain one opening task: %+v", launcher)
					}
					(*deferred)[0]()
					if len(launcher.turns) != 0 || len(launcher.queued) != 1 || len(launcher.drained) != 1 {
						t.Fatal("async task bypassed or duplicated its queued delivery")
					}
				} else if len(launcher.turns) != 1 || launcher.turns[0] != want || len(launcher.queued) != 0 {
					t.Fatalf("sync task not delivered exactly once: turns=%q queued=%q", launcher.turns, launcher.queued)
				}
				if store.sessions[rec.ID].Metadata.Prompt != want {
					t.Fatalf("task receipt lost from session metadata: %+v", store.sessions[rec.ID].Metadata)
				}
			})
		}
	}
}

func TestUnmanagedWorkerCanStillOpenWithoutTask(t *testing.T) {
	launcher := &recordingLauncher{}
	m, _, _ := newChatManager(launcher)
	m.browserCapabilities = browsersvc.NewAuthority()
	cfg := asyncChatSpawnConfig("")
	cfg.Async = false
	if _, _, _, err := m.Spawn(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if len(launcher.started) != 1 || len(launcher.turns) != 0 {
		t.Fatal("unmanaged idle session acquired an invented task")
	}
}

package sessionmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type launchGovernanceFake struct {
	prepareError error
	attachError  error
	resumeError  error
	attached     domain.SessionID
	started      domain.SessionID
	failed       bool
	startError   error
	runtime      *fakeRuntime
}

type unavailableGovernanceInstructions struct{ *launchGovernanceFake }

func (unavailableGovernanceInstructions) Instructions(context.Context, string) (string, error) {
	return "", errGovernanceInstructions
}

var errGovernanceInstructions = errors.New("pinned governance instructions unavailable")

func TestInstructorMissingInstructionsRecordsFailureBeforeControllerCreation(t *testing.T) {
	m, store, runtime, workspace := newManager()
	guard := &launchGovernanceFake{runtime: runtime}
	m.SetInstructorGovernance(unavailableGovernanceInstructions{guard})
	if _, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode}); !errors.Is(err, errGovernanceInstructions) {
		t.Fatalf("instructions failure hidden: %v", err)
	}
	if runtime.created != 0 || len(store.sessions) != 0 || workspace.createCount != 0 || !guard.failed {
		t.Fatalf("launch escaped missing guidance: runtime=%d sessions=%d workspace=%d recordedFailure=%t", runtime.created, len(store.sessions), workspace.createCount, guard.failed)
	}
}

func (f *launchGovernanceFake) PrepareLaunch(context.Context, ports.SpawnConfig, domain.ProjectRecord) (string, error) {
	return "work-stable", f.prepareError
}
func (f *launchGovernanceFake) AttachSession(_ context.Context, _ string, id domain.SessionID, _ ports.SpawnConfig) error {
	if f.runtime.created != 0 {
		return errors.New("controller launched before work attachment")
	}
	f.attached = id
	return f.attachError
}
func (f *launchGovernanceFake) ValidateResume(context.Context, domain.SessionID) error {
	return f.resumeError
}
func (f *launchGovernanceFake) SpawnFailed(context.Context, string, error) error {
	f.failed = true
	return nil
}
func (f *launchGovernanceFake) ControllerStarted(_ context.Context, _ string, id domain.SessionID, _ ports.SpawnConfig) error {
	if f.runtime.created != 1 || id != f.attached {
		return errors.New("no actual attached controller")
	}
	f.started = id
	return f.startError
}

func TestInstructorPostStartRecordFailureStopsControllerAndPreservesWorkspace(t *testing.T) {
	m, store, runtime, workspace := newManager()
	guard := &launchGovernanceFake{runtime: runtime, startError: errors.New("durable record failed")}
	m.SetInstructorGovernance(guard)
	if _, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode}); err == nil {
		t.Fatal("record failure hidden")
	}
	if runtime.created != 1 || runtime.destroyed != 1 || workspace.destroyed != 0 {
		t.Fatalf("runtime created=%d destroyed=%d workspace destroyed=%d", runtime.created, runtime.destroyed, workspace.destroyed)
	}
	if !store.sessions[guard.attached].IsTerminated || !guard.failed {
		t.Fatal("failed launch not durably stopped")
	}
}

func TestInstructorExplicitDelegationRequiresEnabledGovernance(t *testing.T) {
	for _, cfg := range []ports.SpawnConfig{
		{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode, WorkID: "work"},
		{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode, WorkRole: "triage"},
	} {
		manager, _, runtime, _ := newManager()
		if _, _, _, err := manager.Spawn(context.Background(), cfg); !errors.Is(err, ErrInstructorUnavailable) {
			t.Fatalf("spawn err=%v", err)
		}
		if runtime.created != 0 {
			t.Fatalf("launched without governance: %d", runtime.created)
		}
	}
}

func TestInstructorRegistersBeforeControllerAndRecordsActualStart(t *testing.T) {
	m, _, runtime, _ := newManager()
	guard := &launchGovernanceFake{runtime: runtime}
	m.SetInstructorGovernance(guard)
	record, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode, Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if guard.attached != record.ID || guard.started != record.ID || guard.failed {
		t.Fatalf("governance = %#v", guard)
	}
}

func TestInstructorRejectedLaunchNeverCreatesRuntime(t *testing.T) {
	for _, boundary := range []string{"prepare", "attach"} {
		t.Run(boundary, func(t *testing.T) {
			m, store, runtime, _ := newManager()
			guard := &launchGovernanceFake{runtime: runtime}
			if boundary == "prepare" {
				guard.prepareError = errors.New("missing native mapping")
			} else {
				guard.attachError = errors.New("association failure")
			}
			m.SetInstructorGovernance(guard)
			if _, _, _, err := m.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode}); err == nil {
				t.Fatal("launch accepted")
			}
			if runtime.created != 0 || len(store.sessions) != 0 {
				t.Fatalf("runtime=%d sessions=%d", runtime.created, len(store.sessions))
			}
		})
	}
}

func TestInstructorResumeGateRunsBeforeManagerMutation(t *testing.T) {
	m, store, runtime, _ := newManager()
	guard := &launchGovernanceFake{runtime: runtime, resumeError: errors.New("human gate is open")}
	m.SetInstructorGovernance(guard)
	for _, resume := range []func(context.Context, domain.SessionID) (RestoreResult, error){m.RestoreWithMode, m.ResumeAgentWithMode} {
		if _, err := resume(context.Background(), "missing"); !errors.Is(err, guard.resumeError) {
			t.Fatalf("error=%v", err)
		}
	}
	if runtime.created != 0 || len(store.sessions) != 0 {
		t.Fatal("recovery mutated before governance")
	}
}

func TestInstructorNativeMaximumIsPerModelAndRequiresFreshCapability(t *testing.T) {
	role := domain.InstructorRole{Harness: "opencode", Effort: "maximum", EffortByModel: map[string]string{"provider/model": "ultra"}}
	for _, test := range []struct {
		name, effort, support string
		stale                 bool
		wantError             bool
	}{
		{"native maximum", "ultra", "supported", false, false},
		{"universal max rejected", "max", "supported", false, true},
		{"stale rejected", "ultra", "supported", true, true},
		{"unknown rejected", "ultra", "unknown", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := &Manager{modelCatalog: tuningCatalog{catalog: ports.AgentModelCatalog{Stale: test.stale, Models: []ports.AgentModelInfo{{ID: "provider/model", EffortSupport: test.support, Efforts: []string{"low", "ultra"}}}}}}
			err := m.ValidateInstructorAgentConfig(context.Background(), "mer", role, "opencode", "provider/model", test.effort)
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

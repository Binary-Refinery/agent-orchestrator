package sessionmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestGovernanceSeedDisablesNativeLanesBeforeLaunch(t *testing.T) {
	rec := seedRecord(ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker}, domain.ProjectConfig{GovernanceManaged: true}, time.Now())
	if rec.AutoReviewEnabled || rec.AutoInjectReview || rec.AutoInjectCI {
		t.Fatal("unsafe initial lane flags")
	}
	legacy := seedRecord(ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker}, domain.ProjectConfig{}, time.Now())
	if !legacy.AutoInjectReview || !legacy.AutoInjectCI {
		t.Fatal("legacy defaults changed")
	}
}

func TestGovernanceNativeRequirementDoesNotConstructFallback(t *testing.T) {
	agent := &recordingAgent{}
	_, _, _, err := restoreArgv(domain.WithNativeHistoryRequired(context.Background()), agent, "mer-1", t.TempDir(),
		domain.SessionMetadata{Prompt: "original task"}, "rules", "", ports.AgentConfig{}, domain.KindWorker,
		domain.HarnessCodex, t.TempDir(), nil)
	if !errors.Is(err, ErrNotResumable) {
		t.Fatalf("want not resumable, got %v", err)
	}
	if agent.launchCalls != 0 {
		t.Fatal("fallback launch command was constructed")
	}
}

func TestGovernanceManagedResumePreservesExitedStateWithoutHistory(t *testing.T) {
	rt := &fakeRuntime{}
	m, st, _ := newExitedResumeManager(t, rt, fakeAgent{})
	project := st.projects["mer"]
	project.Config.GovernanceManaged = true
	st.projects["mer"] = project
	rec := st.sessions["mer-1"]
	rec.Metadata.AgentSessionID = ""
	st.sessions["mer-1"] = rec
	_, err := m.ResumeAgentWithMode(context.Background(), "mer-1")
	if !errors.Is(err, ErrNotResumable) {
		t.Fatalf("want not resumable, got %v", err)
	}
	if rt.created != 0 || st.sessions["mer-1"].Activity.State != domain.ActivityExited {
		t.Fatal("fallback mutated active runtime")
	}
}

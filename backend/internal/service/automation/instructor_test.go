package automation

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type originAutomationStore struct {
	*fakeStore
	project domain.ProjectID
	source  string
	ref     string
	work    domain.InstructorWork
}

func (s *originAutomationStore) InstructorWorkByOrigin(_ context.Context, project domain.ProjectID, source, ref string) (domain.InstructorWork, bool, error) {
	s.project, s.source, s.ref = project, source, ref
	return s.work, s.work.ID != "", nil
}

func (s *originAutomationStore) ListAutomationRuns(context.Context, domain.AutomationRunFilter) (domain.AutomationRunPage, error) {
	return domain.AutomationRunPage{Items: []domain.AutomationRun{{ID: "run", AutomationID: "automation"}}}, nil
}

func TestAutomationInstructorWorkLinkUsesDurableOriginWithoutSession(t *testing.T) {
	store := &originAutomationStore{fakeStore: newFakeStore(), work: domain.InstructorWork{ID: "work", ProjectID: "scheduled", Source: "automation", SourceRef: "run"}}
	store.automations["automation"] = domain.Automation{ID: "automation", ProjectID: "scheduled"}
	store.latestRuns["automation"] = domain.AutomationRun{ID: "run", AutomationID: "automation"}
	svc := New(Deps{Store: store})
	rec, err := svc.Get(context.Background(), "automation")
	if err != nil || rec.LatestRun == nil || rec.LatestRun.WorkID != "work" || rec.LatestRun.SessionID != nil {
		t.Fatalf("get=%+v err=%v", rec, err)
	}
	page, err := svc.List(context.Background(), domain.AutomationFilter{})
	if err != nil || page.Items[0].LatestRun.WorkID != "work" {
		t.Fatalf("list=%+v err=%v", page, err)
	}
	runs, err := svc.Runs(context.Background(), domain.AutomationRunFilter{AutomationID: "automation"})
	if err != nil || runs.Items[0].WorkID != "work" || store.project != "scheduled" || store.source != "automation" || store.ref != "run" {
		t.Fatalf("runs=%+v err=%v origin=%s/%s/%s", runs, err, store.project, store.source, store.ref)
	}
	store.work.ProjectID = "different-project"
	runs, err = svc.Runs(context.Background(), domain.AutomationRunFilter{AutomationID: "automation"})
	if err != nil || runs.Items[0].WorkID != "" {
		t.Fatalf("claimed cross-project work=%+v err=%v", runs, err)
	}
}

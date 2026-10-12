package instructor_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestInstructorPilotNoProgressNudgesOnceAcrossRestartThenEscalates(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "pilot-stall", instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, work.ID)
	started := f.now
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.State, w.Phase, w.ProgressAt = "running", "implement", started
	})
	f.now = started.Add(time.Duration(work.Policy.NudgeAfterSeconds) * time.Second)
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.NudgedAt == nil || !details.Work.NudgedAt.Equal(f.now) {
		t.Fatalf("missing bounded first nudge: %+v err=%v", details.Work, err)
	}
	nudgedAt := *details.Work.NudgedAt
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store, err = sqlite.Open(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.rebuildService()
	f.now = f.now.Add(time.Minute)
	if _, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "heartbeat", Summary: "still connected"}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	dispatches, err := f.store.ListInstructorDispatches(context.Background(), "", work.ID)
	if err != nil || len(dispatches) != 1 || dispatches[0].Action != "pruefen" || len(f.delivery.calls) != 1 {
		t.Fatalf("restart or heartbeat repeated the nudge: %+v deliveries=%d err=%v", dispatches, len(f.delivery.calls), err)
	}
	f.now = started.Add(time.Duration(work.Policy.EscalateAfterSeconds) * time.Second)
	if _, err := f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "heartbeat", Summary: "available without progress"}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	details, err = f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.State != "blocked" || !details.Work.ProgressAt.Equal(started) || details.Work.NudgedAt == nil || !details.Work.NudgedAt.Equal(nudgedAt) || details.Work.Heartbeat == nil || !details.Work.Heartbeat.At.Equal(f.now) {
		t.Fatalf("fresh presence prevented visible escalation or changed progress: %+v err=%v", details.Work, err)
	}
	counts := map[string]int{}
	for _, event := range details.Events {
		counts[event.Kind]++
	}
	if counts["nudge"] != 1 || counts["escalated"] != 1 {
		t.Fatalf("watchdog audit lost bounded sequence: %v", counts)
	}
}

func TestInstructorPilotExternalWaitRetainsItsDeadline(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "pilot-external-wait", instructorsvc.DefaultPolicy())
	deadline := f.now.Add(time.Hour)
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
		w.State, w.Phase, w.ProgressAt = "waiting_external", "implement", f.now.Add(-time.Hour)
		w.WaitCondition, w.WaitUntil = "external CI run", &deadline
	})
	f.now = deadline.Add(-time.Second)
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	details, err := f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.State != "waiting_external" || details.Work.NudgedAt != nil || len(details.Dispatches) != 0 || details.Work.WaitUntil == nil || !details.Work.WaitUntil.Equal(deadline) {
		t.Fatalf("legitimate external wait was disturbed: %+v err=%v", details.Work, err)
	}
	f.now = deadline
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	details, err = f.service.Details(context.Background(), work.ID)
	if err != nil || details.Work.NudgedAt == nil || len(details.Dispatches) != 1 {
		t.Fatalf("expired external wait stayed invisible: %+v err=%v", details.Work, err)
	}
}

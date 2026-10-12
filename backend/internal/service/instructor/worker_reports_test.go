package instructor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

func managedReportFixture(t *testing.T) (*instructorFixture, domain.InstructorWork, domain.SessionRecord, domain.SessionRecord) {
	t.Helper()
	ctx := context.Background()
	f := newInstructorFixture(t)
	w := f.register(t, "handoff", instructorsvc.DefaultPolicy())
	id := f.attachWorker(t, w.ID)
	worker, _, err := f.store.GetSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	worker.Metadata.RuntimeLaunchID = "worker-runtime"
	if err := f.store.UpdateSession(ctx, worker); err != nil {
		t.Fatal(err)
	}
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) { w.Phase = "implement"; w.PhaseSequence = 1; w.State = "running" })
	orch, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: w.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, Activity: domain.Activity{State: domain.ActivityIdle}, Metadata: domain.SessionMetadata{ControllerGeneration: "orchestrator-runtime"}, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	return f, w, worker, orch
}

func createManagedDone(t *testing.T, f *instructorFixture, worker domain.SessionRecord) domain.ReportRecord {
	t.Helper()
	svc := reportsvc.New(reportsvc.Deps{Store: f.store, ManagedDone: f.service, Now: func() time.Time { return f.now }, NewID: func() string { return "rpt_managed" }})
	r, err := svc.Create(context.Background(), reportsvc.CreateInput{SessionID: worker.ID, State: domain.ReportDone, Note: "implementation ready for checks"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestManagedDoneReportAtomicallyOwnsDurableWakeupAcrossRestart(t *testing.T) {
	ctx := context.Background()
	f, w, worker, _ := managedReportFixture(t)
	r := createManagedDone(t, f, worker)
	if r.DeliveryState != domain.ReportClaimed || r.DeliveryBatchID != "instructor-report:"+r.ID {
		t.Fatalf("outbox ownership=%+v", r)
	}
	ds, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil || len(ds) != 1 {
		t.Fatalf("dispatch=%+v err=%v", ds, err)
	}
	d := ds[0]
	if d.ReportID != r.ID || d.WorkID != w.ID || d.Phase != "implement" || d.PhaseSequence != 1 || d.ProducerSessionID != worker.ID || d.ProducerRuntimeID != "worker-runtime" {
		t.Fatalf("uncorrelated=%+v", d)
	}
	if _, err := f.store.RequeueClaimedReports(ctx); err != nil {
		t.Fatal(err)
	}
	if pending, err := f.store.ListPendingReportSchedule(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("generic coordinator would double send: %+v %v", pending, err)
	}
	f.rebuildService()
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.delivery.calls) != 1 || f.delivery.calls[0].ID != d.ID {
		t.Fatalf("restart lost intent: %+v", f.delivery.calls)
	}
	current, _, err := f.store.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State != "waiting_external" || current.Phase != "implement" {
		t.Fatalf("send fabricated progress: %+v %v", current, err)
	}
	f.now = f.now.Add(121 * time.Second)
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, _ = f.store.GetInstructorWork(ctx, w.ID)
	if current.Gate == nil || current.Gate.Kind != "delivery_uncertain" || len(f.delivery.calls) != 1 {
		t.Fatalf("missing native start must fence without repeat: %+v calls=%d", current, len(f.delivery.calls))
	}
}

func TestManagedWorkerResultNativeTurnReceiptRejectsUnrelatedAndSurvivesLateReceipt(t *testing.T) {
	ctx := context.Background()
	f, w, worker, orch := managedReportFixture(t)
	r := createManagedDone(t, f, worker)
	ds, _ := f.store.ListInstructorDispatches(ctx, "", w.ID)
	d := ds[0]
	if err := f.service.BindDispatchTarget(ctx, d.ID, orch.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.BindDispatchRuntime(ctx, d.ID, orch); err != nil {
		t.Fatal(err)
	}
	conv, err := f.store.CreateConversation(ctx, "orchestrator-conversation", domain.ConversationScopeProject, w.ProjectID, orch.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AppendUserMessage(ctx, conv.ID, orch.ID, "orchestrator-runtime", domain.ConversationMessage{ID: "wake-message", Text: d.Message, ClientMessageID: d.ID, Origin: domain.MessageOriginAutomation}, "wake-turn", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.BindTurnToProvider(ctx, "wake-turn", "provider-wake-turn", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordNativeSemanticStart(ctx, conv.ID, "unrelated-turn", "chat:orchestrator-runtime", "turn.started", f.now); err != nil {
		t.Fatal(err)
	}
	ds, _ = f.store.ListInstructorDispatches(ctx, "", w.ID)
	if ds[0].NativeReceipt.StartedAt != nil {
		t.Fatal("unrelated turn certified wake-up")
	}
	f.now = f.now.Add(121 * time.Second)
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	_, rollbackErr := f.store.ProjectProviderEvent(ctx, conv.ID, orch.ID, "orchestrator-runtime", "rolled-back-start", "turn.started", "{}", f.now, func(txCtx context.Context) error {
		if err := f.store.RecordNativeSemanticStart(txCtx, conv.ID, "provider-wake-turn", "chat:orchestrator-runtime", "turn.started", f.now); err != nil {
			return err
		}
		return errors.New("projection rollback fixture")
	})
	if rollbackErr == nil {
		t.Fatal("fixture didn't roll back")
	}
	ds, _ = f.store.ListInstructorDispatches(ctx, "", w.ID)
	storedBefore, _, _ := f.store.GetReport(ctx, r.ID)
	if ds[0].NativeReceipt.StartedAt != nil || storedBefore.DeliveryState != domain.ReportClaimed {
		t.Fatal("rolled-back provider event published a false start")
	}
	// Receipt is written within the same fenced archive+projection transaction.
	if _, err := f.store.ProjectProviderEvent(ctx, conv.ID, orch.ID, "orchestrator-runtime", "native-start-event", "turn.started", "{}", f.now, func(txCtx context.Context) error {
		return f.store.RecordNativeSemanticStart(txCtx, conv.ID, "provider-wake-turn", "chat:orchestrator-runtime", "turn.started", f.now)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	ds, _ = f.store.ListInstructorDispatches(ctx, "", w.ID)
	if ds[0].State != "started" || ds[0].NativeReceipt.ProviderTurnID != "provider-wake-turn" || ds[0].NativeReceipt.ObserverRuntimeID != "chat:orchestrator-runtime" {
		t.Fatalf("missing durable actual native start: %+v", ds[0])
	}
	stored, _, err := f.store.GetReport(ctx, r.ID)
	if err != nil || stored.DeliveryState != domain.ReportAcknowledged {
		t.Fatalf("native receipt didn't settle exact report: %+v %v", stored, err)
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	if current.Gate != nil || current.State != "ready" || !current.ProgressAt.Equal(w.ProgressAt) {
		t.Fatalf("receipt granted business progress or retained deadlock: %+v", current)
	}
	if len(f.delivery.calls) != 1 {
		t.Fatal("late receipt resent effect")
	}
}

func TestManagedWakeupOnlyRetriesCertifiedPreSubmissionFailure(t *testing.T) {
	ctx := context.Background()
	f, _, worker, _ := managedReportFixture(t)
	createManagedDone(t, f, worker)
	f.delivery.err = fmtRetryable()
	for attempt := 0; attempt < 3; attempt++ {
		if err := f.service.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(6 * time.Second)
	}
	if len(f.delivery.calls) != 3 {
		t.Fatalf("bounded durable retries=%d", len(f.delivery.calls))
	}
	ds, _ := f.store.ListInstructorDispatches(ctx, "", "")
	if len(ds) != 1 || ds[0].State != "failed" || ds[0].Attempts != 3 {
		t.Fatalf("retry audit=%+v", ds)
	}
	if !strings.Contains(ds[0].Error, "before submission") {
		t.Fatalf("missing diagnostic=%+v", ds[0])
	}
	if !errors.Is(f.delivery.err, instructorsvc.ErrDeliveryRetryable) {
		t.Fatal("invalid fixture")
	}
}

func fmtRetryable() error { return instructorsvc.ErrDeliveryRetryable }

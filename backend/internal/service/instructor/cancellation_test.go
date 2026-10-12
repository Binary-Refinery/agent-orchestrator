package instructor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type cancellationDelivery struct {
	*deliveryRecorder
	settled          bool
	observationErr   error
	proof            domain.InstructorCancellationProof
	afterObservation func()
}

func (d *cancellationDelivery) ObserveCancellation(context.Context, domain.InstructorWork) (domain.InstructorCancellationProof, bool, error) {
	if d.afterObservation != nil {
		d.afterObservation()
	}
	return d.proof, d.settled, d.observationErr
}
func cancellationService(f *instructorFixture, d *cancellationDelivery) *instructorsvc.Service {
	return instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: d, Clock: func() time.Time { return f.now }})
}
func TestInstructorCancellationPersistsIntentAndRequiresPositiveNativeProof(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	w := f.register(t, "cancel work", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, w.ID)
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) { w.State = "running"; w.Phase = "implement" })
	instance, _, err := f.store.InstructorSessionInstance(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	d := &cancellationDelivery{deliveryRecorder: f.delivery, proof: domain.InstructorCancellationProof{Producers: []domain.InstructorReportAdmission{{WorkID: w.ID, SessionID: sid, InstanceID: instance, Member: true}}}}
	f.service = cancellationService(f, d)
	snap := f.snapshot(t)
	input := instructorsvc.CommandInput{Command: "cancel cancel work", SnapshotID: snap.ID, IdempotencyKey: "cancel work 1"}
	result, err := f.service.Command(ctx, input)
	if err != nil || len(result.Dispatches) != 1 || result.Dispatches[0].Action != "abbrechen" {
		t.Fatalf("cancel: %+v %v", result, err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err := f.service.Details(ctx, w.ID)
	if err != nil || pending.Work.State != "parked" || pending.Work.Cancellation == nil || pending.Work.Cancellation.CompletedAt != nil {
		t.Fatalf("interrupt was falsely terminal: %+v %v", pending, err)
	}
	if !slices.Equal(pending.Work.Actions, []string{"details", "abbrechen"}) {
		t.Fatalf("cancelling work advertises resume: %v", pending.Work.Actions)
	}
	for _, cmd := range []string{"Weiter", "Go", "Stopp", "Prüfen"} {
		_, err = f.service.Command(ctx, instructorsvc.CommandInput{Command: cmd, WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "forbidden-" + cmd})
		if err == nil {
			t.Fatalf("%s reopened cancellation", cmd)
		}
	}
	for _, kind := range []string{"done", "progress", "needs_input", "waiting", "stuck"} {
		if _, err := f.service.Report(ctx, instructorsvc.ReportInput{WorkID: w.ID, SessionID: sid, Kind: kind}); err == nil {
			t.Fatalf("late %s reopened cancellation", kind)
		}
	}
	if _, err := f.service.ReserveBudget(ctx, w.ID, "turn", "after-cancel", sid, "", false); err == nil {
		t.Fatal("cancellation admitted new native turn")
	}
	f.now = time.Date(2026, 10, 12, 0, 1, 0, 0, time.UTC)
	f.service = cancellationService(f, d)
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err = f.service.Details(ctx, w.ID)
	if err != nil || pending.Work.Gate == nil || pending.Work.Gate.Kind != "cancellation_pending" || !strings.Contains(pending.Work.WaitCondition, "TUI-Interrupt") {
		t.Fatalf("unknown cancellation was silently parked after restart: %+v %v", pending, err)
	}
	d.settled = true
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	closed, err := f.service.Details(ctx, w.ID)
	if err != nil || closed.Work.State != "cancelled" || closed.Work.Cancellation.CompletedAt == nil || !slices.Equal(closed.Work.Actions, []string{"details"}) {
		t.Fatalf("positive proof failed to cancel: %+v %v", closed, err)
	}
	calls := len(f.delivery.calls)
	retry, err := f.service.Command(ctx, input)
	if err != nil || retry.BatchID != result.BatchID {
		t.Fatalf("idempotent cancellation: %+v %v", retry, err)
	}
	if err := f.service.Tick(ctx); err != nil || len(f.delivery.calls) != calls {
		t.Fatalf("restart/retry duplicated control: calls=%d old=%d %v", len(f.delivery.calls), calls, err)
	}
	if _, found, err := f.store.GetSession(ctx, sid); err != nil || !found {
		t.Fatalf("cancel deleted worker: %v %v", found, err)
	}
	events, err := f.store.ListInstructorEvents(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"operator_cancel_requested", "operator_cancel_awaiting_native", "operator_cancel_completed"} {
		if !slices.ContainsFunc(events, func(e domain.InstructorEvent) bool { return e.Kind == kind }) {
			t.Errorf("missing audit %s", kind)
		}
	}
}

func TestInstructorCancellationFrozenCohortIsAtomic(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	a, b := f.register(t, "cancel one", instructorsvc.DefaultPolicy()), f.register(t, "cancel two", instructorsvc.DefaultPolicy())
	snap := f.snapshot(t)
	f.trustedFacts(t, b.ID, func(w *domain.InstructorWork) { w.Summary = "changed" })
	_, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", SnapshotID: snap.ID, WorkIDs: []string{a.ID, b.ID}, IdempotencyKey: "stale-cancel-cohort"})
	if !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("stale cohort: %v", err)
	}
	current, _, err := f.store.GetInstructorWork(ctx, a.ID)
	if err != nil || current.Cancellation != nil {
		t.Fatalf("partial cancellation committed: %+v %v", current, err)
	}
	rows, err := f.store.ListInstructorDispatches(ctx, "", a.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("partial outbox: %+v %v", rows, err)
	}
}

func TestInstructorCancellationEmptyWorkCanFinishAndUncertaintyCannot(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "uncertain"}[fail], func(t *testing.T) {
			f := newInstructorFixture(t)
			ctx := context.Background()
			w := f.register(t, "empty cancel", instructorsvc.DefaultPolicy())
			d := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true}
			if fail {
				d.settled = false
				d.err = instructorsvc.ErrDeliveryUncertain
			}
			f.service = cancellationService(f, d)
			_, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "empty cancel"})
			if err != nil {
				t.Fatal(err)
			}
			if err := f.service.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			current, _, err := f.store.GetInstructorWork(ctx, w.ID)
			if err != nil || (current.State == "cancelled") == fail {
				t.Fatalf("terminal cancellation lost native uncertainty: %+v %v", current, err)
			}
		})
	}
}

func TestInstructorCancellationProofCannotCommitReplacedIncarnation(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	w := f.register(t, "replace cancel", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, w.ID)
	rec, instance, _, err := f.store.InstructorSessionForMembership(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	d := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true, proof: domain.InstructorCancellationProof{Producers: []domain.InstructorReportAdmission{{WorkID: w.ID, SessionID: sid, InstanceID: instance, Member: true}}}}
	f.service = cancellationService(f, d)
	_, err = f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "replace cancellation"})
	if err != nil {
		t.Fatal(err)
	}
	d.afterObservation = func() {
		d.afterObservation = nil
		if _, err := f.store.DeleteSession(ctx, sid); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.CreateSession(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.store.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State == "cancelled" || current.Cancellation.CompletedAt != nil {
		t.Fatalf("replacement inherited cancellation proof: %+v %v", current, err)
	}
}

func TestInstructorCancellationLateMechanicalAuditCannotResumeWork(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	path, sha := committedWorkspace(t)
	w := f.register(t, "cancel review", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, w.ID)
	f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State = "ready"
		w.Phase = "review"
		w.PhaseSequence = 2
		w.SHA = sha
	})
	instance, _, err := f.store.InstructorSessionInstance(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	d := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true, proof: domain.InstructorCancellationProof{Producers: []domain.InstructorReportAdmission{{WorkID: w.ID, SessionID: sid, InstanceID: instance, Member: true}}}}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: d, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	reviews := mechanicalReview{started: make(chan domain.InstructorWork, 1), release: make(chan struct{})}
	f.service.SetPhaseActions(reviews)
	defer func() { _ = f.service.Shutdown(ctx) }()
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reviews.started:
	case <-time.After(time.Second):
		t.Fatal("native review did not start")
	}
	_, err = f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "cancel live review"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
		if len(current.PhaseActions) == 1 && current.PhaseActions[0].State != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel did not stop its mechanical context")
		}
		time.Sleep(time.Millisecond)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.store.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State != "cancelled" || current.Phase != "review" || current.Cancellation == nil || current.Cancellation.CompletedAt == nil {
		t.Fatalf("late action audit resumed cancellation: %+v %v", current, err)
	}
	rows, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil || len(rows) != 1 || rows[0].Action != "abbrechen" || rows[0].State != "delivered" {
		t.Fatalf("action audit lost restrictive cancel or started phase: %+v %v", rows, err)
	}
}

func TestInstructorCancellationNativeFailureSettlesOnlyAfterFreshPositiveQuiet(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	w := f.register(t, "cancel failed", instructorsvc.DefaultPolicy())
	d := &cancellationDelivery{deliveryRecorder: f.delivery}
	d.err = instructorsvc.ErrDeliveryUncertain
	f.service = cancellationService(f, d)
	_, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "cancel failed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	pending, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	if pending.State != "parked" || len(pending.UncertainDispatchIDs) != 1 {
		t.Fatalf("unknown native effect disappeared: %+v", pending)
	}
	d.settled = true
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	closed, _, err := f.store.GetInstructorWork(ctx, w.ID)
	if err != nil || closed.State != "cancelled" || len(closed.UncertainDispatchIDs) != 0 {
		t.Fatalf("positive quiet did not settle its own cancel uncertainty: %+v %v", closed, err)
	}
	rows, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil || len(rows) != 1 || rows[0].State != "failed" {
		t.Fatalf("completion erased failed control audit: %+v %v", rows, err)
	}
}

func TestInstructorCancellationConcurrentRetryAndLateLaunchFailurePreserveIntent(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	w := f.register(t, "concurrent cancel", instructorsvc.DefaultPolicy())
	sid := f.attachWorker(t, w.ID)
	input := instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "concurrent cancel"}
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { _, err := f.service.Command(ctx, input); results <- err }()
	}
	for i := 0; i < 8; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.service.SpawnFailed(ctx, w.ID, errors.New("native launch callback after cancel")); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RecordLaunch(ctx, w.ID, domain.InstructorLaunch{SessionID: sid, Role: "worker", Status: "launched"}); err == nil {
		t.Fatal("late native launch failed to request native cleanup")
	}
	current, _, err := f.store.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State != "parked" || current.Cancellation == nil || len(current.Launches) != 2 {
		t.Fatalf("late launch audit lost durable cancellation: %+v %v", current, err)
	}
	rows, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("concurrent cancellation duplicated native effect: %+v %v", rows, err)
	}
}

func TestInstructorCancellationCapacityReleasesOnlyAfterPositiveQuiet(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	policy := instructorsvc.DefaultPolicy()
	policy.Budgets = &domain.InstructorBudgets{MaxConcurrentSessions: 1, MaxSessions: 1}
	w := f.register(t, "cancel capacity", policy)
	sid := f.attachWorker(t, w.ID)
	instance, _, err := f.store.InstructorSessionInstance(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	d := &cancellationDelivery{deliveryRecorder: f.delivery, proof: domain.InstructorCancellationProof{Producers: []domain.InstructorReportAdmission{{WorkID: w.ID, SessionID: sid, InstanceID: instance, Member: true}}}}
	f.service = cancellationService(f, d)
	if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "cancel capacity"}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	usage, err := f.service.BudgetUsage(ctx, w.ID)
	if err != nil || usage.ConcurrentSessions != 1 || usage.Sessions != 1 {
		t.Fatalf("unknown native effect refunded budget: %+v %v", usage, err)
	}
	d.settled = true
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	usage, err = f.service.BudgetUsage(ctx, w.ID)
	if err != nil || usage.ConcurrentSessions != 0 || usage.Sessions != 1 {
		t.Fatalf("positive quiet erased lifetime or held capacity: %+v %v", usage, err)
	}
}

func TestInstructorCancellationWaitsForOwnedCoordinatorTurnWithoutStoppingSharedOrchestrator(t *testing.T) {
	ctx := context.Background()
	f, w, worker, orch := managedReportFixture(t)
	createManagedDone(t, f, worker)
	ds, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil || len(ds) != 1 {
		t.Fatalf("result dispatch: %+v %v", ds, err)
	}
	directive := ds[0]
	if err := f.service.BindDispatchTarget(ctx, directive.ID, orch.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.BindDispatchRuntime(ctx, directive.ID, orch); err != nil {
		t.Fatal(err)
	}
	conv, err := f.store.CreateConversation(ctx, "cancel-orchestrator-conversation", domain.ConversationScopeProject, w.ProjectID, orch.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AppendUserMessage(ctx, conv.ID, orch.ID, "orchestrator-runtime", domain.ConversationMessage{ID: "cancel-wake-message", Text: directive.Message, ClientMessageID: directive.ID, Origin: domain.MessageOriginAutomation}, "cancel-wake-turn", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.BindTurnToProvider(ctx, "cancel-wake-turn", "provider-cancel-wake-turn", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordNativeSemanticStart(ctx, conv.ID, "provider-cancel-wake-turn", "chat:orchestrator-runtime", "turn.started", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	instance, _, err := f.store.InstructorSessionInstance(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	orchInstance, _, err := f.store.InstructorSessionInstance(ctx, orch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ReserveBudget(ctx, w.ID, "session", "session:"+orchInstance, orch.ID, "", false); err != nil {
		t.Fatal(err)
	}
	d := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true, proof: domain.InstructorCancellationProof{Producers: []domain.InstructorReportAdmission{{WorkID: w.ID, SessionID: worker.ID, InstanceID: instance, RuntimeID: "worker-runtime", Member: true}}}}
	f.service = cancellationService(f, d)
	if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "cancel owned coordinator turn"}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.store.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State != "parked" || current.Cancellation.CompletedAt != nil {
		t.Fatalf("worker quiet erased active coordinator effect: %+v %v", current, err)
	}
	f.now = f.now.Add(121 * time.Second)
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	pending, _, err := f.store.GetInstructorWork(ctx, w.ID)
	if err != nil || pending.Gate == nil || pending.Gate.Kind != "cancellation_pending" {
		t.Fatalf("active coordinator wait was hidden: %+v %v", pending, err)
	}
	if err := f.store.SettleTurnByID(ctx, "cancel-wake-turn", domain.TurnStateCompleted, "native provider receipt", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err = f.store.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State != "cancelled" {
		t.Fatalf("terminal native coordinator receipt failed: %+v %v", current, err)
	}
	usage, err := f.service.BudgetUsage(ctx, w.ID)
	if err != nil || usage.ConcurrentSessions != 0 || usage.Sessions != 2 {
		t.Fatalf("coordinator work capacity: %+v %v", usage, err)
	}
	retained, found, err := f.store.GetSession(ctx, orch.ID)
	if err != nil || !found || retained.IsTerminated {
		t.Fatalf("cancellation terminated shared coordinator: %+v %v %v", retained, found, err)
	}
}

func TestInstructorCancellationCheckProcess(t *testing.T) {
	if len(os.Args) == 0 || !strings.HasSuffix(os.Args[len(os.Args)-1], "instructor-cancel-check-ready") {
		return
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("native process running"), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestInstructorCancellationInterruptsDirectAPIProfileAndRetainsNativeAudit(t *testing.T) {
	f := newInstructorFixture(t)
	ctx := context.Background()
	path, sha := committedWorkspace(t)
	marker := filepath.Join(t.TempDir(), "instructor-cancel-check-ready")
	policy := instructorsvc.DefaultPolicy()
	role := policy.Roles["worker"]
	role.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = role
	policy.Profiles["push"] = domain.InstructorProfile{Command: []string{os.Args[0], "-test.run=^TestInstructorCancellationCheckProcess$", marker}, TimeoutSeconds: 60}
	w := f.register(t, "direct API check cancel", policy)
	worker := f.attachWorker(t, w.ID)
	f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) { w.State = "running"; w.Phase = "implement"; w.SHA = sha })
	instance, _, err := f.store.InstructorSessionInstance(ctx, worker)
	if err != nil {
		t.Fatal(err)
	}
	d := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true, proof: domain.InstructorCancellationProof{Producers: []domain.InstructorReportAdmission{{WorkID: w.ID, SessionID: worker, InstanceID: instance, Member: true}}}}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: d, Workspace: fixedWorkspace(path), Clock: func() time.Time { return f.now }})
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	result := make(chan error, 1)
	go func() { _, err := f.service.RunChecks(runCtx, w.ID, worker, []string{"push"}); result <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("direct native check did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "cancel direct checks"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled profile falsely passed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("direct API check survived cancellation")
	}
	if err := f.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, _, err := f.store.GetInstructorWork(ctx, w.ID)
	if err != nil || current.State != "cancelled" || len(current.PhaseActions) != 1 || current.PhaseActions[0].State != "failed" {
		t.Fatalf("direct profile cancellation lost history: %+v %v", current, err)
	}
}

func TestInstructorCancellationRetiresBusinessGateAndKeepsPublicationAuditDebt(t *testing.T) {
	for _, kind := range []string{"human_approval", "publication_violation"} {
		t.Run(kind, func(t *testing.T) {
			f := newInstructorFixture(t)
			ctx := context.Background()
			w := f.register(t, "cancel gate "+kind, instructorsvc.DefaultPolicy())
			f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
				w.State = "waiting_human"
				w.Gate = &domain.InstructorGate{ID: "old-gate", Kind: kind, Title: "retained decision"}
			})
			f.service = cancellationService(f, &cancellationDelivery{deliveryRecorder: f.delivery, settled: true})
			if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "cancel gate " + kind}); err != nil {
				t.Fatal(err)
			}
			if err := f.service.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			current, _, err := f.store.GetInstructorWork(ctx, w.ID)
			if err != nil || current.State != "cancelled" || (current.Gate != nil) != (kind == "publication_violation") {
				t.Fatalf("cancel gate semantics: %+v %v", current, err)
			}
		})
	}
}

func TestInstructorCancellationCannotHideInFlightOrUncertainProtectedPublish(t *testing.T) {
	for _, state := range []string{"claimed", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			f := newInstructorFixture(t)
			ctx := context.Background()
			w := f.register(t, "cancel publication "+state, instructorsvc.DefaultPolicy())
			f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
				w.PublicationGrants = []domain.InstructorPublicationGrant{{ID: "publish-history", Execution: &domain.InstructorPublicationExecution{State: state}}}
			})
			f.service = cancellationService(f, &cancellationDelivery{deliveryRecorder: f.delivery, settled: true})
			if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "cancel publishing " + state}); err != nil {
				t.Fatal(err)
			}
			f.now = f.now.Add(121 * time.Second)
			if err := f.service.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			current, _, err := f.store.GetInstructorWork(ctx, w.ID)
			if err != nil || current.State != "parked" || current.Cancellation.CompletedAt != nil || !current.PublicationGrants[0].Revoked || current.Gate == nil || current.Gate.Kind != "cancellation_pending" {
				t.Fatalf("in-flight publication was erased by cancellation: %+v %v", current, err)
			}
		})
	}
}

// A bound target is not admission, but a reservation or previous lease makes
// absence of a provider turn insufficient to prove that no native send occurred.
func TestInstructorCancellationUndeliveredDirectiveRequiresAtomicNonAdmission(t *testing.T) {
	for _, scenario := range []string{"never-admitted", "leased", "reserved", "accepted"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			f, w, worker, orch := managedReportFixture(t)
			createManagedDone(t, f, worker)
			ds, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
			if err != nil || len(ds) != 1 {
				t.Fatalf("dispatch: %v %v", ds, err)
			}
			directive := ds[0]
			if err := f.service.BindDispatchTarget(ctx, directive.ID, orch.ID); err != nil {
				t.Fatal(err)
			}
			if err := f.service.BindDispatchRuntime(ctx, directive.ID, orch); err != nil {
				t.Fatal(err)
			}
			if scenario == "leased" {
				if _, found, err := f.store.ClaimInstructorDispatch(ctx, f.now); err != nil || !found {
					t.Fatalf("lease: %v %v", found, err)
				}
			}
			if scenario == "reserved" || scenario == "accepted" {
				if _, fresh, err := f.store.BeginSemanticDelivery(ctx, orch.ID, directive.ID, "chat:orchestrator-runtime"); err != nil || !fresh {
					t.Fatalf("reserve: %v %v", fresh, err)
				}
				if scenario == "accepted" {
					if err := f.store.AcceptSemanticDelivery(ctx, orch.ID, directive.ID, "chat:orchestrator-runtime"); err != nil {
						t.Fatal(err)
					}
				}
			}
			instance, _, err := f.store.InstructorSessionInstance(ctx, worker.ID)
			if err != nil {
				t.Fatal(err)
			}
			d := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true, proof: domain.InstructorCancellationProof{Producers: []domain.InstructorReportAdmission{{WorkID: w.ID, SessionID: worker.ID, InstanceID: instance, RuntimeID: "worker-runtime", Member: true}}}}
			f.service = cancellationService(f, d)
			if _, err := f.service.Command(ctx, instructorsvc.CommandInput{Command: "Abbrechen", WorkIDs: []string{w.ID}, SnapshotID: f.snapshot(t).ID, IdempotencyKey: "cancel non-admission"}); err != nil {
				t.Fatal(err)
			}
			if err := f.service.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			current, _, err := f.store.GetInstructorWork(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "never-admitted" {
				if current.State != "parked" || current.Cancellation.CompletedAt != nil {
					t.Fatalf("uncertain native admission hidden: %+v", current)
				}
				return
			}
			if current.State != "cancelled" {
				t.Fatalf("never admitted directive blocked cancellation: %+v", current)
			}
			receipt, fresh, err := f.store.BeginSemanticDelivery(ctx, orch.ID, directive.ID, "chat:orchestrator-runtime")
			if err != nil || fresh || receipt.State != "cancelled" {
				t.Fatalf("late send not fenced: %+v %v %v", receipt, fresh, err)
			}
			if err := f.store.AcceptSemanticDelivery(ctx, orch.ID, directive.ID, "chat:orchestrator-runtime"); err == nil {
				t.Fatal("late native acceptance bypassed cancellation fence")
			}
		})
	}
}

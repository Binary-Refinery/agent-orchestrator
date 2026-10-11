package instructor_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type planningCapabilityValidator struct {
	calls atomic.Int32
	deny  atomic.Bool
}

func (v *planningCapabilityValidator) ValidateInstructorAgentConfig(_ context.Context, project domain.ProjectID, role domain.InstructorRole, harness, model, effort string) error {
	v.calls.Add(1)
	if v.deny.Load() {
		return errors.New("native capability withdrawn")
	}
	if project != "" || role.Harness != harness || role.Model != model || role.EffortByModel[model] != effort {
		return errors.New("planning must use the explicitly configured global native role")
	}
	return nil
}

type planningObservation struct {
	job      domain.InstructorPlanJob
	snapshot domain.InstructorSnapshot
	history  []domain.InstructorPlanJob
}

type controlledPlanner struct {
	started   chan planningObservation
	release   chan struct{}
	output    domain.InstructorPlanOutput
	confirmed bool
	wrongSent bool
	calls     atomic.Int32
}

func (p *controlledPlanner) Run(ctx context.Context, job domain.InstructorPlanJob, snapshot domain.InstructorSnapshot, history []domain.InstructorPlanJob, observe func(domain.InstructorAgentSelection, *domain.InstructorAgentSelection) error) (domain.InstructorPlanOutput, error) {
	p.calls.Add(1)
	sent := job.Requested
	if p.wrongSent {
		sent.Effort = "minimal"
	}
	var confirmed *domain.InstructorAgentSelection
	if p.confirmed {
		confirmed = &sent
	}
	if err := observe(sent, confirmed); err != nil {
		return domain.InstructorPlanOutput{}, err
	}
	select {
	case p.started <- planningObservation{job, snapshot, history}:
	case <-ctx.Done():
		return domain.InstructorPlanOutput{}, ctx.Err()
	}
	if p.release != nil {
		select {
		case <-p.release:
		case <-ctx.Done():
			return domain.InstructorPlanOutput{}, ctx.Err()
		}
	}
	return p.output, nil
}

func newPlanningFixture(t *testing.T, planner *controlledPlanner) (*instructorFixture, *planningCapabilityValidator) {
	t.Helper()
	f := newInstructorFixture(t)
	validator := &planningCapabilityValidator{}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Planner: planner, RoleValidator: validator, Clock: func() time.Time { return f.now }})
	policy := instructorsvc.DefaultPolicy()
	policy.Roles["instructor"] = domain.InstructorRole{Harness: "codex", Model: "planner-model", Effort: "configured", EffortByModel: map[string]string{"planner-model": "high"}}
	if _, err := f.service.SetPolicy(context.Background(), policy); err != nil {
		t.Fatalf("configure native global role: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := f.service.Shutdown(ctx); err != nil {
			t.Errorf("join native planner before closing SQLite: %v", err)
		}
	})
	return f, validator
}

func planningInput(snapshot domain.InstructorSnapshot, key string) domain.InstructorPlanInput {
	return domain.InstructorPlanInput{Request: "Explain the portfolio and propose the next safe decisions", SnapshotID: snapshot.ID, IdempotencyKey: key, TimeoutSeconds: 30}
}

func awaitPlanningStart(t *testing.T, planner *controlledPlanner) planningObservation {
	t.Helper()
	select {
	case observed := <-planner.started:
		return observed
	case <-time.After(3 * time.Second):
		t.Fatal("queued native planning never started")
		return planningObservation{}
	}
}

func awaitPlanState(t *testing.T, f *instructorFixture, id, state string) domain.InstructorPlanJob {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for {
		job, err := f.service.GetPlan(context.Background(), id)
		if err != nil {
			t.Fatalf("read planning journal: %v", err)
		}
		if job.State == state {
			return job
		}
		select {
		case <-deadline.C:
			t.Fatalf("planning state = %q, want %q (error: %s)", job.State, state, job.Error)
		case <-poll.C:
		}
	}
}

func TestInstructorPlanningQueuesWithoutBlockingStatusAndRemainsAdvisory(t *testing.T) {
	planner := &controlledPlanner{started: make(chan planningObservation, 1), release: make(chan struct{})}
	f, validator := newPlanningFixture(t, planner)
	work := f.register(t, "strategic-work", instructorsvc.DefaultPolicy())
	work = f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) { w.State = "parked"; w.WaitCondition = "Operator decision" })
	snapshot := f.snapshot(t)
	planner.output = domain.InstructorPlanOutput{Summary: "One work can continue", Questions: []string{"Which outcome takes priority?"}, Proposals: []domain.InstructorProposal{{Command: "Weiter", WorkIDs: []string{work.ID}, Reason: "The observed work is ready"}}, Intake: []domain.InstructorPlanIntake{{ProjectID: "governed", Name: "Intake draft", Goal: "Clarify a possible next work"}}}
	job, err := f.service.Plan(context.Background(), planningInput(snapshot, "advisory"))
	if err != nil || job.State != "queued" || planner.calls.Load() != 0 || validator.calls.Load() != 1 {
		t.Fatalf("Plan performed inference or skipped admission: %+v err=%v calls=%d validation=%d", job, err, planner.calls.Load(), validator.calls.Load())
	}
	if err = f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	observed := awaitPlanningStart(t, planner)
	if observed.snapshot.ID != snapshot.ID || observed.snapshot.BaseID != "" || len(observed.snapshot.Works) != 1 || observed.snapshot.Works[0].ID != work.ID || len(observed.history) != 1 {
		t.Fatalf("native planning did not receive the frozen portfolio and durable dialogue: %+v", observed)
	}
	responsive := make(chan error, 1)
	go func() {
		_, statusErr := f.service.Snapshot(context.Background(), "")
		if statusErr == nil {
			_, statusErr = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Status"})
		}
		responsive <- statusErr
	}()
	select {
	case err = <-responsive:
		if err != nil {
			t.Fatalf("status during native inference: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("native planning blocked portfolio status")
	}
	close(planner.release)
	completed := awaitPlanState(t, f, job.ID, "completed")
	if completed.Output == nil || completed.Sent == nil || *completed.Sent != completed.Requested || completed.Confirmed != nil || validator.calls.Load() != 2 {
		t.Fatalf("durable advisory output/provenance = %+v, validations=%d", completed, validator.calls.Load())
	}
	works, err := f.store.ListInstructorWorks(context.Background())
	if err != nil || len(works) != 1 || works[0].Revision != work.Revision {
		t.Fatalf("advice or intake draft changed work inventory: %+v err=%v", works, err)
	}
	dispatches, err := f.store.ListInstructorDispatches(context.Background(), "", "")
	if err != nil || len(dispatches) != 0 || len(f.delivery.calls) != 0 {
		t.Fatalf("planner proposal executed without command admission: %+v err=%v", dispatches, err)
	}
	projects, err := f.store.ListProjects(context.Background())
	if err != nil || len(projects) != 1 {
		t.Fatalf("global planning created a fake project: %+v err=%v", projects, err)
	}
	sessions, err := f.store.ListSessions(context.Background(), "")
	if err != nil || len(sessions) != 0 {
		t.Fatalf("global planning created project sessions: %+v err=%v", sessions, err)
	}
}

func TestInstructorPlanningIdempotencyIsDurableAndSerializesNativeJobs(t *testing.T) {
	planner := &controlledPlanner{started: make(chan planningObservation, 2), release: make(chan struct{}), output: domain.InstructorPlanOutput{Summary: "Portfolio assessed"}, confirmed: true}
	f, _ := newPlanningFixture(t, planner)
	snapshot := f.snapshot(t)
	input := planningInput(snapshot, "stable-plan-key")
	var wg sync.WaitGroup
	results := make(chan domain.InstructorPlanJob, 8)
	errorsSeen := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := f.service.Plan(context.Background(), input)
			results <- job
			errorsSeen <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errorsSeen)
	var id string
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("concurrent identical admission: %v", err)
		}
	}
	for job := range results {
		if id == "" {
			id = job.ID
		}
		if job.ID != id {
			t.Fatal("one caller key created multiple native requests")
		}
	}
	changed := input
	changed.Request = "A different request"
	if _, err := f.service.Plan(context.Background(), changed); !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("reused key accepted a different request: %v", err)
	}
	second, err := f.service.Plan(context.Background(), planningInput(snapshot, "next-plan-key"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitPlanningStart(t, planner)
	if err = f.service.Tick(context.Background()); err != nil || planner.calls.Load() != 1 {
		t.Fatalf("overlapping global native requests: calls=%d err=%v", planner.calls.Load(), err)
	}
	close(planner.release)
	awaitPlanState(t, f, id, "completed")
	deadline := time.Now().Add(3 * time.Second)
	for planner.calls.Load() < 2 && time.Now().Before(deadline) {
		if err = f.service.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	awaitPlanningStart(t, planner)
	completed := awaitPlanState(t, f, second.ID, "completed")
	if completed.Confirmed == nil || *completed.Confirmed != completed.Requested {
		t.Fatalf("actual provider confirmation was lost: %+v", completed)
	}
	jobs, err := f.service.ListPlans(context.Background())
	if err != nil || len(jobs) != 2 {
		t.Fatalf("durable idempotency history = %+v err=%v", jobs, err)
	}
}

func TestInstructorPlanningRestartDoesNotRepeatUnknownNativeRequest(t *testing.T) {
	planner := &controlledPlanner{started: make(chan planningObservation, 1), output: domain.InstructorPlanOutput{Summary: "Safe advice"}}
	f, validator := newPlanningFixture(t, planner)
	input := planningInput(f.snapshot(t), "interrupted-native-key")
	job, err := f.service.Plan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, ok, claimErr := f.store.ClaimInstructorPlan(context.Background(), f.now); claimErr != nil || !ok || claimed.ID != job.ID {
		t.Fatalf("simulate crash after native admission: %+v claimed=%v err=%v", claimed, ok, claimErr)
	}
	if err = f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store, err = sqlite.OpenPreMigrated(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Planner: planner, RoleValidator: validator, Clock: func() time.Time { return f.now }})
	if err = f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	interrupted := awaitPlanState(t, f, job.ID, "interrupted")
	if !strings.Contains(interrupted.Error, "new request key") || planner.calls.Load() != 0 {
		t.Fatalf("restart repeated uncertain native work: %+v calls=%d", interrupted, planner.calls.Load())
	}
	repeated, err := f.service.Plan(context.Background(), input)
	if err != nil || repeated.State != "interrupted" || repeated.ID != job.ID {
		t.Fatalf("same key replayed interrupted request: %+v err=%v", repeated, err)
	}
}

func TestInstructorPlanningRevalidatesPolicyAndNativeCapabilitiesBeforeStart(t *testing.T) {
	for _, fault := range []string{"withdrawn-capability", "changed-policy", "delta-snapshot", "missing-native-role"} {
		t.Run(fault, func(t *testing.T) {
			planner := &controlledPlanner{started: make(chan planningObservation, 1), output: domain.InstructorPlanOutput{Summary: "Safe advice"}}
			f, validator := newPlanningFixture(t, planner)
			snapshot := f.snapshot(t)
			input := planningInput(snapshot, "reject-"+fault)
			if fault == "delta-snapshot" {
				delta, err := f.service.Snapshot(context.Background(), snapshot.ID)
				if err != nil {
					t.Fatal(err)
				}
				input.SnapshotID = delta.ID
			}
			if fault == "missing-native-role" {
				policy, err := f.service.GetPolicy(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				role := policy.Roles["instructor"]
				role.Model = ""
				policy.Roles["instructor"] = role
				if _, err = f.service.SetPolicy(context.Background(), policy); err != nil {
					t.Fatal(err)
				}
			}
			job, err := f.service.Plan(context.Background(), input)
			if fault == "delta-snapshot" || fault == "missing-native-role" {
				if err == nil || planner.calls.Load() != 0 {
					t.Fatalf("unsafe planning admission accepted: %+v err=%v", job, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fault == "withdrawn-capability" {
				validator.deny.Store(true)
			} else {
				policy, getErr := f.service.GetPolicy(context.Background())
				if getErr != nil {
					t.Fatal(getErr)
				}
				policy.NudgeAfterSeconds++
				if _, err = f.service.SetPolicy(context.Background(), policy); err != nil {
					t.Fatal(err)
				}
			}
			if err = f.service.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			failed := awaitPlanState(t, f, job.ID, "failed")
			if planner.calls.Load() != 0 || failed.Output != nil || failed.Sent != nil {
				t.Fatalf("changed authority reached native provider: %+v calls=%d", failed, planner.calls.Load())
			}
		})
	}
}

func TestInstructorPlanningRejectsAdvisoryOutputWithUnadmittedAuthority(t *testing.T) {
	for _, fault := range []string{"unknown-command", "untyped-selector", "unknown-work", "duplicate-work", "new-command", "unknown-project", "wrong-native-selection"} {
		t.Run(fault, func(t *testing.T) {
			planner := &controlledPlanner{started: make(chan planningObservation, 1), output: domain.InstructorPlanOutput{Summary: "Advice"}}
			f, _ := newPlanningFixture(t, planner)
			work := f.register(t, "bounded-advice", instructorsvc.DefaultPolicy())
			f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) { w.State = "parked"; w.WaitCondition = "Operator decision" })
			proposal := domain.InstructorProposal{Command: "Weiter", WorkIDs: []string{work.ID}, Reason: "Observed ready state"}
			switch fault {
			case "unknown-command":
				proposal.Command = "Merge"
			case "untyped-selector":
				proposal.Command = "Weiter " + work.Name
			case "unknown-work":
				proposal.WorkIDs = []string{"unobserved-work"}
			case "duplicate-work":
				proposal.WorkIDs = []string{work.ID, work.ID}
			case "new-command":
				proposal.Command = "Neu"
			case "unknown-project":
				planner.output.Intake = []domain.InstructorPlanIntake{{ProjectID: "invented-project", Name: "Draft", Goal: "New work"}}
			case "wrong-native-selection":
				planner.wrongSent = true
			}
			planner.output.Proposals = []domain.InstructorProposal{proposal}
			job, err := f.service.Plan(context.Background(), planningInput(f.snapshot(t), "bad-advice-"+fault))
			if err != nil {
				t.Fatal(err)
			}
			if err = f.service.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			failed := awaitPlanState(t, f, job.ID, "failed")
			if failed.Output != nil || failed.Error == "" {
				t.Fatalf("invalid authority became accepted advice: %+v", failed)
			}
			dispatches, readErr := f.store.ListInstructorDispatches(context.Background(), "", "")
			if readErr != nil || len(dispatches) != 0 {
				t.Fatalf("rejected advice executed work: %+v err=%v", dispatches, readErr)
			}
		})
	}
}

func TestInstructorPlanningShutdownCancelsAndJoinsDurableNativeJob(t *testing.T) {
	planner := &controlledPlanner{started: make(chan planningObservation, 1), release: make(chan struct{}), output: domain.InstructorPlanOutput{Summary: "Late advice"}}
	f, _ := newPlanningFixture(t, planner)
	job, err := f.service.Plan(context.Background(), planningInput(f.snapshot(t), "shutdown-native-plan"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitPlanningStart(t, planner)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = f.service.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown did not join native planner: %v", err)
	}
	interrupted := awaitPlanState(t, f, job.ID, "interrupted")
	if interrupted.Output != nil || interrupted.Sent == nil || interrupted.Confirmed != nil {
		t.Fatalf("shutdown lost native uncertainty or accepted late output: %+v", interrupted)
	}
	if err = f.service.Tick(context.Background()); err != nil || planner.calls.Load() != 1 {
		t.Fatalf("shutdown restarted native planning: calls=%d err=%v", planner.calls.Load(), err)
	}
}

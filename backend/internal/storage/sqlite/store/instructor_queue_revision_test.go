package store_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const nativeQueueWait = "Native Nachricht dauerhaft eingereiht; wartet auf bestätigten Empfang"

func TestInstructorQueueCarriesParallelDirectivesWithoutChangingTheirAuthority(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "queue-revisions")
	seedProject(t, s, "other-project")
	at := time.Date(2026, time.October, 14, 10, 0, 0, 0, time.UTC)
	w := domain.InstructorWork{ID: "queue-work", ProjectID: "queue-revisions", Goal: "review the frozen worker", State: "running", Phase: "review", PhaseSequence: 2, SHA: "frozen-sha", PolicyVersion: "frozen-policy", Revision: 1, CreatedAt: at, UpdatedAt: at}
	if _, err := s.CreateInstructorWork(ctx, w, domain.InstructorEvent{ID: "created", WorkID: w.ID, At: at}); err != nil {
		t.Fatal(err)
	}
	other := w
	other.ID = "other-work"
	other.SourceRef = "different-work-origin"
	other.Name = "Different work"
	if _, err := s.CreateInstructorWork(ctx, other, domain.InstructorEvent{ID: "other-created", WorkID: other.ID, At: at}); err != nil {
		t.Fatal(err)
	}
	makeDispatch := func(id, state string, revision int64) domain.InstructorDispatch {
		return domain.InstructorDispatch{ID: id, BatchID: id, WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: revision, State: state, Action: "weiter", Message: "frozen directive " + id, SnapshotID: "human-snapshot", CommandDigest: "digest-" + id, TargetSessionID: "orchestrator", TargetSessionInstanceID: "target-incarnation", TargetRuntimeID: "chat:target-generation", TargetCreatedAt: &at, Attempts: 3, CreatedAt: at, UpdatedAt: at}
	}
	stale := []domain.InstructorDispatch{makeDispatch("stale-pending", "pending", 1), makeDispatch("stale-leased", "leased", 1), makeDispatch("other-work-pending", "pending", 1)}
	stale[2].WorkID = other.ID
	if err := s.EnqueueInstructorDispatches(ctx, stale); err != nil {
		t.Fatal(err)
	}
	w.Revision = 2
	if saved, err := s.SaveInstructorWork(ctx, w, 1, domain.InstructorEvent{ID: "ordinary-change", WorkID: w.ID, At: at}); err != nil || !saved {
		t.Fatalf("seed older directives: saved=%v err=%v", saved, err)
	}
	phase := makeDispatch("phase-review", "pending", 2)
	wakeup := makeDispatch("worker-wakeup", "leased", 2)
	wakeup.Action, wakeup.ReportID, wakeup.Phase, wakeup.PhaseSequence, wakeup.PolicyVersion = "worker_result", "completion-claim", w.Phase, w.PhaseSequence, w.PolicyVersion
	rows := []domain.InstructorDispatch{phase, wakeup, makeDispatch("failed", "failed", 2), makeDispatch("cancelled", "cancelled", 2), makeDispatch("accepted", "accepted", 2), makeDispatch("progress", "progress", 2), makeDispatch("started", "pending", 2), makeDispatch("wrong-project", "pending", 2)}
	rows[len(rows)-1].ProjectID = "other-project"
	if err := s.EnqueueInstructorDispatches(ctx, rows); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInstructorDispatch(ctx, "started", "started", "", at); err != nil {
		t.Fatal(err)
	}
	rows[6].State = "started"
	rows = append(rows, stale...)
	snapshot := domain.InstructorSnapshot{ID: "human-snapshot", CreatedAt: at, Works: []domain.InstructorWork{w}}
	if err := s.PutInstructorSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	beforeSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Two independent obligations are outstanding together. Settling either
	// native FIFO entry must carry the other's exact frozen authority atomically.
	for i, id := range []string{phase.ID, wakeup.ID} {
		prior, found, err := s.GetInstructorWork(ctx, w.ID)
		if err != nil || !found {
			t.Fatalf("current work: found=%v err=%v", found, err)
		}
		dispatches, err := s.ListInstructorDispatches(ctx, "", w.ID)
		if err != nil {
			t.Fatal(err)
		}
		var dispatch domain.InstructorDispatch
		for _, item := range dispatches {
			if item.ID == id {
				dispatch = item
			}
		}
		queuedAt := at.Add(time.Duration(i+1) * time.Second)
		deadline := queuedAt.Add(2 * time.Minute)
		current := prior
		current.Revision++
		current.UpdatedAt, current.State, current.WaitCondition, current.WaitUntil = queuedAt, "waiting_external", nativeQueueWait, &deadline
		dispatch.State, dispatch.WorkRevision, dispatch.QueuedAt, dispatch.UpdatedAt = "pending", current.Revision, &queuedAt, queuedAt
		if err := s.QueueInstructorDispatch(ctx, dispatch, current, prior.Revision, domain.InstructorEvent{ID: "queued-" + id, WorkID: w.ID, Kind: "delivery_queued", At: queuedAt}, queuedAt.Add(5*time.Second)); err != nil {
			t.Fatal(err)
		}
		dispatches, err = s.ListInstructorDispatches(ctx, "", w.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range dispatches {
			if item.ID == phase.ID || item.ID == wakeup.ID {
				if item.WorkRevision != current.Revision {
					t.Errorf("parallel directive %s revision=%d want=%d", item.ID, item.WorkRevision, current.Revision)
				}
			} else {
				for _, original := range rows {
					if original.ID == item.ID && !reflect.DeepEqual(original, item) {
						t.Errorf("queue bookkeeping changed ineligible directive %s: before=%+v after=%+v", item.ID, original, item)
					}
				}
			}
		}
	}
	all, err := s.ListInstructorDispatches(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range all {
		if item.ID == phase.ID || item.ID == wakeup.ID {
			var original domain.InstructorDispatch
			for _, row := range rows {
				if row.ID == item.ID {
					original = row
				}
			}
			original.State, original.WorkRevision, original.UpdatedAt, original.QueuedAt = item.State, item.WorkRevision, item.UpdatedAt, item.QueuedAt
			if !reflect.DeepEqual(original, item) {
				t.Errorf("queue carry changed native/message/snapshot facts for %s", item.ID)
			}
		} else {
			for _, original := range rows {
				if original.ID == item.ID && !reflect.DeepEqual(original, item) {
					t.Errorf("ineligible directive changed: %s", item.ID)
				}
			}
		}
	}
	storedSnapshot, found, err := s.GetInstructorSnapshot(ctx, snapshot.ID)
	if err != nil || !found {
		t.Fatalf("snapshot readback: found=%v err=%v", found, err)
	}
	afterSnapshot, err := json.Marshal(storedSnapshot)
	if err != nil || string(beforeSnapshot) != string(afterSnapshot) {
		t.Fatalf("native queue changed immutable human snapshot: err=%v", err)
	}
}

func TestInstructorQueueCarriesOnlyUnchangedTransportBookkeeping(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		initialState, initialWait string
		change                    func(*domain.InstructorWork)
		carry, reject             bool
	}{
		{name: "ready native queue", initialState: "ready", carry: true},
		{name: "running native queue", initialState: "running", carry: true},
		{name: "existing exact native queue", initialState: "waiting_external", initialWait: nativeQueueWait, carry: true},
		{name: "derived actions only", initialState: "ready", carry: true, change: func(w *domain.InstructorWork) { w.Actions = []string{"details"} }},
		{name: "business wait", initialState: "waiting_external", initialWait: "deployment window"},
		{name: "native prefix is not exact proof", initialState: "waiting_external", initialWait: "Native Nachricht: deployment window"},
		{name: "ready retains business wait", initialState: "ready", initialWait: "deployment window"},
		{name: "parked cannot become executable", initialState: "parked"},
		{name: "human wait cannot become executable", initialState: "waiting_human"},
		{name: "blocked cannot become executable", initialState: "blocked"},
		{name: "done cannot become executable", initialState: "done"},
		{name: "cancelled cannot become executable", initialState: "cancelled"},
		{name: "goal", initialState: "ready", change: func(w *domain.InstructorWork) { w.Goal = "different objective" }},
		{name: "summary", initialState: "ready", change: func(w *domain.InstructorWork) { w.Summary = "different summary" }},
		{name: "next", initialState: "ready", change: func(w *domain.InstructorWork) { w.Next = "different command" }},
		{name: "progress", initialState: "ready", change: func(w *domain.InstructorWork) { w.ProgressAt = w.UpdatedAt }},
		{name: "phase", initialState: "ready", change: func(w *domain.InstructorWork) { w.Phase = "fix" }},
		{name: "phase sequence", initialState: "ready", change: func(w *domain.InstructorWork) { w.PhaseSequence++ }},
		{name: "commit", initialState: "ready", change: func(w *domain.InstructorWork) { w.SHA = "different-sha" }},
		{name: "branch", initialState: "ready", change: func(w *domain.InstructorWork) { w.Branch = "different-branch" }},
		{name: "policy version", initialState: "ready", change: func(w *domain.InstructorWork) { w.PolicyVersion = "different-policy" }},
		{name: "policy contents", initialState: "ready", change: func(w *domain.InstructorWork) { w.Policy.Version = "different-policy" }},
		{name: "budget", initialState: "ready", change: func(w *domain.InstructorWork) { w.Policy.Budgets = &domain.InstructorBudgets{MaxTurns: 99} }},
		{name: "evidence", initialState: "ready", change: func(w *domain.InstructorWork) { w.Evidence = []domain.InstructorEvidence{{ID: "new-evidence"}} }},
		{name: "producer binding", initialState: "ready", change: func(w *domain.InstructorWork) { w.SessionIDs = []domain.SessionID{"replacement"} }},
		{name: "launch binding", initialState: "ready", change: func(w *domain.InstructorWork) {
			w.Launches = []domain.InstructorLaunch{{SessionID: "replacement", Role: "worker"}}
		}},
		{name: "transition grant", initialState: "ready", change: func(w *domain.InstructorWork) { w.ApprovedTransitions = []string{"review->fix"} }},
		{name: "publication grant", initialState: "ready", change: func(w *domain.InstructorWork) {
			w.PublicationGrants = []domain.InstructorPublicationGrant{{ID: "new-grant"}}
		}},
		{name: "gate", initialState: "ready", change: func(w *domain.InstructorWork) { w.Gate = &domain.InstructorGate{ID: "new-gate", Kind: "human"} }},
		{name: "cancellation", initialState: "ready", change: func(w *domain.InstructorWork) { w.Cancellation = &domain.InstructorCancellation{DispatchID: "cancel"} }},
		{name: "phase action", initialState: "ready", change: func(w *domain.InstructorWork) {
			w.PhaseActions = []domain.InstructorPhaseAction{{ID: "new-action", State: "running"}}
		}},
		{name: "different queue wait", initialState: "ready", change: func(w *domain.InstructorWork) { w.WaitCondition = "Native Nachricht: fabricated proof" }},
		{name: "different queue deadline", initialState: "ready", change: func(w *domain.InstructorWork) { deadline := w.WaitUntil.Add(time.Second); w.WaitUntil = &deadline }},
		{name: "nonconsecutive revision", initialState: "ready", reject: true, change: func(w *domain.InstructorWork) { w.Revision++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			seedProject(t, s, "queue-matrix")
			at := time.Date(2026, time.October, 14, 11, 0, 0, 0, time.UTC)
			w := domain.InstructorWork{ID: "matrix-work", ProjectID: "queue-matrix", Goal: "unchanged goal", State: tc.initialState, WaitCondition: tc.initialWait, Phase: "review", PhaseSequence: 2, SHA: "frozen-sha", PolicyVersion: "frozen-policy", Revision: 1, CreatedAt: at, UpdatedAt: at}
			if tc.initialWait != "" {
				deadline := at.Add(time.Minute)
				w.WaitUntil = &deadline
			}
			if _, err := s.CreateInstructorWork(ctx, w, domain.InstructorEvent{ID: "created", WorkID: w.ID, At: at}); err != nil {
				t.Fatal(err)
			}
			primary := domain.InstructorDispatch{ID: "primary", BatchID: "primary", WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: 1, State: "leased", Action: "weiter", CreatedAt: at, UpdatedAt: at}
			sibling := primary
			sibling.ID, sibling.BatchID, sibling.State, sibling.Action = "sibling", "sibling", "pending", "worker_result"
			if err := s.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{primary, sibling}); err != nil {
				t.Fatal(err)
			}
			queuedAt := at.Add(time.Second)
			deadline := queuedAt.Add(2 * time.Minute)
			current := w
			current.Revision, current.UpdatedAt, current.State, current.WaitCondition, current.WaitUntil = 2, queuedAt, "waiting_external", nativeQueueWait, &deadline
			if tc.change != nil {
				tc.change(&current)
			}
			primary.State, primary.WorkRevision, primary.UpdatedAt, primary.QueuedAt = "pending", current.Revision, queuedAt, &queuedAt
			err := s.QueueInstructorDispatch(ctx, primary, current, 1, domain.InstructorEvent{ID: "queued", WorkID: w.ID, Kind: "delivery_queued", At: queuedAt}, queuedAt.Add(5*time.Second))
			if (err != nil) != tc.reject {
				t.Fatalf("queue transaction: error=%v reject=%v", err, tc.reject)
			}
			rows, err := s.ListInstructorDispatches(ctx, "", w.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.ID == sibling.ID {
					want := int64(1)
					if tc.carry {
						want = current.Revision
					}
					if row.WorkRevision != want {
						t.Errorf("sibling revision=%d want=%d", row.WorkRevision, want)
					}
				}
			}
			if tc.reject {
				stored, found, err := s.GetInstructorWork(ctx, w.ID)
				if err != nil || !found || !reflect.DeepEqual(w, stored) {
					t.Errorf("rejected queue changed work: work=%+v found=%v err=%v", stored, found, err)
				}
			}
		})
	}
}

func TestInstructorQueueRejectsStaleCaptureWithoutChangingSibling(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "queue-stale")
	at := time.Now().UTC()
	w := domain.InstructorWork{ID: "stale-work", ProjectID: "queue-stale", State: "ready", Revision: 1, CreatedAt: at, UpdatedAt: at}
	if _, err := s.CreateInstructorWork(ctx, w, domain.InstructorEvent{ID: "created", WorkID: w.ID, At: at}); err != nil {
		t.Fatal(err)
	}
	d := domain.InstructorDispatch{ID: "stale-primary", BatchID: "stale-primary", WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: 1, State: "leased", Action: "weiter", CreatedAt: at, UpdatedAt: at}
	if err := s.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{d}); err != nil {
		t.Fatal(err)
	}
	w.Revision = 2
	if saved, err := s.SaveInstructorWork(ctx, w, 1, domain.InstructorEvent{ID: "ordinary-change", WorkID: w.ID, At: at}); err != nil || !saved {
		t.Fatalf("advance current work: saved=%v err=%v", saved, err)
	}
	sibling := d
	sibling.ID, sibling.BatchID, sibling.WorkRevision = "current-sibling", "current-sibling", 2
	if err := s.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{sibling}); err != nil {
		t.Fatal(err)
	}
	current := w
	current.State, current.WaitCondition = "waiting_external", nativeQueueWait
	deadline := at.Add(2 * time.Minute)
	current.WaitUntil = &deadline
	d.State, d.WorkRevision, d.QueuedAt = "pending", 2, &at
	if err := s.QueueInstructorDispatch(ctx, d, current, 1, domain.InstructorEvent{ID: "stale-queue", WorkID: w.ID, At: at}, at.Add(5*time.Second)); err == nil {
		t.Fatal("stale work capture was accepted")
	}
	stored, found, err := s.GetInstructorWork(ctx, w.ID)
	if err != nil || !found || !reflect.DeepEqual(w, stored) {
		t.Fatalf("stale queue changed work: found=%v err=%v", found, err)
	}
	rows, err := s.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == sibling.ID && !reflect.DeepEqual(row, sibling) {
			t.Fatalf("stale queue changed sibling: %+v", row)
		}
	}
}

func TestInstructorQueueWorkerResultRetainsItsPersistedPhaseScopeAcrossCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*domain.InstructorDispatch)
		allow  bool
	}{
		{name: "same phase worker result", allow: true},
		{name: "changed phase", change: func(d *domain.InstructorDispatch) { d.Phase = "implement" }},
		{name: "changed phase sequence", change: func(d *domain.InstructorDispatch) { d.PhaseSequence++ }},
		{name: "changed policy", change: func(d *domain.InstructorDispatch) { d.PolicyVersion = "different-policy" }},
		{name: "operator directive remains revision fenced", change: func(d *domain.InstructorDispatch) { d.Action = "weiter" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			seedProject(t, s, "queue-worker-result")
			at := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
			w := domain.InstructorWork{ID: "worker-result-work", ProjectID: "queue-worker-result", Goal: "frozen worker result", State: "running", Phase: "review", PhaseSequence: 2, PolicyVersion: "frozen-policy", Revision: 1, CreatedAt: at, UpdatedAt: at}
			if _, err := s.CreateInstructorWork(ctx, w, domain.InstructorEvent{ID: "created", WorkID: w.ID, At: at}); err != nil {
				t.Fatal(err)
			}
			primary := domain.InstructorDispatch{ID: "worker-result", BatchID: "worker-result", WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: 1, Action: "worker_result", State: "leased", Phase: w.Phase, PhaseSequence: w.PhaseSequence, PolicyVersion: w.PolicyVersion, ReportID: "retained-report", ProducerSessionID: "worker", ProducerRuntimeID: "chat:worker-generation", TargetSessionID: "orchestrator", TargetSessionInstanceID: "target-incarnation", TargetRuntimeID: "chat:target-generation", Message: "frozen completion claim", SnapshotID: "unchanged-snapshot", Attempts: 1, CreatedAt: at, UpdatedAt: at}
			if tc.change != nil {
				tc.change(&primary)
			}
			stale := primary
			stale.ID, stale.BatchID, stale.Action, stale.State = "stale-sibling", "stale-sibling", "weiter", "pending"
			if err := s.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{primary, stale}); err != nil {
				t.Fatal(err)
			}
			w.Revision, w.UpdatedAt, w.Summary = 2, at.Add(time.Second), "ordinary checkpoint"
			if saved, err := s.SaveInstructorWork(ctx, w, 1, domain.InstructorEvent{ID: "checkpoint", WorkID: w.ID, At: w.UpdatedAt}); err != nil || !saved {
				t.Fatalf("checkpoint: saved=%v err=%v", saved, err)
			}
			currentSibling := stale
			currentSibling.ID, currentSibling.BatchID, currentSibling.WorkRevision = "current-sibling", "current-sibling", 2
			if err := s.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{currentSibling}); err != nil {
				t.Fatal(err)
			}
			beforeRows, err := s.ListInstructorDispatches(ctx, "", w.ID)
			if err != nil {
				t.Fatal(err)
			}
			queuedAt := at.Add(2 * time.Second)
			deadline := queuedAt.Add(2 * time.Minute)
			current := w
			current.Revision, current.UpdatedAt, current.State, current.WaitCondition, current.WaitUntil = 3, queuedAt, "waiting_external", nativeQueueWait, &deadline
			queued := primary
			queued.WorkRevision, queued.State, queued.QueuedAt, queued.UpdatedAt = 3, "pending", &queuedAt, queuedAt
			err = s.QueueInstructorDispatch(ctx, queued, current, 2, domain.InstructorEvent{ID: "queued", WorkID: w.ID, Kind: "delivery_queued", At: queuedAt}, queuedAt.Add(5*time.Second))
			if (err == nil) != tc.allow {
				t.Fatalf("persisted worker-result scope: allowed=%v err=%v", tc.allow, err)
			}
			afterRows, err := s.ListInstructorDispatches(ctx, "", w.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.allow {
				stored, found, err := s.GetInstructorWork(ctx, w.ID)
				if err != nil || !found || !reflect.DeepEqual(w, stored) || !reflect.DeepEqual(beforeRows, afterRows) {
					t.Fatalf("rejected phase/capture scope mutated state: work=%+v rows=%+v err=%v", stored, afterRows, err)
				}
				return
			}
			for _, row := range afterRows {
				want := int64(3)
				if row.ID == stale.ID {
					want = 1
				}
				if row.WorkRevision != want {
					t.Errorf("worker-result queue carried %s to revision %d, want %d", row.ID, row.WorkRevision, want)
				}
				if row.ID == primary.ID {
					primary.State, primary.WorkRevision, primary.UpdatedAt, primary.QueuedAt = row.State, row.WorkRevision, row.UpdatedAt, row.QueuedAt
					if !reflect.DeepEqual(primary, row) {
						t.Errorf("worker-result exception changed producer/runtime/message authority: %+v", row)
					}
				}
			}
		})
	}
}

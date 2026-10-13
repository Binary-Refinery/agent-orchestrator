package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestInstructorReceiptCarriesOnlyCurrentUnchangedAuthority(t *testing.T) {
	for _, tc := range []struct {
		name          string
		initialState  string
		initialWait   string
		receipt       string
		change        func(*domain.InstructorWork)
		carry         bool
		staleRevision bool
		wrongActor    bool
	}{
		{name: "accepted information", receipt: "accepted", carry: true},
		{name: "progress information", receipt: "progress", carry: true},
		{name: "ready to running", receipt: "progress", carry: true, change: func(w *domain.InstructorWork) { w.State = "running" }},
		{name: "running to ready", initialState: "running", receipt: "accepted", carry: true, change: func(w *domain.InstructorWork) { w.State = "ready" }},
		{name: "bound native queue receipt", initialState: "waiting_external", initialWait: "Native Nachricht wartet auf Annahme", receipt: "accepted", carry: true, change: func(w *domain.InstructorWork) { w.State, w.WaitCondition = "ready", "" }},
		{name: "external business wait", initialState: "waiting_external", initialWait: "Waiting for deployment window", receipt: "progress", change: func(w *domain.InstructorWork) { w.State, w.WaitCondition = "running", "" }},
		{name: "paused work cannot become executable", initialState: "parked", receipt: "progress", change: func(w *domain.InstructorWork) { w.State = "running" }},
		{name: "gate", receipt: "accepted", change: func(w *domain.InstructorWork) { w.Gate = &domain.InstructorGate{ID: "new-gate", Kind: "human"} }},
		{name: "phase", receipt: "progress", change: func(w *domain.InstructorWork) { w.Phase = "review" }},
		{name: "phase sequence", receipt: "progress", change: func(w *domain.InstructorWork) { w.PhaseSequence++ }},
		{name: "commit", receipt: "progress", change: func(w *domain.InstructorWork) { w.SHA = "new-commit" }},
		{name: "policy version", receipt: "accepted", change: func(w *domain.InstructorWork) { w.PolicyVersion = "new-policy" }},
		{name: "policy contents", receipt: "accepted", change: func(w *domain.InstructorWork) { w.Policy.Version = "new-policy" }},
		{name: "evidence", receipt: "progress", change: func(w *domain.InstructorWork) { w.Evidence = []domain.InstructorEvidence{{ID: "new-evidence"}} }},
		{name: "session binding", receipt: "accepted", change: func(w *domain.InstructorWork) { w.SessionIDs = []domain.SessionID{"another-session"} }},
		{name: "launch binding", receipt: "accepted", change: func(w *domain.InstructorWork) {
			w.Launches = []domain.InstructorLaunch{{SessionID: "another-session", Role: "worker"}}
		}},
		{name: "transition grant", receipt: "accepted", change: func(w *domain.InstructorWork) { w.ApprovedTransitions = []string{"review->fix"} }},
		{name: "publication grant", receipt: "accepted", change: func(w *domain.InstructorWork) {
			w.PublicationGrants = []domain.InstructorPublicationGrant{{ID: "new-grant"}}
		}},
		{name: "phase action", receipt: "progress", change: func(w *domain.InstructorWork) { w.PhaseActions = []domain.InstructorPhaseAction{{ID: "new-action"}} }},
		{name: "cancel intent", receipt: "accepted", change: func(w *domain.InstructorWork) { w.Cancellation = &domain.InstructorCancellation{DispatchID: "stop"} }},
		{name: "nonconsecutive revision", receipt: "accepted", change: func(w *domain.InstructorWork) { w.Revision++ }},
		{name: "stale work capture", receipt: "accepted", staleRevision: true},
		{name: "changed actor incarnation", receipt: "accepted", wrongActor: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := newTestStore(t)
			seedProject(t, store, "receipts")
			session, err := store.CreateSession(ctx, sampleRecord("receipts"))
			if err != nil {
				t.Fatal(err)
			}
			instance, found, err := store.InstructorSessionInstance(ctx, session.ID)
			if err != nil || !found {
				t.Fatalf("session incarnation: found=%v error=%v", found, err)
			}
			at := time.Date(2026, time.October, 13, 16, 0, 0, 0, time.UTC)
			state := tc.initialState
			if state == "" {
				state = "ready"
			}
			work := domain.InstructorWork{
				ID: "receipt-work", ProjectID: "receipts", Source: "manual", SourceRef: "receipt-test",
				State: state, Phase: "implement", PhaseSequence: 1, WaitCondition: tc.initialWait,
				Revision: 1, Summary: "before receipt", CreatedAt: at, UpdatedAt: at,
			}
			if _, err := store.CreateInstructorWork(ctx, work, domain.InstructorEvent{ID: "created", WorkID: work.ID, At: at}); err != nil {
				t.Fatal(err)
			}
			makeDispatch := func(id, dispatchState string, revision int64) domain.InstructorDispatch {
				return domain.InstructorDispatch{
					ID: id, BatchID: id, WorkID: work.ID, ProjectID: work.ProjectID, WorkRevision: revision,
					TargetSessionID: session.ID, TargetSessionInstanceID: instance,
					State: dispatchState, Action: "weiter", CreatedAt: at, UpdatedAt: at,
				}
			}
			oldDispatches := []domain.InstructorDispatch{
				makeDispatch("bound-receipt", "accepted", 1),
				makeDispatch("stale-pending", "pending", 1),
				makeDispatch("stale-leased", "leased", 1),
			}
			if err := store.EnqueueInstructorDispatches(ctx, oldDispatches); err != nil {
				t.Fatal(err)
			}
			work.Revision = 2
			if saved, err := store.SaveInstructorWork(ctx, work, 1, domain.InstructorEvent{ID: "ordinary-change", WorkID: work.ID, At: at}); err != nil || !saved {
				t.Fatalf("advance work before receipt: saved=%v err=%v", saved, err)
			}
			currentDispatches := []domain.InstructorDispatch{
				makeDispatch("current-pending", "pending", 2),
				makeDispatch("current-leased", "leased", 2),
				makeDispatch("terminal-failed", "failed", 2),
				makeDispatch("terminal-cancelled", "cancelled", 2),
			}
			if err := store.EnqueueInstructorDispatches(ctx, currentDispatches); err != nil {
				t.Fatal(err)
			}
			snapshot := domain.InstructorSnapshot{ID: "human-snapshot", CreatedAt: at, Works: []domain.InstructorWork{work}}
			if err := store.PutInstructorSnapshot(ctx, snapshot); err != nil {
				t.Fatal(err)
			}
			beforeSnapshot, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			admission, found, err := store.AdmitInstructorReport(ctx, work.ID, session.ID, "bound-receipt")
			if err != nil || !found {
				t.Fatalf("admit exact target: found=%v err=%v", found, err)
			}
			current := work
			current.Revision++
			current.UpdatedAt = at.Add(time.Second)
			current.Summary, current.Next = "task received", "inspect code"
			current.ProgressAt, current.NudgedAt = current.UpdatedAt, &at
			if tc.change != nil {
				tc.change(&current)
			}
			expected := work.Revision
			if tc.staleRevision {
				expected--
			}
			if tc.wrongActor {
				admission.InstanceID = "another-incarnation"
			}
			saved, err := store.SaveInstructorReport(ctx, current, expected, domain.InstructorEvent{ID: "receipt", WorkID: work.ID, Kind: tc.receipt, At: current.UpdatedAt}, admission, tc.receipt)
			if tc.wrongActor {
				if saved || !errors.Is(err, domain.ErrInstructorProducerChanged) {
					t.Fatalf("wrong actor changed work: saved=%v err=%v", saved, err)
				}
			} else if err != nil || saved == tc.staleRevision {
				t.Fatalf("receipt save: saved=%v err=%v", saved, err)
			}
			dispatches, err := store.ListInstructorDispatches(ctx, "", work.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, dispatch := range dispatches {
				want := int64(2)
				switch dispatch.ID {
				case "stale-pending", "stale-leased", "bound-receipt":
					want = 1
				case "current-pending", "current-leased":
					if tc.carry {
						want = current.Revision
					}
				}
				if dispatch.WorkRevision != want {
					t.Fatalf("dispatch %s revision=%d want=%d", dispatch.ID, dispatch.WorkRevision, want)
				}
			}
			storedSnapshot, found, err := store.GetInstructorSnapshot(ctx, snapshot.ID)
			if err != nil || !found {
				t.Fatalf("snapshot: found=%v err=%v", found, err)
			}
			afterSnapshot, err := json.Marshal(storedSnapshot)
			if err != nil || string(afterSnapshot) != string(beforeSnapshot) {
				t.Fatalf("receipt changed immutable human snapshot: error=%v", err)
			}
		})
	}
}

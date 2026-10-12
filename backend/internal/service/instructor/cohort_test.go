package instructor_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// Native queue admission and a later explicit rejection are distinct results.
// SQLite, work gates and controlled publisher admission are real in these tests.
type queuedCohortDelivery struct {
	calls    []domain.InstructorDispatch
	rejected bool
}

func (d *queuedCohortDelivery) Deliver(_ context.Context, item domain.InstructorDispatch) error {
	d.calls = append(d.calls, item)
	if item.Position == 1 {
		if d.rejected {
			return instructorsvc.ErrDeliveryRejected
		}
		return instructorsvc.ErrDeliveryQueued
	}
	return nil
}

func TestOrderedMixedBranchApprovalCohortStopsOnDelayedNativeFailure(t *testing.T) {
	f := newInstructorFixture(t)
	publication := &protectedPublicationFixture{}
	delivery := &queuedCohortDelivery{}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: delivery, Publication: publication, Clock: func() time.Time { return f.now }})
	works := []domain.InstructorWork{publicationReadyWork(t, f, "alpha"), publicationReadyWork(t, f, "beta"), publicationReadyWork(t, f, "gamma"), publicationReadyWork(t, f, "unselected")}
	snap := f.snapshot(t)
	// The explicit selected order differs from database/snapshot order.
	selected := []domain.InstructorWork{works[2], works[0], works[1]}
	in := instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, IdempotencyKey: "ordered-mixed-branches"}
	for _, w := range selected {
		in.WorkIDs = append(in.WorkIDs, w.ID)
		in.Approvals = append(in.Approvals, domain.InstructorWorkApproval{WorkID: w.ID, Approval: w.Gate.ExactApproval})
	}
	result, err := f.service.Command(context.Background(), in)
	if err != nil || len(result.Dispatches) != 3 {
		t.Fatalf("ordered admission: result=%+v err=%v", result, err)
	}
	for i, dispatch := range result.Dispatches {
		if dispatch.Position != i || dispatch.WorkID != selected[i].ID || dispatch.BatchID != result.BatchID {
			t.Fatalf("cohort lost captured order: %+v", result.Dispatches)
		}
	}
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.calls) != 2 {
		t.Fatalf("queued native item allowed a following submission: %+v", delivery.calls)
	}
	last, err := f.service.Details(context.Background(), selected[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	grant := last.Work.PublicationGrants[0]
	pubInput := instructorsvc.PublicationInput{WorkID: last.Work.ID, GrantID: grant.ID, SnapshotID: grant.SnapshotID, WorkRevision: last.Work.Revision, SHA: grant.SHA, Branch: grant.Branch, Operation: grant.Operation}
	if _, err := f.service.ExecutePublication(context.Background(), pubInput); err == nil || len(publication.requests) != 0 {
		t.Fatalf("undelivered cohort suffix published: %v", err)
	}
	// Reopen actual SQLite while the middle native directive remains queued.
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store, err = sqlite.OpenPreMigrated(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: delivery, Publication: publication, Clock: func() time.Time { return f.now }})
	delivery.rejected = true
	f.now = f.now.Add(6 * time.Second)
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.ListInstructorDispatches(context.Background(), result.BatchID, "")
	if err != nil || len(rows) != 3 || rows[0].State != "delivered" || rows[1].State != "failed" || rows[2].State != "cancelled" {
		t.Fatalf("delayed failure lost earlier receipt or failed to stop suffix: %+v err=%v", rows, err)
	}
	last, err = f.service.Details(context.Background(), selected[2].ID)
	if err != nil || last.Work.State != "waiting_human" || last.Work.Gate == nil || last.Work.Gate.ID != selected[2].Gate.ID || !last.Work.PublicationGrants[0].Revoked {
		t.Fatalf("cancelled suffix retained executable authority: %+v err=%v", last.Work, err)
	}
	pubInput.WorkRevision = last.Work.Revision
	if _, err := f.service.ExecutePublication(context.Background(), pubInput); err == nil || len(publication.requests) != 0 {
		t.Fatalf("cancelled publication grant executed: %v", err)
	}
	unselected, err := f.service.Details(context.Background(), works[3].ID)
	if err != nil || unselected.Work.Gate == nil || len(unselected.Work.PublicationGrants) != 0 {
		t.Fatalf("approval escaped selected snapshot subset: %+v err=%v", unselected.Work, err)
	}
	again, err := f.service.Command(context.Background(), in)
	if err != nil || again.BatchID != result.BatchID || len(again.Dispatches) != 3 || again.Dispatches[2].State != "cancelled" {
		t.Fatalf("retry duplicated or reset failed cohort: %+v err=%v", again, err)
	}
	for _, call := range delivery.calls {
		if call.Position == 2 {
			t.Fatal("cancelled suffix reached native delivery")
		}
	}
}

func TestPerWorkApprovalsAreValidatedBeforeAtomicCohortAdmission(t *testing.T) {
	for _, fault := range []string{"wrong-phrase", "missing", "duplicate", "duplicate-selection", "outside-selection", "scalar-mixed", "not-go", "stale-snapshot"} {
		t.Run(fault, func(t *testing.T) {
			f := newInstructorFixture(t)
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Publication: &protectedPublicationFixture{}})
			first, second := publicationReadyWork(t, f, "first"), publicationReadyWork(t, f, "second")
			snap := f.snapshot(t)
			in := instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, WorkIDs: []string{first.ID, second.ID}, IdempotencyKey: "invalid-cohort", Approvals: []domain.InstructorWorkApproval{{WorkID: first.ID, Approval: first.Gate.ExactApproval}, {WorkID: second.ID, Approval: second.Gate.ExactApproval}}}
			switch fault {
			case "wrong-phrase":
				in.Approvals[1].Approval = first.Gate.ExactApproval
			case "missing":
				in.Approvals = in.Approvals[:1]
			case "duplicate":
				in.Approvals[1] = in.Approvals[0]
			case "duplicate-selection":
				in.WorkIDs = []string{first.ID, first.ID}
			case "outside-selection":
				in.WorkIDs = in.WorkIDs[:1]
			case "scalar-mixed":
				in.Approval = first.Gate.ExactApproval
			case "not-go":
				in.Command = "Weiter"
			case "stale-snapshot":
				f.trustedFacts(t, second.ID, func(w *domain.InstructorWork) { w.Summary = "new fact" })
			}
			if _, err := f.service.Command(context.Background(), in); err == nil {
				t.Fatal("invalid cohort was admitted")
			}
			for _, original := range []domain.InstructorWork{first, second} {
				details, err := f.service.Details(context.Background(), original.ID)
				if err != nil || details.Work.Gate == nil || len(details.Work.PublicationGrants) != 0 || len(details.Dispatches) != 0 {
					t.Fatalf("invalid suffix partially authorized another work: %+v err=%v", details, err)
				}
			}
		})
	}
}

func TestCohortFailureCannotAdvanceAStoppedHumanTransition(t *testing.T) {
	f := newInstructorFixture(t)
	works := []domain.InstructorWork{}
	for _, name := range []string{"first-transition", "second-transition"} {
		w := f.register(t, name, instructorsvc.DefaultPolicy())
		w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
			w.Phase, w.State = "triage", "waiting_human"
			w.Gate = &domain.InstructorGate{ID: name, Kind: "transition", TransitionTo: "fix", OpenedAt: f.now}
		})
		works = append(works, w)
	}
	snap := f.snapshot(t)
	result, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, WorkIDs: []string{works[0].ID, works[1].ID}, IdempotencyKey: "transitions", Approvals: []domain.InstructorWorkApproval{{WorkID: works[0].ID}, {WorkID: works[1].ID}}})
	if err != nil {
		t.Fatal(err)
	}
	f.delivery.err = errors.New("native failure")
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	details, err := f.service.Details(context.Background(), works[1].ID)
	if err != nil || details.Work.Phase != "triage" || details.Work.Gate == nil || details.Work.Gate.ID != works[1].Gate.ID || len(details.Work.ApprovedTransitions) != 0 {
		t.Fatalf("cancelled suffix autonomously entered fix: %+v err=%v", details.Work, err)
	}
	rows, err := f.store.ListInstructorDispatches(context.Background(), result.BatchID, "")
	if err != nil || rows[1].State != "cancelled" {
		t.Fatalf("cohort suffix not cancelled: %+v err=%v", rows, err)
	}
}

func TestCohortLateFailureFencesLeasedSuffixAtNativeAdmission(t *testing.T) {
	f := newInstructorFixture(t)
	works := []domain.InstructorWork{}
	for _, name := range []string{"leased-first", "leased-second"} {
		w := f.register(t, name, instructorsvc.DefaultPolicy())
		w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
			w.Phase, w.State = "triage", "waiting_human"
			w.Gate = &domain.InstructorGate{ID: name, Kind: "transition", TransitionTo: "fix", OpenedAt: f.now}
		})
		works = append(works, w)
	}
	snap := f.snapshot(t)
	result, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, WorkIDs: []string{works[0].ID, works[1].ID}, IdempotencyKey: "leased-cohort", Approvals: []domain.InstructorWorkApproval{{WorkID: works[0].ID}, {WorkID: works[1].ID}}})
	if err != nil {
		t.Fatal(err)
	}
	first, claimed, err := f.store.ClaimInstructorDispatch(context.Background(), f.now)
	if err != nil || !claimed || first.ID != result.Dispatches[0].ID {
		t.Fatalf("first claim: %+v claimed=%v err=%v", first, claimed, err)
	}
	if err := f.store.SetInstructorDispatch(context.Background(), first.ID, "delivered", "", f.now); err != nil {
		t.Fatal(err)
	}
	second, claimed, err := f.store.ClaimInstructorDispatch(context.Background(), f.now)
	if err != nil || !claimed || second.ID != result.Dispatches[1].ID {
		t.Fatalf("suffix claim: %+v claimed=%v err=%v", second, claimed, err)
	}
	target, err := f.store.CreateSession(context.Background(), domain.SessionRecord{ProjectID: "governed", Kind: domain.KindOrchestrator, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.BindInstructorDispatchTarget(context.Background(), second.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetInstructorDispatch(context.Background(), first.ID, "failed", "late native rejection", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ValidateDispatchAdmission(context.Background(), target.ID, second.ID); err == nil {
		t.Fatal("leased suffix crossed native submission after cohort failure")
	}
	rows, err := f.store.ListInstructorDispatches(context.Background(), result.BatchID, "")
	if err != nil || rows[1].State != "cancelled" {
		t.Fatalf("leased suffix was not durably fenced: %+v err=%v", rows, err)
	}
}

func TestLegacyScalarCohortRejectsMixedOrUnknownSignaturesAtomically(t *testing.T) {
	for _, fault := range []string{"mixed-branches", "unknown-gate", "missing-gate-kind", "wrong-literal", "missing-publication-phrase"} {
		t.Run(fault, func(t *testing.T) {
			f := newInstructorFixture(t)
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Publication: &protectedPublicationFixture{}})
			first, second := publicationReadyWork(t, f, "scalar-first"), publicationReadyWork(t, f, "scalar-second")
			literal := first.Gate.ExactApproval
			switch fault {
			case "unknown-gate":
				second = f.trustedFacts(t, second.ID, func(w *domain.InstructorWork) { w.Gate = nil; w.State = "ready" })
			case "wrong-literal":
				literal = "Go"
			case "missing-gate-kind":
				first = f.trustedFacts(t, first.ID, func(w *domain.InstructorWork) { w.Gate.Kind, w.Gate.ExactApproval = "", "" })
				second = f.trustedFacts(t, second.ID, func(w *domain.InstructorWork) { w.Gate.Kind, w.Gate.ExactApproval = "", "" })
				literal = ""
			case "missing-publication-phrase":
				first = f.trustedFacts(t, first.ID, func(w *domain.InstructorWork) { w.Gate.ExactApproval = "" })
				second = f.trustedFacts(t, second.ID, func(w *domain.InstructorWork) { w.Gate.ExactApproval = "" })
				literal = ""
			}
			snap := f.snapshot(t)
			if _, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, WorkIDs: []string{first.ID, second.ID}, Approval: literal, IdempotencyKey: "legacy-invalid-scalar"}); err == nil {
				t.Fatal("scalar approval inferred another work's signature")
			}
			for _, original := range []domain.InstructorWork{first, second} {
				details, err := f.service.Details(context.Background(), original.ID)
				if err != nil || details.Work.Revision != original.Revision || len(details.Work.PublicationGrants) != 0 || len(details.Dispatches) != 0 {
					t.Fatalf("invalid scalar batch granted partial authority: %+v err=%v", details, err)
				}
			}
		})
	}
}

func TestLegacyScalarCohortRetainsFailureAndIdempotencyFences(t *testing.T) {
	f := newInstructorFixture(t)
	works := []domain.InstructorWork{}
	for _, name := range []string{"legacy-first", "legacy-second"} {
		w := f.register(t, name, instructorsvc.DefaultPolicy())
		w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
			w.Phase, w.State = "triage", "waiting_human"
			w.Gate = &domain.InstructorGate{ID: name, Kind: "transition", TransitionTo: "fix", ExactApproval: "GO", OpenedAt: f.now}
		})
		works = append(works, w)
	}
	snapshot := f.snapshot(t)
	in := instructorsvc.CommandInput{Command: "Go", SnapshotID: snapshot.ID, WorkIDs: []string{works[1].ID, works[0].ID}, Approval: "GO", IdempotencyKey: "legacy-failure"}
	result, err := f.service.Command(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	f.delivery.err = instructorsvc.ErrDeliveryRejected
	if err := f.service.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.ListInstructorDispatches(context.Background(), result.BatchID, "")
	if err != nil || len(rows) != 2 || rows[0].State != "failed" || rows[1].State != "cancelled" || len(f.delivery.calls) != 1 {
		t.Fatalf("legacy scalar bypassed ordered cancellation: %+v err=%v", rows, err)
	}
	last, err := f.service.Details(context.Background(), works[0].ID)
	if err != nil || last.Work.Phase != "triage" || last.Work.Gate == nil || last.Work.Gate.ID != works[0].Gate.ID || len(last.Work.ApprovedTransitions) != 0 {
		t.Fatalf("legacy cancelled suffix advanced: %+v err=%v", last.Work, err)
	}
	retry, err := f.service.Command(context.Background(), in)
	if err != nil || retry.BatchID != result.BatchID || len(retry.Dispatches) != 2 || retry.Dispatches[1].State != "cancelled" {
		t.Fatalf("legacy retry duplicated or restarted failure: %+v err=%v", retry, err)
	}
}

func TestLegacyScalarCohortReplaysPreNormalizationDigest(t *testing.T) {
	f := newInstructorFixture(t)
	works := []domain.InstructorWork{}
	for _, name := range []string{"old-first", "old-second"} {
		w := f.register(t, name, instructorsvc.DefaultPolicy())
		w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
			w.State = "waiting_human"
			w.Gate = &domain.InstructorGate{ID: name, Kind: "human", ExactApproval: "GO", OpenedAt: f.now}
		})
		works = append(works, w)
	}
	snapshot := f.snapshot(t)
	in := instructorsvc.CommandInput{Command: "Go", SnapshotID: snapshot.ID, WorkIDs: []string{works[0].ID, works[1].ID}, Approval: "GO", IdempotencyKey: "pre-normalization-key"}
	request, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	keyDigest, requestDigest := sha256.Sum256([]byte(in.IdempotencyKey)), sha256.Sum256(request)
	batchID := "batch-" + hex.EncodeToString(keyDigest[:])
	old := []domain.InstructorDispatch{
		{ID: batchID + "-0", BatchID: batchID, WorkID: works[0].ID, ProjectID: works[0].ProjectID, SnapshotID: snapshot.ID, WorkRevision: works[0].Revision, Position: 0, Action: "go", CommandDigest: hex.EncodeToString(requestDigest[:]), State: "failed", CreatedAt: f.now, UpdatedAt: f.now},
		{ID: batchID + "-1", BatchID: batchID, WorkID: works[1].ID, ProjectID: works[1].ProjectID, SnapshotID: snapshot.ID, WorkRevision: works[1].Revision, Position: 1, Action: "go", CommandDigest: hex.EncodeToString(requestDigest[:]), State: "cancelled", CreatedAt: f.now, UpdatedAt: f.now},
	}
	if err := f.store.EnqueueInstructorDispatches(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Command(context.Background(), in)
	if err != nil || result.BatchID != batchID || len(result.Dispatches) != 2 || result.Dispatches[1].State != "cancelled" {
		t.Fatalf("legacy persisted digest was treated as a new command: %+v err=%v", result, err)
	}
	for _, w := range works {
		details, err := f.service.Details(context.Background(), w.ID)
		if err != nil || details.Work.Gate == nil || len(details.Work.PublicationGrants) != 0 {
			t.Fatalf("legacy retry created new authority: %+v err=%v", details, err)
		}
	}
}

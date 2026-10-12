package instructor_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// This is a trusted boundary fake for admission tests, never a production
// claim that host workers are isolated. The real host boundary denies all three.
type protectedPublicationFixture struct {
	mu           sync.Mutex
	missing      string
	authorizeErr error
	executeErr   error
	requests     []instructorsvc.PublicationRequest
	started      chan struct{}
	release      chan struct{}
}

func (p *protectedPublicationFixture) Capabilities(context.Context, domain.InstructorWork) (domain.InstructorPublicationCapabilities, error) {
	return domain.InstructorPublicationCapabilities{Available: true, WorkerIsolation: p.missing != "workers", ProtectedOperator: p.missing != "operator", IsolatedCredentials: p.missing != "credentials"}, nil
}
func (p *protectedPublicationFixture) Authorize(context.Context, instructorsvc.PublicationDecision) (instructorsvc.PublicationAuthorization, error) {
	if p.authorizeErr != nil {
		return instructorsvc.PublicationAuthorization{}, p.authorizeErr
	}
	if p.missing == "identity" {
		return instructorsvc.PublicationAuthorization{}, nil
	}
	return instructorsvc.PublicationAuthorization{OperatorID: "protected-operator", AuthorizationID: "protected-decision-proof"}, nil
}
func (p *protectedPublicationFixture) Execute(_ context.Context, request instructorsvc.PublicationRequest) (instructorsvc.PublicationReceipt, error) {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	p.mu.Unlock()
	if p.started != nil {
		close(p.started)
		<-p.release
	}
	return instructorsvc.PublicationReceipt{Ref: "https://publisher.example/pr/1"}, p.executeErr
}

func publicationReadyWork(t *testing.T, f *instructorFixture, names ...string) domain.InstructorWork {
	t.Helper()
	name := "controlled-publication"
	if len(names) > 0 {
		name = names[0]
	}
	w := f.register(t, name, instructorsvc.DefaultPolicy())
	worker := f.attachWorker(t, w.ID)
	return f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.Phase, w.SHA, w.Branch, w.State = "closeout", immutableSHA, "feature/"+name, "waiting_human"
		w.PhaseSequence, w.FixPhaseSequence, w.DeltaPhaseSequence = 4, 2, 3
		w.Gate = &domain.InstructorGate{ID: "controlled-publication-gate", Kind: "publication", ExactApproval: "PUBLISH-GO " + w.Branch, OpenedAt: f.now}
		for _, kind := range w.Policy.CloseoutRequires {
			if kind == "ci" || kind == "traceability" {
				continue
			}
			e := domain.InstructorEvidence{ID: "ready-" + kind, WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: kind, Phase: "delta", PhaseSequence: 3, Producer: "independent-reviewer", Role: "reviewer", Ref: "trusted:" + kind, Verified: true}
			if kind == "push" || kind == "full" {
				e.Kind, e.Profile, e.Role, e.Producer = "check", kind, "worker", worker
			}
			if kind == "delta" {
				e.Outcome = "approved"
			}
			w.Evidence = append(w.Evidence, e)
		}
	})
}

func installPublication(t *testing.T, f *instructorFixture, boundary *protectedPublicationFixture) (domain.InstructorWork, instructorsvc.PublicationInput) {
	t.Helper()
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Clock: func() time.Time { return f.now }, Publication: boundary})
	w := publicationReadyWork(t, f)
	snap := f.snapshot(t)
	_, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "controlled-approval", Approval: "PUBLISH-GO " + w.Branch})
	if err != nil {
		t.Fatalf("protected approval: %v", err)
	}
	d, err := f.service.Details(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	g := d.Work.PublicationGrants[0]
	if g.ID == "" || g.OperatorID != "protected-operator" || g.AuthorizationID == "" || g.Operation != "publish" {
		t.Fatalf("grant lost protected operator or exact operation: %+v", g)
	}
	return d.Work, instructorsvc.PublicationInput{WorkID: w.ID, GrantID: g.ID, SnapshotID: g.SnapshotID, WorkRevision: d.Work.Revision, SHA: g.SHA, Branch: g.Branch, Operation: g.Operation}
}

func TestPublicationDeniesUnprotectedApprovalAndSessionSelfGrant(t *testing.T) {
	for _, missing := range []string{"absent", "workers", "operator", "credentials", "identity", "authorization", "session"} {
		t.Run(missing, func(t *testing.T) {
			f := newInstructorFixture(t)
			boundary := &protectedPublicationFixture{missing: missing}
			if missing == "authorization" {
				boundary.authorizeErr = errors.New("operator proof rejected")
			}
			deps := instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Publication: boundary}
			if missing == "absent" {
				deps.Publication = nil
			}
			f.service = instructorsvc.New(deps)
			w := publicationReadyWork(t, f)
			snap := f.snapshot(t)
			in := instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "denied-" + missing, Approval: "PUBLISH-GO " + w.Branch}
			if missing == "session" {
				in.RequesterSessionID = "worker-1"
			}
			if _, err := f.service.Command(context.Background(), in); err == nil {
				t.Fatal("unprotected caller minted publication authority")
			}
			d, err := f.service.Details(context.Background(), w.ID)
			if err != nil || len(d.Work.PublicationGrants) != 0 || d.Work.Gate == nil || len(d.Dispatches) != 0 {
				t.Fatalf("denial changed the gate or dispatched work: %+v err=%v", d, err)
			}
		})
	}
}

func TestPublicationExecutorRejectsChangedExactScopeBeforeAnyEffect(t *testing.T) {
	for _, fault := range []string{"sha", "branch", "snapshot", "revision", "operation", "grant", "policy", "legacy-proof", "missing-check", "parked"} {
		t.Run(fault, func(t *testing.T) {
			f := newInstructorFixture(t)
			p := &protectedPublicationFixture{}
			_, in := installPublication(t, f, p)
			switch fault {
			case "sha":
				in.SHA = "2222222222222222222222222222222222222222"
			case "branch":
				in.Branch = "feature/other"
			case "snapshot":
				in.SnapshotID = "other-snapshot"
			case "revision":
				in.WorkRevision++
			case "operation":
				in.Operation = "merge"
			case "grant":
				in.GrantID = "other-grant"
			default:
				w := f.trustedFacts(t, in.WorkID, func(w *domain.InstructorWork) {
					switch fault {
					case "policy":
						w.PolicyVersion = "changed-policy"
					case "legacy-proof":
						w.PublicationGrants[0].AuthorizationID = ""
					case "missing-check":
						w.Evidence = nil
					case "parked":
						w.State = "parked"
					}
				})
				in.WorkRevision = w.Revision
			}
			if _, err := f.service.ExecutePublication(context.Background(), in); err == nil {
				t.Fatal("changed scope published")
			}
			if len(p.requests) != 0 {
				t.Fatal("denial reached publisher")
			}
		})
	}
}

func TestPublicationMergeAuthorityDoesNotAuthorizePushOrSkipRemoteProof(t *testing.T) {
	f := newInstructorFixture(t)
	p := &protectedPublicationFixture{}
	_, in := installPublication(t, f, p)
	w := f.trustedFacts(t, in.WorkID, func(w *domain.InstructorWork) { w.PublicationGrants[0].Operation = "merge" })
	in.Operation, in.WorkRevision = "merge", w.Revision
	if instructorsvc.HasPublicationGrant(w) {
		t.Fatal("a merge-only approval authorized a push")
	}
	if _, err := f.service.ExecutePublication(context.Background(), in); err == nil || len(p.requests) != 0 {
		t.Fatalf("merge skipped current remote CI/traceability: err=%v calls=%d", err, len(p.requests))
	}
}

func TestPublicationExecutionRechecksExceptionExpiryBeforeTick(t *testing.T) {
	f := newInstructorFixture(t)
	p := &protectedPublicationFixture{}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Clock: func() time.Time { return f.now }, Publication: p})
	w := publicationReadyWork(t, f)
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.Exceptions = []domain.InstructorException{{ID: "scoped-role-exception", ExpiresAt: f.now.Add(time.Minute)}}
	})
	snap := f.snapshot(t)
	_, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "valid-exception-approval", Approval: "PUBLISH-GO " + w.Branch})
	if err != nil {
		t.Fatal(err)
	}
	details, err := f.service.Details(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	g := details.Work.PublicationGrants[0]
	f.now = f.now.Add(2 * time.Minute)
	_, err = f.service.ExecutePublication(context.Background(), instructorsvc.PublicationInput{WorkID: w.ID, GrantID: g.ID, SnapshotID: g.SnapshotID, WorkRevision: details.Work.Revision, SHA: g.SHA, Branch: g.Branch, Operation: g.Operation})
	if err == nil || !strings.Contains(err.Error(), "exception expired") || len(p.requests) != 0 {
		t.Fatalf("exception expired before any Tick but reached publisher: err=%v calls=%d", err, len(p.requests))
	}
	details, err = f.service.Details(context.Background(), w.ID)
	if err != nil || details.Work.PublicationGrants[0].Execution != nil {
		t.Fatalf("expired exception claimed the publication grant: %+v err=%v", details.Work.PublicationGrants, err)
	}
}

func TestPublicationGrantIsClaimedOnceAcrossParallelCallsAndServiceRestart(t *testing.T) {
	f := newInstructorFixture(t)
	p := &protectedPublicationFixture{started: make(chan struct{}), release: make(chan struct{})}
	_, in := installPublication(t, f, p)
	done := make(chan error, 1)
	go func() { _, err := f.service.ExecutePublication(context.Background(), in); done <- err }()
	<-p.started
	// Another service has its own lock: only the persisted CAS/claim protects
	// this retry while the first daemon is between admission and completion.
	restarted := instructorsvc.New(instructorsvc.Deps{Store: f.store, Publication: p})
	if _, err := restarted.ExecutePublication(context.Background(), in); !errors.Is(err, instructorsvc.ErrPublicationConsumed) {
		t.Fatalf("parallel replay: %v", err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.ExecutePublication(context.Background(), in); !errors.Is(err, instructorsvc.ErrPublicationConsumed) {
		t.Fatalf("completed replay: %v", err)
	}
	d, err := restarted.Details(context.Background(), in.WorkID)
	if err != nil || d.Work.PublicationGrants[0].Execution.State != "completed" || len(p.requests) != 1 {
		t.Fatalf("single protected effect lost: %+v calls=%d err=%v", d.Work.PublicationGrants, len(p.requests), err)
	}
}

func TestPublicationErrorConsumesGrantAndFencesAnotherApproval(t *testing.T) {
	f := newInstructorFixture(t)
	p := &protectedPublicationFixture{executeErr: errors.New("connection lost after remote submission")}
	_, in := installPublication(t, f, p)
	if _, err := f.service.ExecutePublication(context.Background(), in); !errors.Is(err, instructorsvc.ErrPublicationConsumed) {
		t.Fatalf("uncertain publisher outcome: %v", err)
	}
	if _, err := f.service.ExecutePublication(context.Background(), in); !errors.Is(err, instructorsvc.ErrPublicationConsumed) {
		t.Fatalf("uncertain retry: %v", err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	var reopenErr error
	f.store, reopenErr = sqlite.OpenPreMigrated(f.dataDir)
	if reopenErr != nil {
		t.Fatal(reopenErr)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Publication: p})
	if _, err := f.service.ExecutePublication(context.Background(), in); !errors.Is(err, instructorsvc.ErrPublicationConsumed) {
		t.Fatalf("SQLite restart lost consumed publication: %v", err)
	}
	w := f.trustedFacts(t, in.WorkID, func(w *domain.InstructorWork) {
		w.State = "waiting_human"
		w.Gate = &domain.InstructorGate{ID: "unsafe-fresh-approval", Kind: "publication", ExactApproval: "PUBLISH-GO " + w.Branch}
	})
	snap := f.snapshot(t)
	_, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: snap.ID, WorkIDs: []string{w.ID}, IdempotencyKey: "retry-new-approval", Approval: "PUBLISH-GO " + w.Branch})
	if !errors.Is(err, instructorsvc.ErrPublicationConsumed) || len(p.requests) != 1 {
		t.Fatalf("new phrase silently retried uncertain publication: %v calls=%d", err, len(p.requests))
	}
}

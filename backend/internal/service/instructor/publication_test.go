package instructor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type publicationFacts struct {
	observed bool
	err      error
	calls    int
}

func (*publicationFacts) Verify(context.Context, domain.InstructorWork, domain.InstructorEvidence) error {
	return errors.New("test must not certify remote evidence")
}
func (v *publicationFacts) ObservedPublication(context.Context, domain.InstructorWork) (string, bool, error) {
	v.calls++
	return "https://github.com/fork/repo/pull/observed", v.observed, v.err
}

func TestInstructorPublicationGrantNeedsLocalProofAndPrecedesRemoteProof(t *testing.T) {
	for _, fault := range []string{"valid-before-remote", "missing-local-full", "negative-local-delta", "previous-remote-effect", "remote-inspection-error", "wrong-signature"} {
		t.Run(fault, func(t *testing.T) {
			f := newInstructorFixture(t)
			observer := &publicationFacts{}
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, Verifier: observer, Clock: func() time.Time { return f.now }})
			work := f.register(t, "publish-"+fault, instructorsvc.DefaultPolicy())
			worker := f.attachWorker(t, work.ID)
			work = f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
				w.Phase, w.SHA, w.Branch, w.State = "closeout", immutableSHA, "feature/publish", "waiting_human"
				w.PhaseSequence, w.FixPhaseSequence, w.DeltaPhaseSequence = 4, 2, 3
				w.Gate = &domain.InstructorGate{ID: "publication-local-proof", Kind: "publication", ExactApproval: "PUBLISH-GO " + w.Branch, OpenedAt: f.now}
				for _, kind := range w.Policy.CloseoutRequires {
					if kind == "ci" || kind == "traceability" || kind == "full" && fault == "missing-local-full" {
						continue
					}
					evidence := domain.InstructorEvidence{ID: "local-" + kind, WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: kind, Phase: "delta", PhaseSequence: 3, Producer: "independent-reviewer", Role: "reviewer", Ref: "trusted:" + kind, Verified: true}
					if kind == "push" || kind == "full" {
						evidence.Kind, evidence.Profile, evidence.Role, evidence.Producer = "check", kind, "worker", worker
					}
					if kind == "delta" {
						evidence.Outcome = "approved"
						if fault == "negative-local-delta" {
							evidence.Outcome = "changes_requested"
						}
					}
					w.Evidence = append(w.Evidence, evidence)
				}
				// A client URL is neither real remote observation nor verified CI.
				w.Evidence = append(w.Evidence, domain.InstructorEvidence{ID: "unverified-client-url", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: "ci", Producer: worker, Ref: "https://github.com/fork/repo/pull/client-claim"})
			})
			if fault == "previous-remote-effect" {
				observer.observed = true
			}
			if fault == "remote-inspection-error" {
				observer.err = errors.New("provider observation unavailable")
			}
			snapshot := f.snapshot(t)
			approval := "PUBLISH-GO " + work.Branch
			if fault == "wrong-signature" {
				approval = "Go"
			}
			result, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Go", SnapshotID: snapshot.ID, WorkIDs: []string{work.ID}, IdempotencyKey: "approval-" + fault, Approval: approval})
			details, readErr := f.service.Details(context.Background(), work.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if fault != "valid-before-remote" {
				if err == nil || instructorsvc.HasPublicationGrant(details.Work) || len(result.Dispatches) != 0 {
					t.Fatalf("invalid publication acquired human authority: %+v work=%+v err=%v", result, details.Work, err)
				}
				return
			}
			if err != nil || !instructorsvc.HasPublicationGrant(details.Work) || len(result.Dispatches) != 1 || observer.calls != 1 || len(details.Work.PublicationGrants) != 1 {
				t.Fatalf("local readiness required remote proof before approval or lost durable authority: %+v work=%+v calls=%d err=%v", result, details.Work, observer.calls, err)
			}
			grant := details.Work.PublicationGrants[0]
			if grant.SnapshotID != snapshot.ID || grant.WorkRevision != work.Revision || grant.Approval != approval {
				t.Fatalf("publication grant is not the operator's frozen decision: %+v", grant)
			}
			if _, err = f.service.Report(context.Background(), instructorsvc.ReportInput{WorkID: work.ID, SessionID: worker, Kind: "done"}); err == nil {
				t.Fatal("publication authorization itself certified absent remote CI/traceability")
			}
		})
	}
}

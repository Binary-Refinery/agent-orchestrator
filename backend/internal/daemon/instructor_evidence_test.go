package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type evidenceStoreFake struct {
	runs   map[string]domain.ReviewRun
	checks []domain.PullRequestCheck
	prs    []domain.PullRequest
}

func (f evidenceStoreFake) GetReviewRun(_ context.Context, id string) (domain.ReviewRun, bool, error) {
	run, found := f.runs[id]
	return run, found, nil
}
func (f evidenceStoreFake) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return domain.SessionRecord{}, false, nil
}
func (f evidenceStoreFake) ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error) {
	return f.prs, nil
}
func (f evidenceStoreFake) ListChecks(context.Context, string) ([]domain.PullRequestCheck, error) {
	return f.checks, nil
}

func evidenceFixture() (domain.InstructorWork, domain.InstructorEvidence, evidenceStoreFake) {
	sha := strings.Repeat("a", 40)
	selection := domain.InstructorAgentSelection{Harness: "codex", Model: "native-model", Effort: "xhigh"}
	work := domain.InstructorWork{ID: "work", ProjectID: "project", SHA: sha, PolicyVersion: "policy", SessionIDs: []domain.SessionID{"worker"}, Policy: domain.InstructorPolicy{Roles: map[string]domain.InstructorRole{"reviewer": {Harness: "codex", Effort: "maximum", Independent: true, EffortByModel: map[string]string{"native-model": "xhigh"}}}}, Launches: []domain.InstructorLaunch{{ReviewID: "reviewer", ReviewRunID: "run", SHA: sha, Role: "reviewer", Requested: selection, Sent: &selection, Status: "launched"}}}
	evidence := domain.InstructorEvidence{WorkID: "work", PolicyVersion: "policy", SHA: sha, Kind: "review-report", Producer: "reviewer", Role: "reviewer", Harness: "codex", Model: "native-model", Effort: "xhigh", Ref: "run"}
	store := evidenceStoreFake{runs: map[string]domain.ReviewRun{"run": {ID: "run", ReviewID: "reviewer", SessionID: "worker", Harness: "codex", TargetSHA: sha, Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved}}}
	return work, evidence, store
}

func TestInstructorReviewEvidenceUsesActualRunAndLaunch(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*domain.InstructorWork, *domain.InstructorEvidence, *evidenceStoreFake)
		reject bool
	}{
		{"approved", func(*domain.InstructorWork, *domain.InstructorEvidence, *evidenceStoreFake) {}, false},
		{"other commit", func(w *domain.InstructorWork, e *domain.InstructorEvidence, f *evidenceStoreFake) {
			r := f.runs["run"]
			r.TargetSHA = strings.Repeat("b", 40)
			f.runs["run"] = r
		}, true},
		{"worker impersonation", func(w *domain.InstructorWork, e *domain.InstructorEvidence, f *evidenceStoreFake) {
			e.Producer = "worker"
		}, true},
		{"unrecorded sent config", func(w *domain.InstructorWork, e *domain.InstructorEvidence, f *evidenceStoreFake) {
			w.Launches[0].Sent = nil
		}, true},
		{"other pass same reviewer", func(w *domain.InstructorWork, e *domain.InstructorEvidence, f *evidenceStoreFake) {
			w.Launches[0].ReviewRunID = "other"
		}, true},
		{"false model assertion", func(w *domain.InstructorWork, e *domain.InstructorEvidence, f *evidenceStoreFake) { e.Effort = "low" }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			work, evidence, store := evidenceFixture()
			test.mutate(&work, &evidence, &store)
			err := (instructorEvidenceVerifier{store: store}).Verify(context.Background(), work, evidence)
			if (err != nil) != test.reject {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestInstructorNegativeReviewCanEnterTriageButCannotClearFindings(t *testing.T) {
	work, evidence, store := evidenceFixture()
	run := store.runs["run"]
	run.Verdict = domain.VerdictChangesRequested
	store.runs["run"] = run
	verifier := instructorEvidenceVerifier{store: store}
	for _, kind := range []string{"review-report", "triage"} {
		evidence.Kind = kind
		evidence.Outcome = "approved"
		evidence.CriticalFindings = 0
		assessed, err := verifier.Assess(context.Background(), work, evidence)
		if err != nil {
			t.Fatal(err)
		}
		if assessed.CriticalFindings != 1 || assessed.Outcome != string(domain.VerdictChangesRequested) {
			t.Fatalf("assessment=%#v", assessed)
		}
	}
	evidence.Kind = "delta"
	if _, err := verifier.Assess(context.Background(), work, evidence); err == nil {
		t.Fatal("negative review certified a green delta")
	}
}

func TestInstructorCIEvidenceRequiresAllChecksAtExactHead(t *testing.T) {
	work, evidence, store := evidenceFixture()
	evidence.Kind = "ci"
	work.Branch = "feature"
	instructorTestPublicationGrant(&work)
	evidence.Producer = "worker"
	evidence.Ref = "https://github.com/fork/repo/pull/1"
	store.prs = []domain.PullRequest{{URL: evidence.Ref, HeadSHA: work.SHA}}
	oldSHA := strings.Repeat("b", 40)
	check := func(status domain.PRCheckStatus, conclusion string) domain.PullRequestCheck {
		return domain.PullRequestCheck{CommitHash: work.SHA, Status: status, Conclusion: conclusion}
	}
	green := check(domain.PRCheckPassed, "success")
	neutral := check(domain.PRCheckPassed, "neutral")
	skipped := check(domain.PRCheckSkipped, "skipped")
	for _, test := range []struct {
		name   string
		checks []domain.PullRequestCheck
		reject bool
	}{
		{"current green", []domain.PullRequestCheck{green}, false},
		{"native neutral", []domain.PullRequestCheck{neutral}, false},
		{"green and neutral", []domain.PullRequestCheck{green, neutral}, false},
		{"green and optional skipped", []domain.PullRequestCheck{green, skipped}, false},
		{"neutral and optional skipped", []domain.PullRequestCheck{neutral, skipped}, false},
		{"green neutral and optional skipped", []domain.PullRequestCheck{green, neutral, skipped}, false},
		{"all skipped", []domain.PullRequestCheck{skipped, skipped}, true},
		{"historical green and failure", []domain.PullRequestCheck{{CommitHash: oldSHA, Status: domain.PRCheckPassed, Conclusion: "success"}, {CommitHash: oldSHA, Status: domain.PRCheckFailed, Conclusion: "failure"}, green}, false},
		{"historical only", []domain.PullRequestCheck{{CommitHash: oldSHA, Status: domain.PRCheckPassed, Conclusion: "success"}}, true},
		{"historical pass cannot certify current skipped", []domain.PullRequestCheck{{CommitHash: oldSHA, Status: domain.PRCheckPassed, Conclusion: "success"}, skipped}, true},
		{"current failure", []domain.PullRequestCheck{green, check(domain.PRCheckFailed, "failure")}, true},
		{"GitLab current failure", []domain.PullRequestCheck{green, check(domain.PRCheckFailed, "failed")}, true},
		{"cancelled", []domain.PullRequestCheck{green, check(domain.PRCheckCancelled, "cancelled")}, true},
		{"GitLab canceled", []domain.PullRequestCheck{green, check(domain.PRCheckCancelled, "canceled")}, true},
		{"timed out", []domain.PullRequestCheck{green, check(domain.PRCheckFailed, "timed_out")}, true},
		{"queued stale conclusion", []domain.PullRequestCheck{green, check(domain.PRCheckQueued, "success")}, true},
		{"running stale conclusion", []domain.PullRequestCheck{green, check(domain.PRCheckInProgress, "success")}, true},
		{"queued neutral", []domain.PullRequestCheck{green, check(domain.PRCheckQueued, "neutral")}, true},
		{"running skipped", []domain.PullRequestCheck{green, check(domain.PRCheckInProgress, "skipped")}, true},
		{"stale is not a legitimate skip", []domain.PullRequestCheck{green, check(domain.PRCheckSkipped, "stale")}, true},
		{"manual is not a completed skip", []domain.PullRequestCheck{green, check(domain.PRCheckSkipped, "manual")}, true},
		{"unknown status", []domain.PullRequestCheck{green, check(domain.PRCheckUnknown, "success")}, true},
		{"missing normalized status", []domain.PullRequestCheck{green, check("", "success")}, true},
		{"unrecognized normalized status", []domain.PullRequestCheck{green, check("completed", "success")}, true},
		{"unknown conclusion", []domain.PullRequestCheck{green, check(domain.PRCheckPassed, "unknown")}, true},
		{"missing conclusion", []domain.PullRequestCheck{green, check(domain.PRCheckPassed, "")}, true},
		{"failed status contradicts success", []domain.PullRequestCheck{green, check(domain.PRCheckFailed, "success")}, true},
		{"cancelled status contradicts success", []domain.PullRequestCheck{green, check(domain.PRCheckCancelled, "success")}, true},
		{"passed status contradicts failure", []domain.PullRequestCheck{green, check(domain.PRCheckPassed, "failure")}, true},
		{"passed status contradicts skipped", []domain.PullRequestCheck{green, check(domain.PRCheckPassed, "skipped")}, true},
		{"skipped status contradicts success", []domain.PullRequestCheck{green, check(domain.PRCheckSkipped, "success")}, true},
		{"skipped status contradicts neutral", []domain.PullRequestCheck{green, check(domain.PRCheckSkipped, "neutral")}, true},
		{"no checks", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store.checks = test.checks
			err := (instructorEvidenceVerifier{store: store}).Verify(context.Background(), work, evidence)
			if (err != nil) != test.reject {
				t.Fatalf("reject=%v, error=%v", test.reject, err)
			}
		})
	}
}

func TestInstructorDeltaRejectsInitialOrSupersededApproval(t *testing.T) {
	w, e, store := evidenceFixture()
	e.Kind = "delta"
	w.DeltaPhaseSequence = 4
	run := store.runs["run"]
	run.CreatedAt = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	run.ScopeKey = "work/delta/4"
	store.runs["run"] = run
	verifier := instructorEvidenceVerifier{store: store}
	if err := verifier.Verify(context.Background(), w, e); err == nil {
		t.Fatal("initial approval masqueraded as delta")
	}
	w.Launches[0].Phase, w.Launches[0].PhaseSequence = "delta", 4
	if err := verifier.Verify(context.Background(), w, e); err != nil {
		t.Fatalf("current distinct delta refused: %v", err)
	}
	newRun := run
	newRun.ID = "new-run"
	newRun.Verdict = domain.VerdictChangesRequested
	newRun.CreatedAt = run.CreatedAt.Add(time.Second)
	store.runs[newRun.ID] = newRun
	later := w.Launches[0]
	later.ReviewRunID = newRun.ID
	w.Launches = append(w.Launches, later)
	if err := verifier.Verify(context.Background(), w, e); err == nil {
		t.Fatal("historical approval erased later review findings")
	}
}

func TestInstructorVerificationKeyChangesWhenNativeFactsChange(t *testing.T) {
	w, _, store := evidenceFixture()
	verifier := instructorEvidenceVerifier{store: store}
	before, err := verifier.VerificationKey(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	run := store.runs["run"]
	run.Verdict = domain.VerdictChangesRequested
	store.runs["run"] = run
	after, err := verifier.VerificationKey(context.Background(), w)
	if err != nil || before == after {
		t.Fatalf("source change did not wake verification: %v", err)
	}
}

func TestInstructorVerificationKeyIgnoresObservationChurn(t *testing.T) {
	w, _, store := evidenceFixture()
	store.prs = []domain.PullRequest{{URL: "https://github.com/fork/repo/pull/1", HeadSHA: w.SHA, CI: domain.CIPassing}}
	store.checks = []domain.PullRequestCheck{{Name: "check", CommitHash: w.SHA, Status: domain.PRCheckPassed, Conclusion: "success"}}
	v := instructorEvidenceVerifier{store: store}
	before, err := v.VerificationKey(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	store.prs[0].ObservedAt = time.Now()
	store.prs[0].CIObservedAt = time.Now()
	store.prs[0].UpdatedAt = time.Now()
	store.checks[0].CreatedAt = time.Now()
	store.checks[0].LogTail = "new log excerpt"
	store.checks = append(store.checks, domain.PullRequestCheck{Name: "historical", CommitHash: strings.Repeat("b", 40), Status: domain.PRCheckFailed, Conclusion: "failure"})
	after, err := v.VerificationKey(context.Background(), w)
	if err != nil || before != after {
		t.Fatalf("unchanged provider facts reset verification: %v", err)
	}
	e := domain.InstructorEvidence{Kind: "task-packet", Ref: "custom/packet.md", WorkID: w.ID, SHA: w.SHA, PolicyVersion: w.PolicyVersion, Producer: "worker", Role: "worker"}
	w.Evidence = append(w.Evidence, e)
	changed, err := v.VerificationKey(context.Background(), w)
	if err != nil || changed == after {
		t.Fatalf("new claim failed to wake verification: %v", err)
	}
	e.ID = "another"
	e.Verified = true
	e.CreatedAt = time.Now()
	w.Evidence = append(w.Evidence, e)
	stable, err := v.VerificationKey(context.Background(), w)
	if err != nil || stable != changed {
		t.Fatalf("reverification reset budget: %v", err)
	}
}

func instructorTestPublicationGrant(work *domain.InstructorWork) {
	work.PublicationGrants = []domain.InstructorPublicationGrant{{WorkID: work.ID, PolicyVersion: work.PolicyVersion, SHA: work.SHA, Branch: work.Branch, GateID: "publication-test", SnapshotID: "snapshot-test", WorkRevision: 1, Approval: "PUBLISH-GO " + work.Branch, ApprovedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}}
}

func TestInstructorRemoteProofRequiresActualPublicationGrant(t *testing.T) {
	work, evidence, store := evidenceFixture()
	evidence.Kind = "ci"
	evidence.Producer = "worker"
	evidence.Ref = "https://github.com/fork/repo/pull/1"
	store.prs = []domain.PullRequest{{URL: evidence.Ref, HeadSHA: work.SHA}}
	store.checks = []domain.PullRequestCheck{{CommitHash: work.SHA, Status: domain.PRCheckPassed, Conclusion: "success"}}
	verifier := instructorEvidenceVerifier{store: store}
	if err := verifier.Verify(context.Background(), work, evidence); err == nil {
		t.Fatal("unapproved remote publication was certified")
	}
	ref, observed, err := verifier.ObservedPublication(context.Background(), work)
	if err != nil || !observed || ref != evidence.Ref {
		t.Fatalf("actual persisted publication not detected: %q %v %v", ref, observed, err)
	}
	work.Branch = "feature"
	instructorTestPublicationGrant(&work)
	before, err := verifier.VerificationKey(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	work.PublicationGrants = nil
	after, err := verifier.VerificationKey(context.Background(), work)
	if err != nil || before == after {
		t.Fatalf("publication authority did not wake verification: %v", err)
	}
}

func TestInstructorFailedCIEvidenceRequiresCurrentNativeFailure(t *testing.T) {
	work, evidence, store := evidenceFixture()
	work.Branch = "feature"
	instructorTestPublicationGrant(&work)
	evidence.Kind = "ci-failed"
	evidence.Producer = "worker"
	evidence.Ref = "https://github.com/fork/repo/pull/1"
	evidence.Outcome = "approved"
	evidence.Verified = true
	store.prs = []domain.PullRequest{{URL: evidence.Ref, HeadSHA: work.SHA}}
	store.checks = []domain.PullRequestCheck{{CommitHash: work.SHA, Conclusion: "failure", Status: domain.PRCheckFailed}}
	verifier := instructorEvidenceVerifier{store: store}
	proof, err := verifier.Assess(context.Background(), work, evidence)
	if err != nil || proof.Outcome != "failed" {
		t.Fatalf("native failure not authoritative: %+v %v", proof, err)
	}
	work.Launches = append(work.Launches, domain.InstructorLaunch{SessionID: "worker", Role: "worker", Status: "launched"})
	claims, err := verifier.Discover(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, claim := range claims {
		found = found || claim.Kind == "ci-failed"
	}
	if !found {
		t.Fatal("failed native checks were not discovered")
	}
	for _, test := range []struct {
		name, sha, conclusion string
		status                domain.PRCheckStatus
	}{
		{"later success", work.SHA, "success", domain.PRCheckPassed}, {"native neutral", work.SHA, "neutral", domain.PRCheckPassed}, {"old failure", strings.Repeat("b", 40), "failure", domain.PRCheckFailed}, {"running", work.SHA, "failure", domain.PRCheckInProgress}, {"pending", work.SHA, "", domain.PRCheckQueued}, {"optional skipped", work.SHA, "skipped", domain.PRCheckSkipped},
	} {
		t.Run(test.name, func(t *testing.T) {
			store.checks = []domain.PullRequestCheck{{CommitHash: test.sha, Conclusion: test.conclusion, Status: test.status}}
			if _, err := (instructorEvidenceVerifier{store: store}).Assess(context.Background(), work, evidence); err == nil {
				t.Fatal("noncurrent/nonterminal/nonfailure certified ci-failed")
			}
		})
	}
}

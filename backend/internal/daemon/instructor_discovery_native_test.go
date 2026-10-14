package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestInstructorDiscoveryAttributionUsesPersistedWorkerNativeConfiguration(t *testing.T) {
	repo := instructorTestRepository(t)
	instructorTestGit(t, repo, "-c", "user.name=Operator", "-c", "user.email=operator@example.test", "commit", "--allow-empty", "-m", "implemented\n\nCo-authored-by: AO Worker <ao@example.test>\nAO-session: worker\nIssue: #12\n")
	sha := instructorTestGit(t, repo, "rev-parse", "HEAD")
	for _, test := range []struct {
		name   string
		mutate func(*instructorTraceabilityStore)
		reject bool
	}{
		{"matching native configuration", func(*instructorTraceabilityStore) {}, false},
		{"changed native model", func(s *instructorTraceabilityStore) { s.session.Metadata.Model = "actual-other-model" }, true},
		{"missing native effort", func(s *instructorTraceabilityStore) { s.session.Metadata.Effort = "" }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			work := domain.InstructorWork{ID: "work", ProjectID: "project", SHA: sha, PolicyVersion: "policy", IssueID: "#12", SessionIDs: []domain.SessionID{"worker"}, Policy: domain.InstructorPolicy{Attribution: domain.InstructorAttribution{AuthorName: "Operator", AuthorEmail: "operator@example.test", CoAuthorName: "AO Worker", CoAuthorEmail: "ao@example.test"}}}
			store := instructorTraceabilityStore{session: domain.SessionRecord{ID: "worker", ProjectID: "project", Metadata: domain.SessionMetadata{WorkspaceRepoPath: repo}}}
			var evidence domain.InstructorEvidence
			instructorAttributionProvenance(&work, &evidence, &store)
			test.mutate(&store)
			verifier := instructorEvidenceVerifier{store: store}
			claims, err := verifier.Discover(context.Background(), work)
			if err != nil {
				t.Fatal(err)
			}
			if len(claims) != 1 || claims[0].Kind != "attribution" {
				t.Fatalf("expected one attribution claim, got %+v", claims)
			}
			claim := claims[0]
			if claim.Harness != string(store.session.Harness) || claim.Model != store.session.Metadata.Model || claim.Effort != store.session.Metadata.Effort {
				t.Fatalf("discovery lost the persisted native configuration: claim=%+v, session=%+v", claim, store.session)
			}
			if claim.WorkID != work.ID || claim.PolicyVersion != work.PolicyVersion || claim.SHA != sha || claim.Ref != sha || claim.Producer != "worker" || claim.Role != "worker" || claim.Verified {
				t.Fatalf("discovery changed the evidence identity or granted verification: %+v", claim)
			}
			if err := verifier.verifyAttribution(context.Background(), work, claim); (err != nil) != test.reject {
				t.Fatalf("reject=%v, verification error=%v", test.reject, err)
			}
		})
	}
}

type instructorDiscoveryNativeStore struct {
	instructorTraceabilityStore
	found bool
	err   error
}

func (s instructorDiscoveryNativeStore) GetSession(_ context.Context, _ domain.SessionID) (domain.SessionRecord, bool, error) {
	return s.session, s.found, s.err
}

func TestInstructorDiscoveryAttributionRequiresActualSessionRead(t *testing.T) {
	readErr := errors.New("session read failed")
	for _, test := range []struct {
		name string
		err  error
	}{
		{"missing session", nil},
		{"session read failure", readErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			work := domain.InstructorWork{SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SessionIDs: []domain.SessionID{"worker"}, Launches: []domain.InstructorLaunch{{SessionID: "worker", Role: "worker", Status: "launched", Requested: domain.InstructorAgentSelection{Harness: "opencode", Model: "requested-model", Effort: "xhigh"}}}}
			store := instructorDiscoveryNativeStore{err: test.err}
			claims, err := (instructorEvidenceVerifier{store: store}).Discover(context.Background(), work)
			if !errors.Is(err, test.err) {
				t.Fatalf("expected session read error %v, got %v", test.err, err)
			}
			if len(claims) != 0 {
				t.Fatalf("discovery fabricated attribution without a session: %+v", claims)
			}
		})
	}
}

func TestInstructorDiscoveryMissingNativeSessionPreservesSCMFacts(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const oldSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const prURL = "https://github.com/fork/repo/pull/1"
	readErr := errors.New("session read failed")
	for _, test := range []struct {
		name       string
		prSHA      string
		checkSHA   string
		status     domain.PRCheckStatus
		conclusion string
		wantFailed bool
		readErr    error
	}{
		{"current failed check", sha, sha, domain.PRCheckFailed, "failure", true, nil},
		{"old pull request head", oldSHA, sha, domain.PRCheckFailed, "failure", false, nil},
		{"old check commit", sha, oldSHA, domain.PRCheckFailed, "failure", false, nil},
		{"queued failure", sha, sha, domain.PRCheckQueued, "failure", false, nil},
		{"running failure", sha, sha, domain.PRCheckInProgress, "failure", false, nil},
		{"current success", sha, sha, domain.PRCheckPassed, "success", false, nil},
		{"session read error", sha, sha, domain.PRCheckFailed, "failure", false, readErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			work := domain.InstructorWork{ID: "work", ProjectID: "project", SHA: sha, PolicyVersion: "policy", SessionIDs: []domain.SessionID{"worker"}, Launches: []domain.InstructorLaunch{{SessionID: "worker", Role: "worker", Status: "launched", Requested: domain.InstructorAgentSelection{Harness: "opencode", Model: "requested-model", Effort: "xhigh"}}}}
			store := instructorDiscoveryNativeStore{
				instructorTraceabilityStore: instructorTraceabilityStore{
					evidenceStoreFake: evidenceStoreFake{
						prs:    []domain.PullRequest{{URL: prURL, HeadSHA: test.prSHA}},
						checks: []domain.PullRequestCheck{{CommitHash: test.checkSHA, Status: test.status, Conclusion: test.conclusion}},
					},
					// An unavailable row must not supply native facts, even if
					// the store returns populated fields alongside found=false.
					session: domain.SessionRecord{ID: "worker", Harness: domain.HarnessOpenCode, Metadata: domain.SessionMetadata{Model: "unavailable-native-model", Effort: "ultra"}},
				},
				err: test.readErr,
			}
			claims, err := (instructorEvidenceVerifier{store: store}).Discover(context.Background(), work)
			if !errors.Is(err, test.readErr) {
				t.Fatalf("expected session read error %v, got %v", test.readErr, err)
			}
			wantKinds := map[string]bool{}
			if test.readErr == nil && test.prSHA == sha {
				wantKinds["ci"], wantKinds["traceability"] = true, true
				if test.wantFailed {
					wantKinds["ci-failed"] = true
				}
			}
			if len(claims) != len(wantKinds) {
				t.Fatalf("expected SCM kinds %v without attribution/task-packet, got %+v", wantKinds, claims)
			}
			for _, claim := range claims {
				if !wantKinds[claim.Kind] {
					t.Fatalf("unexpected or duplicate discovered kind: %+v", claim)
				}
				delete(wantKinds, claim.Kind)
				if claim.WorkID != work.ID || claim.PolicyVersion != work.PolicyVersion || claim.SHA != sha || claim.Ref != prURL || claim.Producer != "worker" || claim.Role != "worker" || claim.Verified || claim.Outcome != "" {
					t.Fatalf("SCM discovery changed the evidence identity or granted verification: %+v", claim)
				}
				if claim.Harness != "" || claim.Model != "" || claim.Effort != "" {
					t.Fatalf("SCM discovery fabricated unavailable or requested native configuration: %+v", claim)
				}
			}
		})
	}
}

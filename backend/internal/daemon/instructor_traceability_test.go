package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type instructorTraceabilityStore struct {
	evidenceStoreFake
	session domain.SessionRecord
}

func (s instructorTraceabilityStore) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	return s.session, s.session.ID == id, nil
}

func instructorAttributionProvenance(work *domain.InstructorWork, evidence *domain.InstructorEvidence, store *instructorTraceabilityStore) {
	selection := domain.InstructorAgentSelection{Harness: "opencode", Model: "native-model", Effort: "ultra"}
	work.Policy.Roles = map[string]domain.InstructorRole{"worker": {Harness: "opencode", Model: "native-model", Effort: "maximum", EffortByModel: map[string]string{"native-model": "ultra"}}}
	work.Launches = []domain.InstructorLaunch{{SessionID: "worker", Role: "worker", Requested: selection, Sent: &selection, Status: "launched"}}
	evidence.Role = "worker"
	evidence.Harness, evidence.Model, evidence.Effort = selection.Harness, selection.Model, selection.Effort
	store.session.Kind = domain.KindWorker
	store.session.Harness = domain.HarnessOpenCode
	store.session.Metadata.Model, store.session.Metadata.Effort = selection.Model, selection.Effort
}

func TestInstructorAttributionVerifiesExactCommitWithoutRewriting(t *testing.T) {
	repo := instructorTestRepository(t)
	message := "implemented\n\nCo-authored-by: AO Worker <ao@example.test>\nAO-session: worker\nIssue: #12\n"
	instructorTestGit(t, repo, "-c", "user.name=Operator", "-c", "user.email=operator@example.test", "commit", "--allow-empty", "-m", message)
	sha := instructorTestGit(t, repo, "rev-parse", "HEAD")
	work := domain.InstructorWork{ID: "work", ProjectID: "project", SHA: sha, IssueID: "#12", SessionIDs: []domain.SessionID{"worker"}, Policy: domain.InstructorPolicy{Attribution: domain.InstructorAttribution{AuthorName: "Operator", AuthorEmail: "operator@example.test", CoAuthorName: "AO Worker", CoAuthorEmail: "ao@example.test"}}}
	store := instructorTraceabilityStore{session: domain.SessionRecord{ID: "worker", ProjectID: "project", Metadata: domain.SessionMetadata{WorkspaceRepoPath: repo}}}
	evidence := domain.InstructorEvidence{Producer: "worker"}
	instructorAttributionProvenance(&work, &evidence, &store)
	verifier := instructorEvidenceVerifier{store: store}
	if err := verifier.verifyAttribution(context.Background(), work, evidence); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*domain.InstructorWork){
		func(w *domain.InstructorWork) { w.Policy.Attribution.AuthorName = "Agent" },
		func(w *domain.InstructorWork) { w.Policy.Attribution.AuthorEmail = "" },
		func(w *domain.InstructorWork) { w.Policy.Attribution.CoAuthorEmail = "someone@example.test" },
		func(w *domain.InstructorWork) { w.IssueID = "#13" },
		func(w *domain.InstructorWork) { w.SHA = strings.Repeat("f", 40) },
	} {
		candidate := work
		mutate(&candidate)
		if err := verifier.verifyAttribution(context.Background(), candidate, evidence); err == nil {
			t.Fatalf("accepted mismatched attribution: %+v", candidate)
		}
	}
	if after := instructorTestGit(t, repo, "rev-parse", "HEAD"); after != sha {
		t.Fatalf("verification rewrote history: %s", after)
	}
}

func TestInstructorAttributionRejectsBodyTextOutsideTrailerBlock(t *testing.T) {
	repo := instructorTestRepository(t)
	message := "implemented\n\nCo-authored-by: AO Worker <ao@example.test>\nAO-session: worker\nIssue: #12\n\nThis is body text after the apparent trailers."
	instructorTestGit(t, repo, "-c", "user.name=Operator", "-c", "user.email=operator@example.test", "commit", "--allow-empty", "-m", message)
	work := domain.InstructorWork{ProjectID: "project", SHA: instructorTestGit(t, repo, "rev-parse", "HEAD"), IssueID: "#12", SessionIDs: []domain.SessionID{"worker"}, Policy: domain.InstructorPolicy{Attribution: domain.InstructorAttribution{AuthorName: "Operator", AuthorEmail: "operator@example.test", CoAuthorName: "AO Worker", CoAuthorEmail: "ao@example.test"}}}
	store := instructorTraceabilityStore{session: domain.SessionRecord{ID: "worker", ProjectID: "project", Metadata: domain.SessionMetadata{WorkspacePath: repo}}}
	evidence := domain.InstructorEvidence{Producer: "worker"}
	instructorAttributionProvenance(&work, &evidence, &store)
	if err := (instructorEvidenceVerifier{store: store}).verifyAttribution(context.Background(), work, evidence); err == nil || !strings.Contains(err.Error(), "required AO co-author") {
		t.Fatal("body text was accepted as attribution trailers")
	}
}

func TestInstructorAttributionRejectsRoleAndNativeLaunchImpersonation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*domain.InstructorWork, *domain.InstructorEvidence, *instructorTraceabilityStore)
	}{
		{"orchestrator role", func(_ *domain.InstructorWork, e *domain.InstructorEvidence, _ *instructorTraceabilityStore) {
			e.Role = "orchestrator"
		}},
		{"actual orchestrator", func(_ *domain.InstructorWork, _ *domain.InstructorEvidence, s *instructorTraceabilityStore) {
			s.session.Kind = domain.KindOrchestrator
		}},
		{"no launch", func(w *domain.InstructorWork, _ *domain.InstructorEvidence, _ *instructorTraceabilityStore) {
			w.Launches = nil
		}},
		{"unsent configuration", func(w *domain.InstructorWork, _ *domain.InstructorEvidence, _ *instructorTraceabilityStore) {
			w.Launches[0].Sent = nil
		}},
		{"wrong effort", func(_ *domain.InstructorWork, e *domain.InstructorEvidence, _ *instructorTraceabilityStore) {
			e.Effort = "minimal"
		}},
		{"runtime changed", func(_ *domain.InstructorWork, _ *domain.InstructorEvidence, s *instructorTraceabilityStore) {
			s.session.Metadata.Effort = "minimal"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			work := domain.InstructorWork{ProjectID: "project", SessionIDs: []domain.SessionID{"worker"}}
			evidence := domain.InstructorEvidence{Producer: "worker"}
			store := instructorTraceabilityStore{session: domain.SessionRecord{ID: "worker", ProjectID: "project"}}
			instructorAttributionProvenance(&work, &evidence, &store)
			test.mutate(&work, &evidence, &store)
			if err := (instructorEvidenceVerifier{store: store}).verifyAttributionWorker(context.Background(), work, evidence); err == nil {
				t.Fatal("unproven worker configuration accepted")
			}
		})
	}
}

type instructorTraceabilityFakeReader struct {
	responses map[string]any
	err       error
	calls     [][]string
}

func (f *instructorTraceabilityFakeReader) ReadJSON(_ context.Context, args []string, dst any) error {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return f.err
	}
	key := args[len(args)-1]
	if len(args) > 5 && args[5] == "graphql" {
		key = "graphql"
		if !strings.HasPrefix(args[len(args)-1], "query=query {") || strings.Contains(args[len(args)-1], "mutation") {
			return errors.New("not a readonly query")
		}
	}
	value, found := f.responses[key]
	if !found {
		return fmt.Errorf("unexpected provider read: %v", args)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func instructorTraceabilityFixture() (domain.InstructorWork, domain.InstructorEvidence, instructorTraceabilityStore, *instructorTraceabilityFakeReader) {
	sha := strings.Repeat("a", 40)
	work := domain.InstructorWork{ID: "work", ProjectID: "project", SHA: sha, Branch: "feature", Phase: "closeout", IssueID: "#12", PolicyVersion: "policy", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), SessionIDs: []domain.SessionID{"worker"}, Policy: domain.InstructorPolicy{Traceability: domain.InstructorTraceability{Required: true, RequireAssignee: true, AllowedRepositories: []string{"fork/repo"}, AllowedBaseBranches: []string{"main", "staging"}, RequiredPhases: []string{"start", "review"}, ProjectNumber: 3, BoardStatuses: map[string]string{"closeout": "Done"}}}}
	instructorTestPublicationGrant(&work)
	evidence := domain.InstructorEvidence{Producer: "worker", Ref: "https://github.com/fork/repo/pull/1"}
	store := instructorTraceabilityStore{evidenceStoreFake: evidenceStoreFake{prs: []domain.PullRequest{{URL: evidence.Ref, HeadSHA: sha, Repo: "fork/repo"}}}}
	comments := []instructorGitHubComment{}
	for _, phase := range work.Policy.Traceability.RequiredPhases {
		data, _ := json.Marshal(instructorPhaseComment{WorkID: work.ID, PolicyVersion: work.PolicyVersion, SHA: sha, Phase: phase, SessionID: "worker"})
		comments = append(comments, instructorGitHubComment{Body: "<!-- ao-phase:v1 -->\n```json\n" + string(data) + "\n```", CreatedAt: "2026-10-02T00:00:00Z"})
	}
	reader := &instructorTraceabilityFakeReader{responses: map[string]any{
		"repos/fork/repo/pulls/1":                                map[string]any{"html_url": evidence.Ref, "base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "fork/repo"}}, "head": map[string]any{"sha": sha, "ref": "feature"}},
		"repos/fork/repo/issues/12":                              map[string]any{"number": 12, "html_url": "https://github.com/fork/repo/issues/12", "assignees": []any{map[string]any{"login": "operator"}}},
		"repos/fork/repo/issues/12/comments?per_page=100&page=1": comments,
		"graphql": map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{"projectItems": map[string]any{"nodes": []any{map[string]any{"project": map[string]any{"number": 3, "url": "https://github.com/users/fork/projects/3"}, "status": map[string]any{"name": "Done"}}}, "pageInfo": map[string]any{"hasNextPage": false}}}}}},
	}}
	return work, evidence, store, reader
}

func TestInstructorTraceabilityUsesProviderIssueBoardAndExactForkCommit(t *testing.T) {
	work, evidence, store, reader := instructorTraceabilityFixture()
	if err := (instructorEvidenceVerifier{store: store}).verifyTraceabilityWith(context.Background(), work, evidence, reader); err != nil {
		t.Fatal(err)
	}
	if len(reader.calls) != 4 {
		t.Fatalf("calls=%v", reader.calls)
	}
}

func TestInstructorTraceabilityRejectsMissingAndStaleProviderEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*domain.InstructorWork, *domain.InstructorEvidence, *instructorTraceabilityFakeReader)
	}{
		{"unconfigured fork", func(w *domain.InstructorWork, _ *domain.InstructorEvidence, _ *instructorTraceabilityFakeReader) {
			w.Policy.Traceability.AllowedRepositories = nil
		}},
		{"upstream destination", func(_ *domain.InstructorWork, e *domain.InstructorEvidence, _ *instructorTraceabilityFakeReader) {
			e.Ref = "https://github.com/upstream/repo/pull/1"
		}},
		{"arbitrary endpoint", func(_ *domain.InstructorWork, e *domain.InstructorEvidence, _ *instructorTraceabilityFakeReader) {
			e.Ref = "https://attacker.test/fork/repo/pull/1"
		}},
		{"missing assignee", func(_ *domain.InstructorWork, _ *domain.InstructorEvidence, r *instructorTraceabilityFakeReader) {
			r.responses["repos/fork/repo/issues/12"] = map[string]any{"number": 12, "html_url": "https://github.com/fork/repo/issues/12"}
		}},
		{"missing board config", func(w *domain.InstructorWork, _ *domain.InstructorEvidence, _ *instructorTraceabilityFakeReader) {
			w.Policy.Traceability.ProjectNumber = 0
		}},
		{"wrong project owner", func(w *domain.InstructorWork, _ *domain.InstructorEvidence, _ *instructorTraceabilityFakeReader) {
			w.Policy.Traceability.ProjectOwner = "other"
		}},
		{"missing phase", func(_ *domain.InstructorWork, _ *domain.InstructorEvidence, r *instructorTraceabilityFakeReader) {
			r.responses["repos/fork/repo/issues/12/comments?per_page=100&page=1"] = []instructorGitHubComment{}
		}},
		{"stale provider head", func(_ *domain.InstructorWork, _ *domain.InstructorEvidence, r *instructorTraceabilityFakeReader) {
			pr := r.responses["repos/fork/repo/pulls/1"].(map[string]any)
			pr["head"] = map[string]any{"sha": strings.Repeat("b", 40), "ref": "feature"}
		}},
		{"unapproved base branch", func(_ *domain.InstructorWork, _ *domain.InstructorEvidence, r *instructorTraceabilityFakeReader) {
			pr := r.responses["repos/fork/repo/pulls/1"].(map[string]any)
			pr["base"] = map[string]any{"ref": "release", "repo": map[string]any{"full_name": "fork/repo"}}
		}},
		{"multiple boards", func(_ *domain.InstructorWork, _ *domain.InstructorEvidence, r *instructorTraceabilityFakeReader) {
			board := r.responses["graphql"].(map[string]any)
			items := board["data"].(map[string]any)["repository"].(map[string]any)["issue"].(map[string]any)["projectItems"].(map[string]any)
			items["nodes"] = append(items["nodes"].([]any), map[string]any{"project": map[string]any{"number": 4, "url": "https://github.com/users/fork/projects/4"}})
		}},
		{"stale phase policy", func(w *domain.InstructorWork, _ *domain.InstructorEvidence, _ *instructorTraceabilityFakeReader) {
			w.PolicyVersion = "new"
		}},
		{"provider unavailable", func(_ *domain.InstructorWork, _ *domain.InstructorEvidence, r *instructorTraceabilityFakeReader) {
			r.err = errors.New("provider unavailable")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			work, evidence, store, reader := instructorTraceabilityFixture()
			test.mutate(&work, &evidence, reader)
			if err := (instructorEvidenceVerifier{store: store}).verifyTraceabilityWith(context.Background(), work, evidence, reader); err == nil {
				t.Fatal("unsupported or stale evidence was accepted")
			}
		})
	}
}

func TestInstructorTraceabilityRetainsActualHistoricalAncestorSHA(t *testing.T) {
	repo := instructorTestRepository(t)
	oldSHA := instructorTestGit(t, repo, "rev-parse", "HEAD")
	instructorTestGit(t, repo, "-c", "user.name=Operator", "-c", "user.email=operator@example.test", "commit", "--allow-empty", "-m", "fix")
	finalSHA := instructorTestGit(t, repo, "rev-parse", "HEAD")
	work, evidence, store, reader := instructorTraceabilityFixture()
	work.SHA = finalSHA
	instructorTestPublicationGrant(&work)
	store.prs[0].HeadSHA = finalSHA
	store.session = domain.SessionRecord{ID: "worker", ProjectID: work.ProjectID, Metadata: domain.SessionMetadata{WorkspacePath: repo}}
	pr := reader.responses["repos/fork/repo/pulls/1"].(map[string]any)
	pr["head"] = map[string]any{"sha": finalSHA, "ref": "feature"}
	comments := reader.responses["repos/fork/repo/issues/12/comments?per_page=100&page=1"].([]instructorGitHubComment)
	for i := range comments {
		comments[i].Body = strings.ReplaceAll(comments[i].Body, strings.Repeat("a", 40), oldSHA)
	}
	reader.responses["repos/fork/repo/issues/12/comments?per_page=100&page=1"] = comments
	if err := (instructorEvidenceVerifier{store: store}).verifyTraceabilityWith(context.Background(), work, evidence, reader); err != nil {
		t.Fatal(err)
	}
	comments[1].Body = strings.ReplaceAll(comments[1].Body, oldSHA, strings.Repeat("f", 40))
	reader.responses["repos/fork/repo/issues/12/comments?per_page=100&page=1"] = comments
	if err := (instructorEvidenceVerifier{store: store}).verifyTraceabilityWith(context.Background(), work, evidence, reader); err == nil {
		t.Fatal("nonexistent historical commit accepted")
	}
}

func TestInstructorTraceabilityRejectsPhaseOrderAndOldFinalCheckpoint(t *testing.T) {
	work, evidence, store, reader := instructorTraceabilityFixture()
	comments := reader.responses["repos/fork/repo/issues/12/comments?per_page=100&page=1"].([]instructorGitHubComment)
	comments[0].CreatedAt = "2026-10-03T00:00:00Z"
	reader.responses["repos/fork/repo/issues/12/comments?per_page=100&page=1"] = comments
	if err := (instructorEvidenceVerifier{store: store}).verifyTraceabilityWith(context.Background(), work, evidence, reader); err == nil {
		t.Fatal("review before start accepted")
	}
	work, evidence, store, reader = instructorTraceabilityFixture()
	work.Policy.Traceability.RequiredPhases = append(work.Policy.Traceability.RequiredPhases, "delta")
	phase := instructorPhaseComment{WorkID: work.ID, PolicyVersion: work.PolicyVersion, SHA: strings.Repeat("b", 40), Phase: "delta", SessionID: "worker"}
	data, _ := json.Marshal(phase)
	comments = reader.responses["repos/fork/repo/issues/12/comments?per_page=100&page=1"].([]instructorGitHubComment)
	comments = append(comments, instructorGitHubComment{Body: "<!-- ao-phase:v1 -->\n```json\n" + string(data) + "\n```", CreatedAt: "2026-10-03T00:00:00Z"})
	reader.responses["repos/fork/repo/issues/12/comments?per_page=100&page=1"] = comments
	if err := (instructorEvidenceVerifier{store: store}).verifyTraceabilityWith(context.Background(), work, evidence, reader); err == nil {
		t.Fatal("old delta SHA accepted")
	}
}

func TestInstructorRepositoryPolicyExampleLoadsWithLockedEvidence(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := instructorsvc.LoadPolicy(root, "docs/artifaktory/instructor/POLICY.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := instructorsvc.ValidateLockedPolicy(policy); err != nil {
		t.Fatal(err)
	}
	if !instructorContains(policy.CloseoutRequires, "attribution") || !instructorContains(policy.CloseoutRequires, "traceability") || policy.Traceability.ProjectNumber <= 0 {
		t.Fatalf("policy=%+v", policy)
	}
}

func TestInstructorWorkInstructionsContainActionablePinnedEvidenceProtocol(t *testing.T) {
	work, _, _, _ := instructorTraceabilityFixture()
	work.Name, work.Goal, work.Phase = "Worker handoff", "Preserve a distinct delegated task", "implementation"
	text := instructorWorkInstructions(work)
	for _, required := range []string{work.ID, work.PolicyVersion, work.Goal, `"phase":"implementation"`, "ao spawn --prompt", "own concrete subtask", "do not create a duplicate worker", "queued, delivered, accepted or actually running", "ao instructor checks", "ao instructor verify", "<!-- ao-phase:v1 -->", "Co-authored-by", "AO-session", "Issue", "explicitly configured fork", "allowedRepositories", "projectNumber"} {
		if !strings.Contains(text, required) {
			t.Fatalf("instructions missing %q", required)
		}
	}
}

func TestInstructorPhaseCommentAcceptsWindowsLineEndings(t *testing.T) {
	body := "Actual phase completed.\r\n<!-- ao-phase:v1 -->\r\n```json\r\n{\"workId\":\"work\",\"policyVersion\":\"policy\",\"sha\":\"" + strings.Repeat("a", 40) + "\",\"phase\":\"review\",\"sessionId\":\"worker\"}\r\n```"
	phase, ok := instructorParsePhaseComment(body)
	if !ok || phase.Phase != "review" || phase.WorkID != "work" {
		t.Fatalf("phase=%+v ok=%v", phase, ok)
	}
}

func TestInstructorTraceabilityRejectsUnapprovedRemotePublication(t *testing.T) {
	work, evidence, store, reader := instructorTraceabilityFixture()
	work.PublicationGrants = nil
	if err := (instructorEvidenceVerifier{store: store}).verifyTraceabilityWith(context.Background(), work, evidence, reader); err == nil {
		t.Fatal("remote publication without grant was certified")
	}
	if len(reader.calls) != 0 {
		t.Fatal("unapproved publication triggered deep remote reads")
	}
}

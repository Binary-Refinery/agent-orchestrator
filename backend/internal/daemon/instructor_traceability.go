package daemon

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func (v instructorEvidenceVerifier) attributionWorkspace(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence) (string, error) {
	if !workHasSession(work, evidence.Producer) {
		return "", evidenceInvalid("evidence producer is not a work member")
	}
	session, found, err := v.store.GetSession(ctx, evidence.Producer)
	if err != nil {
		return "", err
	}
	if !found || session.ProjectID != work.ProjectID {
		return "", evidenceInvalid("evidence producer has no matching project workspace")
	}
	path := session.Metadata.WorkspaceRepoPath
	if path == "" {
		path = session.Metadata.WorkspacePath
	}
	if path == "" {
		return "", evidenceInvalid("evidence producer has no recorded repository")
	}
	return filepath.Clean(path), nil
}

func (v instructorEvidenceVerifier) verifyAttribution(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence) error {
	if err := v.verifyAttributionWorker(ctx, work, evidence); err != nil {
		return err
	}
	policy := work.Policy.Attribution
	if strings.TrimSpace(policy.AuthorName) == "" || strings.TrimSpace(policy.AuthorEmail) == "" || strings.TrimSpace(policy.CoAuthorName) == "" || strings.TrimSpace(policy.CoAuthorEmail) == "" {
		return evidenceInvalid("operator author and AO co-author identities must be configured in the pinned attribution policy")
	}
	if work.IssueID == "" {
		return evidenceInvalid("attribution requires the work's stable issue identity")
	}
	path, err := v.attributionWorkspace(ctx, work, evidence)
	if err != nil {
		return err
	}
	commit, err := instructorReadGit(ctx, path, "show", "-s", "--format=%H%x00%an%x00%ae%x00%B", work.SHA+"^{commit}")
	if err != nil {
		return fmt.Errorf("exact commit attribution is unavailable: %w", err)
	}
	parts := strings.SplitN(string(commit), "\x00", 4)
	if len(parts) != 4 || parts[0] != work.SHA || parts[1] != policy.AuthorName || parts[2] != policy.AuthorEmail {
		return evidenceInvalid("exact commit author does not match the configured operator")
	}
	// Git itself parses the trailer block: a matching sentence in the body is
	// insufficient and cannot satisfy the attribution requirement.
	trailers, err := instructorReadGit(ctx, path, "show", "-s", "--format=%(trailers:only,unfold)", work.SHA+"^{commit}")
	if err != nil {
		return err
	}
	parsed := map[string][]string{}
	for _, line := range strings.Split(string(trailers), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found {
			key = strings.ToLower(strings.TrimSpace(key))
			parsed[key] = append(parsed[key], strings.TrimSpace(value))
		}
	}
	sessionKey, issueKey := policy.SessionTrailer, policy.IssueTrailer
	if sessionKey == "" {
		sessionKey = "AO-session"
	}
	if issueKey == "" {
		issueKey = "Issue"
	}
	if !instructorExactTrailer(parsed, "co-authored-by", policy.CoAuthorName+" <"+policy.CoAuthorEmail+">") || !instructorExactTrailer(parsed, sessionKey, string(evidence.Producer)) || !instructorExactTrailer(parsed, issueKey, string(work.IssueID)) {
		return evidenceInvalid("exact commit lacks the required AO co-author, AO-session or Issue trailer")
	}
	return nil
}

func (v instructorEvidenceVerifier) verifyAttributionWorker(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence) error {
	if evidence.Role != "worker" || !workHasSession(work, evidence.Producer) {
		return evidenceInvalid("attribution must name the attached worker role")
	}
	session, found, err := v.store.GetSession(ctx, evidence.Producer)
	if err != nil {
		return err
	}
	if !found || session.Kind != domain.KindWorker || session.ProjectID != work.ProjectID {
		return evidenceInvalid("attribution producer is not an actual worker of this project")
	}
	role, found := work.Policy.Roles["worker"]
	if !found {
		return evidenceInvalid("attribution worker role is missing from the pinned policy")
	}
	for n := len(work.Launches) - 1; n >= 0; n-- {
		launch := work.Launches[n]
		if launch.SessionID != evidence.Producer || launch.Role != "worker" {
			continue
		}
		selection := launch.Requested
		if (launch.Status != "launched" && launch.Status != "sent") || launch.Sent == nil || *launch.Sent != selection || selection.Model == "" || selection.Effort == "" {
			return evidenceInvalid("attribution worker lacks a successful native launch record")
		}
		expectedEffort := role.Effort
		if expectedEffort == "maximum" {
			expectedEffort = role.EffortByModel[selection.Model]
		}
		if selection.Harness != role.Harness || role.Model != "" && selection.Model != role.Model || expectedEffort == "" || expectedEffort != "configured" && selection.Effort != expectedEffort {
			return evidenceInvalid("attribution worker launch does not satisfy the pinned model and effort")
		}
		if string(session.Harness) != selection.Harness || session.Metadata.Model != selection.Model || session.Metadata.Effort != selection.Effort || evidence.Harness != selection.Harness || evidence.Model != selection.Model || evidence.Effort != selection.Effort {
			return evidenceInvalid("attribution evidence differs from the actual worker native configuration")
		}
		return nil
	}
	return evidenceInvalid("attribution worker has no managed launch provenance")
}

func instructorExactTrailer(trailers map[string][]string, key, expected string) bool {
	values := trailers[strings.ToLower(key)]
	// Conflicting duplicate identity trailers are not accepted.
	return len(values) == 1 && values[0] == expected
}

type instructorTraceabilityReader interface {
	ReadJSON(context.Context, []string, any) error
}

// Instructions exposes the pinned, actionable contract to real managed
// controllers. Enforcement still belongs to the verifier and workflow engine.
func (g instructorGovernance) Instructions(ctx context.Context, workID string) (string, error) {
	work, found, err := g.store.GetInstructorWork(ctx, workID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("managed work instructions are unavailable")
	}
	return instructorWorkInstructions(work), nil
}

func instructorWorkInstructions(work domain.InstructorWork) string {
	policy, _ := json.Marshal(work.Policy)
	phase, _ := json.Marshal(instructorPhaseComment{WorkID: work.ID, PolicyVersion: work.PolicyVersion, SHA: "ACTUAL_FINAL_40_CHARACTER_SHA", Phase: "ACTUAL_CHECKPOINT_NAME", SessionID: "ACTUAL_ATTACHED_SESSION_ID"})
	return fmt.Sprintf(`Durable work %s; issue %s; pinned policy %s.
Read ao instructor details %q before delegating. Preserve --work %q and use the exact role's native model/effort. Report actual acceptance separately from progress, with the Instructor dispatch identifier. A finished process is not accepted work.
Build and commit the task packet at %q; execute the configured push/full and additional required check profiles through ao instructor checks %q --session ACTUAL_WORKER_SESSION --profile push,full, obtain the independent recorded review and triage, repair only authorized findings, and obtain a fresh approved delta review at the final SHA. Never attest a review/check result yourself.
Commit new changes with the configured operator author and exact Co-authored-by, AO-session and Issue trailers; never rewrite existing history to manufacture attribution. Stay within the explicitly configured fork/base destinations and exact human publication grants. Associate the real issue, assignee and tracked PR with this work.
After actual start, commit, push, pr, ci, review, fix, delta and closeout checkpoints, post factual issue comments and update the configured project board through your authorized tracker workflow. Include this block, replacing every ACTUAL placeholder:
<!-- ao-phase:v1 -->
%s
Historical start/review/fix checkpoint comments retain their actual immutable SHA; it must be an ancestor of the final work SHA. Commit/push/pr/ci/delta/closeout checkpoints must refer to the final SHA. Never restate a historical phase as if it ran against a later commit. A no-change correction still needs a documented result and independent delta review.
Submit task-packet/ci/review/triage/delta/attribution/traceability evidence using ao instructor verify %q --input evidence.json with workId,policyVersion,sha,kind,producer,role,ref; attribution producer is the commit's attached worker, traceability ref is the tracked PR URL. Missing tooling, board access, assignee, artifacts or exact identity is a blocker to report, never a verified claim.
Pinned policy JSON:
%s`, work.ID, work.IssueID, work.PolicyVersion, work.ID, work.ID, ".ao/task-packets/"+work.ID+".md", work.ID, "```json\n"+string(phase)+"\n```", work.ID, policy)
}

// This adapter uses the existing user's gh credential selection, only GET/view
// operations and a fixed read-only GraphQL query on github.com. It performs no publication,
// comments, board changes, credential reads or history rewrites.
type instructorGitHubTraceability struct{}

func (instructorGitHubTraceability) ReadJSON(ctx context.Context, args []string, dst any) error {
	data, err := instructorReadCommand(ctx, "", "gh", args...)
	if err != nil {
		return fmt.Errorf("GitHub traceability read unavailable (gh access is required): %w", err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return errors.New("GitHub traceability response is invalid")
	}
	return nil
}

type instructorGitHubIssue struct {
	Number    int    `json:"number"`
	HTMLURL   string `json:"html_url"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}
type instructorGitHubComment struct {
	Body      string `json:"body"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
}
type instructorPhaseComment struct {
	WorkID        string           `json:"workId"`
	PolicyVersion string           `json:"policyVersion"`
	SHA           string           `json:"sha"`
	Phase         string           `json:"phase"`
	SessionID     domain.SessionID `json:"sessionId"`
}
type instructorGitHubPR struct {
	HTMLURL string `json:"html_url"`
	Base    struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
	Head struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
}
type instructorGitHubBoard struct {
	Data struct {
		Repository struct {
			Issue struct {
				ProjectItems struct {
					Nodes []struct {
						Status struct {
							Name string `json:"name"`
						} `json:"status"`
						Project struct {
							Number int    `json:"number"`
							URL    string `json:"url"`
						} `json:"project"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"projectItems"`
			} `json:"issue"`
		} `json:"repository"`
	} `json:"data"`
	Errors []json.RawMessage `json:"errors"`
}

func (v instructorEvidenceVerifier) verifyTraceability(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence) error {
	return v.verifyTraceabilityWith(ctx, work, evidence, instructorGitHubTraceability{})
}

func (v instructorEvidenceVerifier) verifyTraceabilityWith(ctx context.Context, work domain.InstructorWork, evidence domain.InstructorEvidence, reader instructorTraceabilityReader) error {
	if !instructorsvc.HasPublicationGrant(work) {
		return evidenceInvalid("remote traceability requires the exact recorded publication approval")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	policy := work.Policy.Traceability
	if !workHasSession(work, evidence.Producer) || work.IssueID == "" {
		return evidenceInvalid("traceability requires a work member and stable issue identity")
	}
	repo, number, err := instructorGitHubReference(evidence.Ref, "pull")
	if err != nil || !instructorContainsFold(policy.AllowedRepositories, repo) {
		return evidenceInvalid("PR destination is not an exact configured fork repository")
	}
	prs, err := v.store.ListPRsBySession(ctx, evidence.Producer)
	if err != nil {
		return err
	}
	found := false
	for _, pr := range prs {
		if pr.URL == evidence.Ref && pr.HeadSHA == work.SHA && strings.EqualFold(pr.Repo, repo) {
			found = true
		}
	}
	if !found {
		return evidenceInvalid("traceability PR is not attached to this work at its exact commit")
	}
	var pr instructorGitHubPR
	if err := reader.ReadJSON(ctx, instructorGitHubAPI(fmt.Sprintf("repos/%s/pulls/%d", repo, number)), &pr); err != nil {
		return err
	}
	if pr.HTMLURL != evidence.Ref || !strings.EqualFold(pr.Base.Repo.FullName, repo) || pr.Head.SHA != work.SHA || pr.Head.Ref != work.Branch || !instructorContains(policy.AllowedBaseBranches, pr.Base.Ref) {
		return evidenceInvalid("provider PR commit, source branch or target fork/base does not match the pinned work")
	}
	issueNumber, err := instructorIssueNumber(string(work.IssueID), repo)
	if err != nil {
		return err
	}
	var issue instructorGitHubIssue
	issuePath := fmt.Sprintf("repos/%s/issues/%d", repo, issueNumber)
	if err := reader.ReadJSON(ctx, instructorGitHubAPI(issuePath), &issue); err != nil {
		return err
	}
	if issue.Number != issueNumber || issue.HTMLURL != fmt.Sprintf("https://github.com/%s/issues/%d", repo, issueNumber) {
		return evidenceInvalid("provider issue differs from the work's stable issue")
	}
	if policy.RequireAssignee && len(issue.Assignees) == 0 {
		return evidenceInvalid("work issue has no provider-confirmed assignee")
	}
	if label := policy.PhaseLabels[work.Phase]; label != "" {
		found := false
		for _, value := range issue.Labels {
			found = found || value.Name == label
		}
		if !found {
			return evidenceInvalid("work issue is missing its configured current phase label")
		}
	}
	if policy.Required {
		expected := policy.BoardStatuses[work.Phase]
		if policy.ProjectNumber <= 0 || expected == "" {
			return evidenceInvalid("required traceability needs an exact GitHub project and board status for the current phase")
		}
		var board instructorGitHubBoard
		parts := strings.Split(repo, "/")
		query := fmt.Sprintf(`query { repository(owner: %q, name: %q) { issue(number: %d) { projectItems(first: 100) { nodes { project { number url } status: fieldValueByName(name: "Status") { ... on ProjectV2ItemFieldSingleSelectValue { name } } } pageInfo { hasNextPage } } } } }`, parts[0], parts[1], issueNumber)
		if err := reader.ReadJSON(ctx, []string{"api", "--hostname", "github.com", "--method", "POST", "graphql", "--raw-field", "query=" + query}, &board); err != nil {
			return err
		}
		items := board.Data.Repository.Issue.ProjectItems
		if len(board.Errors) != 0 || items.PageInfo.HasNextPage {
			return errors.New("provider board response is unavailable or incomplete")
		}
		if len(items.Nodes) != 1 {
			return evidenceInvalid("work issue must belong to exactly one provider project board")
		}
		owner := policy.ProjectOwner
		if owner == "" {
			owner = parts[0]
		}
		found := false
		for _, item := range items.Nodes {
			projectURL, parseErr := url.Parse(item.Project.URL)
			projectParts := strings.Split(strings.Trim(projectURLPath(projectURL), "/"), "/")
			matches := parseErr == nil && projectURL != nil && projectURL.Scheme == "https" && projectURL.Host == "github.com" && len(projectParts) == 4 && (projectParts[0] == "orgs" || projectParts[0] == "users") && strings.EqualFold(projectParts[1], owner) && projectParts[2] == "projects" && projectParts[3] == strconv.Itoa(policy.ProjectNumber)
			found = found || (matches && item.Project.Number == policy.ProjectNumber && item.Status.Name == expected)
		}
		if !found {
			return evidenceInvalid("provider project board does not show the configured current phase")
		}
	}
	phaseTimes := map[string]time.Time{}
	ancestry := map[string]bool{}
	ancestorProbes := 0
	for page := 1; page <= 5; page++ {
		var comments []instructorGitHubComment
		if err := reader.ReadJSON(ctx, instructorGitHubAPI(fmt.Sprintf("%s/comments?per_page=100&page=%d", issuePath, page)), &comments); err != nil {
			return err
		}
		for _, comment := range comments {
			phase, ok := instructorParsePhaseComment(comment.Body)
			if !ok || phase.WorkID != work.ID || phase.PolicyVersion != work.PolicyVersion || !workHasSession(work, phase.SessionID) {
				continue
			}
			if phase.SHA != work.SHA {
				if phase.Phase != "start" && phase.Phase != "review" && phase.Phase != "fix" {
					continue
				}
				if len(phase.SHA) != 40 {
					continue
				}
				if _, err := hex.DecodeString(phase.SHA); err != nil {
					continue
				}
				valid, checked := ancestry[phase.SHA]
				if !checked {
					ancestorProbes++
					if ancestorProbes > 16 {
						return evidenceInvalid("historical phase ancestry exceeds bounded verification; evidence is incomplete")
					}
					path, err := v.attributionWorkspace(ctx, work, evidence)
					if err != nil {
						return err
					}
					_, err = instructorReadGit(ctx, path, "merge-base", "--is-ancestor", phase.SHA, work.SHA)
					valid = err == nil
					ancestry[phase.SHA] = valid
				}
				if !valid {
					continue
				}
			}
			createdAt, parseErr := time.Parse(time.RFC3339, comment.CreatedAt)
			if parseErr != nil || createdAt.Before(work.CreatedAt) {
				continue
			}
			if previous, found := phaseTimes[phase.Phase]; !found || createdAt.Before(previous) {
				phaseTimes[phase.Phase] = createdAt
			}
		}
		if len(comments) < 100 {
			break
		}
		if page == 5 {
			return evidenceInvalid("issue phase-comment history exceeds bounded verification; evidence is incomplete")
		}
	}
	for _, phase := range policy.RequiredPhases {
		if _, found := phaseTimes[phase]; !found {
			return evidenceInvalid(fmt.Sprintf("issue lacks a provider-observed %s phase comment for this work/policy and valid commit history", phase))
		}
	}
	for phase, at := range phaseTimes {
		if start, found := phaseTimes["start"]; found && at.Before(start) {
			return evidenceInvalid(fmt.Sprintf("%s phase comment predates work start", phase))
		}
	}
	for _, sequence := range [][]string{{"review", "fix", "delta", "closeout"}, {"commit", "push", "pr", "ci", "delta", "closeout"}} {
		var previous time.Time
		for _, phase := range sequence {
			at, found := phaseTimes[phase]
			if !found {
				continue
			}
			if !previous.IsZero() && at.Before(previous) {
				return evidenceInvalid(fmt.Sprintf("%s phase comment is out of workflow order", phase))
			}
			previous = at
		}
	}
	return nil
}

func projectURLPath(value *url.URL) string {
	if value == nil {
		return ""
	}
	return value.Path
}

func instructorGitHubAPI(path string) []string {
	return []string{"api", "--hostname", "github.com", "--method", "GET", path}
}

func instructorGitHubReference(ref, kind string) (string, int, error) {
	u, err := url.Parse(ref)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", 0, evidenceInvalid("reference must be an exact github.com URL")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != kind || !instructorSafeRepoPart(parts[0]) || !instructorSafeRepoPart(parts[1]) {
		return "", 0, evidenceInvalid("reference has invalid repository or resource")
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return "", 0, evidenceInvalid("reference number is invalid")
	}
	return parts[0] + "/" + parts[1], number, nil
}

func instructorSafeRepoPart(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9', char == '-', char == '_', char == '.':
			continue
		default:
			return false
		}
	}
	return true
}

func instructorIssueNumber(id, repo string) (int, error) {
	if strings.HasPrefix(id, "https://") {
		actual, number, err := instructorGitHubReference(id, "issues")
		if err != nil || !strings.EqualFold(actual, repo) {
			return 0, evidenceInvalid("issue is outside the configured fork destination")
		}
		return number, nil
	}
	if strings.Contains(id, "#") {
		prefix, suffix, _ := strings.Cut(id, "#")
		if prefix != "" && !strings.EqualFold(prefix, repo) {
			return 0, evidenceInvalid("issue is outside the configured fork destination")
		}
		id = suffix
	}
	number, err := strconv.Atoi(id)
	if err != nil || number <= 0 {
		return 0, evidenceInvalid("issue must be a number, repository#number or exact GitHub issue URL")
	}
	return number, nil
}

func instructorParsePhaseComment(body string) (instructorPhaseComment, bool) {
	var phase instructorPhaseComment
	body = strings.ReplaceAll(body, "\r\n", "\n")
	marker := "<!-- ao-phase:v1 -->"
	_, suffix, found := strings.Cut(body, marker)
	if !found {
		return phase, false
	}
	suffix = strings.TrimSpace(suffix)
	if !strings.HasPrefix(suffix, "```json\n") {
		return phase, false
	}
	content, _, found := strings.Cut(strings.TrimPrefix(suffix, "```json\n"), "\n```")
	if !found || len(content) > 8192 || json.Unmarshal([]byte(content), &phase) != nil {
		return phase, false
	}
	return phase, phase.Phase != ""
}

func instructorContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func instructorContainsFold(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(value, expected) {
			return true
		}
	}
	return false
}

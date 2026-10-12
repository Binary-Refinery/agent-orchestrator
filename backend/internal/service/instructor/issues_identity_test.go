package instructor_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	trackergithub "github.com/aoagents/agent-orchestrator/backend/internal/adapters/tracker/github"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorIssueCanonicalIdentitySurvivesActualTrackerRedirects(t *testing.T) {
	for _, tc := range []struct {
		name, targetRepository, canonicalURL string
		accepted                             bool
	}{
		{"transferred", "newowner/newrepo", "https://github.com/newowner/newrepo/issues/42", false},
		{"renamed", "source/renamed", "https://github.com/source/renamed/issues/42", false},
		{"same repository case", "Source/Repository", "https://github.com/Source/Repository/issues/42", true},
	} {
		for _, operation := range []string{"browse", "preview", "intake"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				f, _, _ := newIssueFixture(t)
				var redirects atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						t.Errorf("intake mutated GitHub: %s", r.Method)
					}
					suffix := ""
					if operation != "browse" {
						suffix = "/42"
					}
					if r.URL.Path == "/repos/source/repository/issues"+suffix {
						redirects.Add(1)
						http.Redirect(w, r, "/repos/"+tc.targetRepository+"/issues"+suffix, http.StatusMovedPermanently)
						return
					}
					if r.URL.Path != "/repos/"+tc.targetRepository+"/issues"+suffix {
						t.Errorf("unexpected tracker path: %s", r.URL.Path)
					}
					payload := fmt.Sprintf(`{"number":42,"title":"Actual redirected issue","body":"Keep its actual origin","state":"open","html_url":%q}`, tc.canonicalURL)
					if operation == "browse" {
						payload = "[" + payload + "]"
					}
					_, _ = w.Write([]byte(payload))
				}))
				t.Cleanup(server.Close)
				tracker, err := trackergithub.New(trackergithub.Options{BaseURL: server.URL, Token: trackergithub.StaticTokenSource("test"), HTTPClient: server.Client()})
				if err != nil {
					t.Fatal(err)
				}
				f.service.SetIssueTracker(tracker)
				var actualURL string
				switch operation {
				case "browse", "preview":
					var candidates domain.InstructorIssueCandidates
					if operation == "browse" {
						candidates, err = f.service.IssueCandidates(context.Background(), "governed")
					} else {
						candidates, err = f.service.IssuePreview(context.Background(), "governed", 42)
					}
					if len(candidates.Issues) == 1 {
						actualURL = candidates.Issues[0].URL
					}
				case "intake":
					var work domain.InstructorWork
					work, err = f.service.IntakeIssue(context.Background(), instructorsvc.IssueIntakeInput{ProjectID: "governed", Repository: "source/repository", Number: 42})
					if strings.Contains(work.Goal, `"url":"`+tc.canonicalURL+`"`) {
						actualURL = tc.canonicalURL
					}
				}
				if redirects.Load() != 1 {
					t.Fatalf("real tracker did not follow redirect: %d", redirects.Load())
				}
				if tc.accepted {
					if err != nil || actualURL != tc.canonicalURL {
						t.Fatalf("same-repository issue provenance lost: url=%q err=%v", actualURL, err)
					}
				} else {
					if !errors.Is(err, instructorsvc.ErrIssuesUnavailable) {
						t.Fatalf("admitted a different canonical repository: %v", err)
					}
					works, readErr := f.store.ListInstructorWorks(context.Background())
					if readErr != nil || len(works) != 0 {
						t.Fatalf("foreign repository created work: %+v %v", works, readErr)
					}
				}
			})
		}
	}
}

func TestInstructorIssueRejectsMalformedOrNonIssueCanonicalURLs(t *testing.T) {
	for _, canonicalURL := range []string{
		"", "://invalid", "http://github.com/source/repository/issues/42",
		"https://github.com.attacker.test/source/repository/issues/42",
		"https://github.com:443/source/repository/issues/42",
		"https://user@github.com/source/repository/issues/42",
		"https://github.com/source/repository/pull/42",
		"https://github.com/source/repository/issues/43",
		"https://github.com/source/repository/issues/42?other=1",
		"https://github.com/source/repository/issues/42?",
		"https://github.com/source/repository/issues/42#comment",
		"https://github.com/source/repository/issues/42#",
		"https://github.com/source/%72epository/issues/42",
	} {
		t.Run(canonicalURL, func(t *testing.T) {
			f, tracker, _ := newIssueFixture(t)
			tracker.issue.URL = canonicalURL
			tracker.issues = []domain.Issue{tracker.issue}
			if _, err := f.service.IssueCandidates(context.Background(), "governed"); !errors.Is(err, instructorsvc.ErrIssuesUnavailable) {
				t.Fatalf("invalid candidate URL accepted: %v", err)
			}
			if _, err := f.service.IntakeIssue(context.Background(), instructorsvc.IssueIntakeInput{ProjectID: "governed", Repository: "source/repository", Number: 42}); !errors.Is(err, instructorsvc.ErrIssuesUnavailable) {
				t.Fatalf("invalid work URL accepted: %v", err)
			}
		})
	}
}

func TestInstructorIssueIntakeRejectsConflictingSavedSourceWithoutChangingProvenance(t *testing.T) {
	f, _, _ := newIssueFixture(t)
	ctx := context.Background()
	existing, err := f.service.Register(ctx, instructorsvc.RegisterInput{ProjectID: "governed", Name: "Different issue identity", Goal: "Task for another issue", Source: "issue", SourceRef: "source/repository#42", IssueID: "other/repository#99"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.IntakeIssue(ctx, instructorsvc.IssueIntakeInput{ProjectID: "governed", Repository: "source/repository", Number: 42})
	if !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("conflicting saved source returned as successful intake: %v", err)
	}
	works, err := f.store.ListInstructorWorks(ctx)
	if err != nil || len(works) != 1 || works[0].ID != existing.ID || works[0].IssueID != existing.IssueID || works[0].SourceRef != existing.SourceRef || works[0].Goal != existing.Goal || works[0].Revision != existing.Revision || works[0].PolicyVersion != existing.PolicyVersion {
		t.Fatalf("previous work identity rewritten: %+v %v", works, err)
	}
	events, err := f.store.ListInstructorEvents(ctx, existing.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("failed intake changed audit history: %+v %v", events, err)
	}
}

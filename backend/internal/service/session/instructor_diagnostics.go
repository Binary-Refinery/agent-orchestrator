package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

type instructorDiagnosticReader interface {
	CaptureInstructorDiagnostics(context.Context, domain.InstructorDiagnosticScope, bool, int64, int64) (domain.InstructorDiagnosticCapture, error)
}

// InstructorDiagnostics reads only persisted facts, atomically with the exact
// membership authority. It never starts or consults a native controller.
func (s *Service) InstructorDiagnostics(ctx context.Context, scope domain.InstructorDiagnosticScope, timeline bool, before, limit int64) (domain.InstructorMemberDiagnostics, error) {
	if strings.TrimSpace(scope.WorkID) == "" || scope.ProjectID == "" || scope.MemberID == "" || scope.SessionID == "" || scope.InstanceID == "" || before < 0 || limit < 1 || limit > 100 {
		return domain.InstructorMemberDiagnostics{}, apierr.Invalid("INSTRUCTOR_DIAGNOSTIC_SCOPE_REQUIRED", "Explicit work, project, member, session, instance, generation and bounded pagination are required", nil)
	}
	reader, ok := s.store.(instructorDiagnosticReader)
	if !ok {
		return domain.InstructorMemberDiagnostics{}, apierr.NotImplemented("INSTRUCTOR_DIAGNOSTICS_UNAVAILABLE", "Atomic persisted member diagnostics are unavailable")
	}
	captured, err := reader.CaptureInstructorDiagnostics(ctx, scope, timeline, before, limit)
	if errors.Is(err, domain.ErrInstructorProducerChanged) {
		return domain.InstructorMemberDiagnostics{}, apierr.Conflict("INSTRUCTOR_DIAGNOSTIC_SCOPE_CHANGED", "The captured work/member/project incarnation or generation no longer matches", nil)
	}
	if err != nil {
		return domain.InstructorMemberDiagnostics{}, err
	}
	return projectInstructorDiagnostics(scope, captured), nil
}

const diagnosticTextBytes = 4096
const diagnosticPageBytes = 128 * 1024

var diagnosticSecrets = []*regexp.Regexp{
	// Remove a partial PEM too: the closing marker may lie beyond the read cap.
	regexp.MustCompile(`(?s)-{5}BEGIN [A-Z ]*PRIVATE KEY-{5}.*`),
	regexp.MustCompile(`\b(?:github_pat_[A-Za-z0-9_]+|gh[pousr]_[A-Za-z0-9_]+|sk-(?:proj-)?[A-Za-z0-9_-]{12,}|AKIA[A-Z0-9]{16})\b`),
	regexp.MustCompile(`(?i)\b(?:authorization\s*[:=]\s*(?:bearer|basic)\s+|bearer\s+)[^\s"'<>\\]+`),
	regexp.MustCompile(`(?i)\b(?:cookie|set-cookie)["']?\s*[:=]\s*[^\r\n]+`),
	regexp.MustCompile(`(?is)\b(?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|password|passwd|authorization|token|secret)["']?\s*[:=]\s*(?:"(?:\\.|[^"\\])*(?:"|\\?$)|'(?:\\.|[^'\\])*(?:'|\\?$)|[^\s&;,]+)`),
}

var diagnosticURLs = regexp.MustCompile(`https?://[^\s<>"']+`)

// diagnosticText caps before parsing to keep even pathological provider output
// bounded. Known credentials and URL userinfo/query/fragment values are removed;
// arbitrary prose is not claimed to be a comprehensive secret classifier.
func diagnosticText(input string) (output string, truncated, redacted bool) {
	output = input
	if len(output) > diagnosticTextBytes {
		output = output[:diagnosticTextBytes]
		for !utf8.ValidString(output) {
			output = output[:len(output)-1]
		}
		truncated = true
	}
	for _, pattern := range diagnosticSecrets {
		output = pattern.ReplaceAllString(output, "[REDACTED]")
	}
	output = diagnosticURLs.ReplaceAllStringFunc(output, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "[REDACTED URL]"
		}
		if parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" {
			return raw
		}
		parsed.User = nil
		if parsed.RawQuery != "" {
			parsed.RawQuery = "REDACTED"
		}
		if parsed.Fragment != "" {
			parsed.Fragment = "REDACTED"
		}
		return parsed.String()
	})
	// Compare against the capped original independently of output lengths: a
	// replacement can be longer than the credential it removed.
	capped := input
	if truncated {
		capped = input[:diagnosticTextBytes]
		for !utf8.ValidString(capped) {
			capped = capped[:len(capped)-1]
		}
	}
	redacted = output != capped
	// Replacements can expand short values, so enforce the wire cap again.
	if len(output) > diagnosticTextBytes {
		output = output[:diagnosticTextBytes]
		for !utf8.ValidString(output) {
			output = output[:len(output)-1]
		}
		truncated = true
	}
	return output, truncated, redacted
}

func projectInstructorDiagnostics(scope domain.InstructorDiagnosticScope, captured domain.InstructorDiagnosticCapture) domain.InstructorMemberDiagnostics {
	rec := captured.Session
	result := domain.InstructorMemberDiagnostics{
		Schema: "ao.instructor.member-diagnostics.v1", WorkID: scope.WorkID, ProjectID: scope.ProjectID, WorkRevision: captured.WorkRevision,
		MemberID: scope.MemberID, Kind: captured.Kind, SessionID: scope.SessionID, InstanceID: scope.InstanceID, RuntimeGeneration: scope.RuntimeGeneration,
		CapturedAt: time.Now().UTC(), Atomic: true, PersistedOnly: true,
		Session:  domain.InstructorDiagnosticSession{Mode: domain.NormalizeSessionMode(rec.Mode), Harness: rec.Harness, ActivityState: rec.Activity.State, IsTerminated: rec.IsTerminated, ControllerGeneration: rec.Metadata.ControllerGeneration, ProvisionState: rec.ProvisionState, ProviderConversationID: rec.Metadata.ProviderConversationID, RuntimeLaunchID: rec.Metadata.RuntimeLaunchID, Model: rec.Metadata.Model, Effort: rec.Metadata.Effort},
		Timeline: domain.InstructorDiagnosticTimeline{Available: captured.Conversation.ID != "", Reason: captured.TimelineReason, Scope: captured.TimelineScope, ConversationID: captured.Conversation.ID, LatestSequence: captured.Conversation.LatestSequence, Items: []domain.InstructorDiagnosticItem{}, Turns: []domain.InstructorDiagnosticTurn{}, OldestSequence: captured.OldestSequence, HasMoreBefore: captured.HasMoreBefore},
	}
	result.Session.ProvisionError, result.Session.Truncated, result.Session.Redacted = diagnosticText(rec.ProvisionError)
	if captured.Review != nil && captured.ReviewRun != nil {
		review, run := captured.Review, captured.ReviewRun
		result.Review = &domain.InstructorDiagnosticReview{ReviewID: review.ID, ReviewRunID: run.ID, Harness: review.Harness, Status: run.Status, Verdict: run.Verdict, ControllerGeneration: review.ControllerGeneration}
		result.Review.ControllerError, result.Review.Truncated, result.Review.Redacted = diagnosticText(review.ControllerError)
	}
	tl := &result.Timeline
	if tl.Available {
		timestamp := captured.Conversation.UpdatedAt
		tl.ConversationUpdatedAt = &timestamp
	}
	switch tl.Scope {
	case "review":
		tl.ScopeNote = "Review-wide persisted conversation; individual items are not attributed to the selected review run."
	case "project":
		tl.ScopeNote = "Project-wide orchestrator conversation; individual items are not attributed to this work."
	}
	items := make([]domain.InstructorDiagnosticItem, 0, len(captured.Messages)+len(captured.Activities))
	for _, message := range captured.Messages {
		item := domain.InstructorDiagnosticItem{Kind: "message", ID: message.ID, Sequence: message.Sequence, Revision: message.Revision, Streaming: message.Streaming, TurnID: message.TurnID, Role: string(message.Role), CreatedAt: message.CreatedAt, UpdatedAt: message.UpdatedAt}
		if item.Role == "user" || item.Role == "assistant" {
			item.Text, item.Truncated, item.Redacted = diagnosticText(message.Text)
		} else {
			item.Redacted = true
		}
		items = append(items, item)
	}
	for _, activity := range captured.Activities {
		item := domain.InstructorDiagnosticItem{Kind: "activity", ID: activity.ID, Sequence: activity.Sequence, Revision: activity.Revision, TurnID: activity.TurnID, ActivityKind: string(activity.Kind), Status: string(activity.Status), CreatedAt: activity.CreatedAt, UpdatedAt: activity.UpdatedAt, Redacted: true}
		if item.ActivityKind == "error" || item.ActivityKind == "system" {
			item.Text, item.Truncated, _ = diagnosticText(activity.Summary)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Sequence > items[j].Sequence })
	remaining := diagnosticPageBytes
	retainedTurns := map[string]bool{}
	for _, item := range items {
		encoded, _ := json.Marshal(item)
		if len(encoded) > remaining {
			tl.Truncated, tl.HasMoreBefore = true, true
			break
		}
		remaining -= len(encoded)
		tl.Items = append(tl.Items, item)
		retainedTurns[item.TurnID] = true
		tl.OldestSequence = item.Sequence
		tl.Truncated, tl.Redacted = tl.Truncated || item.Truncated, tl.Redacted || item.Redacted
	}
	sort.Slice(tl.Items, func(i, j int) bool { return tl.Items[i].Sequence < tl.Items[j].Sequence })
	for _, turn := range captured.Turns {
		if !retainedTurns[turn.ID] {
			continue
		}
		entry := domain.InstructorDiagnosticTurn{ID: turn.ID, State: turn.State, RequestedAt: turn.RequestedAt, StartedAt: turn.StartedAt, CompletedAt: turn.CompletedAt}
		entry.ErrorMessage, entry.Truncated, entry.Redacted = diagnosticText(turn.ErrorMessage)
		encoded, _ := json.Marshal(entry)
		if len(encoded) > remaining || len(tl.Turns) >= 100 {
			tl.Truncated = true
			break
		}
		remaining -= len(encoded)
		tl.Turns = append(tl.Turns, entry)
		tl.Truncated, tl.Redacted = tl.Truncated || entry.Truncated, tl.Redacted || entry.Redacted
	}
	if tl.HasMoreBefore && len(tl.Items) > 0 {
		tl.NextBeforeSequence = tl.OldestSequence
	}
	return result
}

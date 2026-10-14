package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func diagnosticTestScope() domain.InstructorDiagnosticScope {
	return domain.InstructorDiagnosticScope{WorkID: "work-a", ProjectID: "project-a", MemberID: "session:instance-a", SessionID: "worker-a", InstanceID: "instance-a", RuntimeGeneration: "generation-a"}
}

func diagnosticTestCapture() domain.InstructorDiagnosticCapture {
	return domain.InstructorDiagnosticCapture{
		WorkRevision: 7, Kind: "session", TimelineScope: "session",
		Session: domain.SessionRecord{ID: "worker-a", ProjectID: "project-a", Mode: domain.SessionModeChat, IsTerminated: true,
			ProvisionState: domain.SessionProvisionFailed, ProvisionError: "provider launch failed: password=private-startup-value"},
		Conversation: domain.ConversationRecord{ID: "conversation-a", Scope: domain.ConversationScopeSession, LatestSequence: 100, UpdatedAt: time.Unix(1000, 0).UTC()},
	}
}

func TestDiagnosticTextRedactsKnownSecretsIncludingTruncatedPEM(t *testing.T) {
	pemStart := strings.Repeat("-", 5) + "BEGIN PRIVATE KEY" + strings.Repeat("-", 5)
	tests := []struct {
		name, input, forbidden string
	}{
		{"quoted JSON", `{"api_key":"plain-api-value"}`, "plain-api-value"},
		{"escaped quote JSON", `{"password":"left\"plain-escaped-value"}`, "plain-escaped-value"},
		{"quoted id token", `{"id_token": "plain-id-value"}`, "plain-id-value"},
		{"cookie", "Cookie: session=plain-cookie-value; theme=dark", "plain-cookie-value"},
		{"multiple cookies", "Cookie: first=abc; sessionid=plain-second-cookie-value", "plain-second-cookie-value"},
		{"set cookie", "Set-Cookie: first=abc; sessionid=plain-set-cookie-value; HttpOnly", "plain-set-cookie-value"},
		{"multiline quoted value", "{\"client_secret\":\"first-line\nplain-secret-continuation\"}", "plain-secret-continuation"},
		{"authorization", "Authorization: Bearer plain-bearer-value", "plain-bearer-value"},
		{"URL", "https://operator:plain-url-password@example.test/callback?access_token=plain-query-value#plain-fragment-value", "plain-"},
		{"PEM crossing cap", strings.Repeat("x", 3000) + "\n" + pemStart + "\n" + strings.Repeat("sensitive-key-material\n", 200) + "-----END PRIVATE KEY-----", "sensitive-key-material"},
		{"unfinished PEM", pemStart + "\nsensitive-key-material", "sensitive-key-material"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, redacted := diagnosticText(tt.input)
			if strings.Contains(got, tt.forbidden) || !redacted {
				t.Fatalf("known secret was not removed (redacted=%v)", redacted)
			}
			if !utf8.ValidString(got) {
				t.Fatal("redaction produced invalid UTF-8")
			}
		})
	}
}

func TestDiagnosticTextPreservesUTF8AndBoundsExpandedRedaction(t *testing.T) {
	plain := "valid ordinary provider failure"
	if got, truncated, redacted := diagnosticText(plain); got != plain || truncated || redacted {
		t.Fatalf("ordinary diagnostics changed: %q, truncated=%v, redacted=%v", got, truncated, redacted)
	}
	got, truncated, _ := diagnosticText(strings.Repeat("界", diagnosticTextBytes))
	if !truncated || !utf8.ValidString(got) || len(got) > diagnosticTextBytes || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("UTF-8 cap invalid: bytes=%d, truncated=%v, valid=%v", len(got), truncated, utf8.ValidString(got))
	}
	got, _, redacted := diagnosticText(strings.Repeat("token=a ", diagnosticTextBytes/8))
	if !redacted || len(got) > diagnosticTextBytes {
		t.Fatalf("redaction expanded beyond text cap: bytes=%d, redacted=%v", len(got), redacted)
	}
}

func TestInstructorDiagnosticProjectionRetainsFailuresAndStreamingWithoutRawPayloads(t *testing.T) {
	stamp := time.Unix(1000, 0).UTC()
	captured := diagnosticTestCapture()
	captured.Messages = []domain.ConversationMessage{{ID: "message-a", TurnID: "turn-a", Sequence: 5, Revision: 9, Role: domain.MessageRoleAssistant,
		Text: "provider failed: password=private-message-value", Streaming: true, CreatedAt: stamp, UpdatedAt: stamp}}
	captured.Activities = []domain.ConversationActivity{
		{ID: "reasoning-a", TurnID: "turn-a", Sequence: 6, Revision: 1, Kind: domain.ActivityKindReasoning, Summary: "private-reasoning-payload", CreatedAt: stamp, UpdatedAt: stamp},
		{ID: "command-a", TurnID: "turn-a", Sequence: 7, Revision: 1, Kind: domain.ActivityKindCommand, Summary: "private-command-payload", CreatedAt: stamp, UpdatedAt: stamp},
		{ID: "error-a", TurnID: "turn-a", Sequence: 8, Revision: 2, Kind: domain.ActivityKindError, Summary: "provider disconnected", CreatedAt: stamp, UpdatedAt: stamp},
	}
	captured.Turns = []domain.ConversationTurn{{ID: "turn-a", State: domain.TurnStateFailed, ErrorMessage: "token=private-turn-value", RequestedAt: stamp}}
	result := projectInstructorDiagnostics(diagnosticTestScope(), captured)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-startup-value", "private-message-value", "private-turn-value", "private-reasoning-payload", "private-command-payload"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("private payload escaped projection: %s", forbidden)
		}
	}
	if !result.Atomic || !result.PersistedOnly || !result.Session.IsTerminated || result.Session.ProvisionState != domain.SessionProvisionFailed || !result.Session.Redacted {
		t.Fatalf("failed retained session lost diagnostic facts: %+v", result.Session)
	}
	if len(result.Timeline.Items) != 4 || !result.Timeline.Items[0].Streaming || result.Timeline.Items[0].Revision != 9 || result.Timeline.Items[3].Text != "provider disconnected" {
		t.Fatalf("timeline lost sequence/revision/error facts: %+v", result.Timeline.Items)
	}
	if len(result.Timeline.Turns) != 1 || result.Timeline.Turns[0].State != domain.TurnStateFailed || !result.Timeline.Turns[0].Redacted {
		t.Fatalf("turn failure lost: %+v", result.Timeline.Turns)
	}
}

func TestInstructorDiagnosticByteBudgetKeepsNewestContiguousPageAndAdvancingCursor(t *testing.T) {
	captured := diagnosticTestCapture()
	for sequence := int64(1); sequence <= 100; sequence++ {
		captured.Messages = append(captured.Messages, domain.ConversationMessage{ID: "message", Sequence: sequence, Revision: 1, Role: domain.MessageRoleAssistant, Text: strings.Repeat("z", 3000)})
	}
	result := projectInstructorDiagnostics(diagnosticTestScope(), captured)
	page := result.Timeline
	if !page.Truncated || !page.HasMoreBefore || len(page.Items) < 1 || len(page.Items) >= 100 {
		t.Fatalf("expected a bounded partial page: count=%d truncated=%v more=%v", len(page.Items), page.Truncated, page.HasMoreBefore)
	}
	if page.Items[len(page.Items)-1].Sequence != 100 || page.OldestSequence != 101-int64(len(page.Items)) || page.NextBeforeSequence != page.OldestSequence {
		t.Fatalf("byte truncation skipped items or lost continuation: oldest=%d next=%d count=%d", page.OldestSequence, page.NextBeforeSequence, len(page.Items))
	}
	bytes := 0
	for i, item := range page.Items {
		if item.Sequence != page.OldestSequence+int64(i) {
			t.Fatal("page is not contiguous and oldest-first")
		}
		encoded, _ := json.Marshal(item)
		bytes += len(encoded)
	}
	if bytes > diagnosticPageBytes {
		t.Fatalf("item payload exceeded page byte budget: %d", bytes)
	}
}

func TestInstructorDiagnosticScopesAndExistingStoreCursorRemainExplicit(t *testing.T) {
	for _, scope := range []string{"project", "review"} {
		captured := diagnosticTestCapture()
		captured.TimelineScope, captured.OldestSequence, captured.HasMoreBefore = scope, 17, true
		captured.Activities = []domain.ConversationActivity{{ID: "reasoning", Sequence: 17, Revision: 1, Kind: domain.ActivityKindReasoning, Summary: "excluded-body"}}
		result := projectInstructorDiagnostics(diagnosticTestScope(), captured)
		if result.Timeline.Scope != scope || result.Timeline.ScopeNote == "" || !result.Timeline.HasMoreBefore || result.Timeline.NextBeforeSequence != 17 {
			t.Fatalf("scope or page continuation hidden: %+v", result.Timeline)
		}
	}
	captured := diagnosticTestCapture()
	captured.Conversation = domain.ConversationRecord{}
	captured.TimelineReason = "native_tui_logs_unavailable"
	result := projectInstructorDiagnostics(diagnosticTestScope(), captured)
	if result.Timeline.Available || result.Timeline.Reason != captured.TimelineReason || len(result.Timeline.Items) != 0 {
		t.Fatalf("unavailable history was invented: %+v", result.Timeline)
	}
}

type instructorDiagnosticTestStore struct {
	Store
	capture       domain.InstructorDiagnosticCapture
	err           error
	calls         int
	scope         domain.InstructorDiagnosticScope
	timeline      bool
	before, limit int64
}

func (s *instructorDiagnosticTestStore) CaptureInstructorDiagnostics(_ context.Context, scope domain.InstructorDiagnosticScope, timeline bool, before, limit int64) (domain.InstructorDiagnosticCapture, error) {
	s.calls++
	s.scope, s.timeline, s.before, s.limit = scope, timeline, before, limit
	return s.capture, s.err
}

func TestInstructorDiagnosticsServiceForwardsExactFenceAndFailsClosed(t *testing.T) {
	scope := diagnosticTestScope()
	scope.RuntimeGeneration = ""
	store := &instructorDiagnosticTestStore{capture: diagnosticTestCapture()}
	service := &Service{store: store}
	result, err := service.InstructorDiagnostics(context.Background(), scope, true, 17, 20)
	if err != nil || store.calls != 1 || store.scope != scope || !store.timeline || store.before != 17 || store.limit != 20 || result.RuntimeGeneration != "" || !result.PersistedOnly {
		t.Fatalf("exact diagnostic admission changed: %+v, err=%v", store, err)
	}
	store.err = domain.ErrInstructorProducerChanged
	if _, err := service.InstructorDiagnostics(context.Background(), scope, true, 0, 20); err == nil || !strings.Contains(err.Error(), "no longer matches") {
		t.Fatalf("stale ownership was not rejected: %v", err)
	}
	failure := errors.New("test persistence unavailable")
	store.err = failure
	if _, err := service.InstructorDiagnostics(context.Background(), scope, true, 0, 20); !errors.Is(err, failure) {
		t.Fatalf("persistence failure was concealed: %v", err)
	}
	for _, invalid := range []domain.InstructorDiagnosticScope{{}, {WorkID: "work-a", ProjectID: "project-a", MemberID: "session:instance-a", SessionID: "worker-a"}} {
		calls := store.calls
		if _, err := service.InstructorDiagnostics(context.Background(), invalid, true, 0, 20); err == nil || store.calls != calls {
			t.Fatal("invalid identity reached persistence")
		}
	}
	for _, page := range [][2]int64{{-1, 20}, {0, 0}, {0, 101}} {
		calls := store.calls
		if _, err := service.InstructorDiagnostics(context.Background(), scope, true, page[0], page[1]); err == nil || store.calls != calls {
			t.Fatal("invalid pagination reached persistence")
		}
	}
	unsupported := &Service{}
	if _, err := unsupported.InstructorDiagnostics(context.Background(), scope, false, 0, 20); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unavailable") {
		t.Fatalf("missing atomic read capability did not fail closed: %v", err)
	}
}

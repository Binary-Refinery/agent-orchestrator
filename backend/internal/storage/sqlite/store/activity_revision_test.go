package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestActivityProjectionCannotOverwriteHumanMessageWithUnchangedTimestamp(t *testing.T) {
	for _, offset := range []time.Duration{0, -time.Minute} {
		t.Run(offset.String(), func(t *testing.T) {
			s, sessionID, conversationID := conversationFixture(t)
			ctx := context.Background()
			before, _, err := s.GetSession(ctx, sessionID)
			if err != nil {
				t.Fatal(err)
			}
			before.UpdatedAt = histClock.Add(time.Hour)
			before.Metadata.LatestUserPrompt = "old prompt"
			before.Metadata.LatestUserPromptAt = histClock
			before.Metadata.LatestAssistantUpdate = "old answer"
			before.Metadata.ConversationCheckpointState = domain.ConversationCheckpointComplete
			before.Metadata.ConversationCheckpointGeneration = "gen-1"
			before.Metadata.ConversationCheckpointNativeID = "native-1"
			if err := s.UpdateSession(ctx, before); err != nil {
				t.Fatal(err)
			}
			before, _, err = s.GetSession(ctx, sessionID)
			if err != nil {
				t.Fatal(err)
			}
			created, err := s.AppendUserMessage(ctx, conversationID, sessionID, "gen-1", domain.ConversationMessage{
				ID: "new-message", Origin: domain.MessageOriginHuman, Text: "new prompt",
			}, "new-turn", before.UpdatedAt.Add(offset))
			if err != nil || !created {
				t.Fatalf("append message: created=%v err=%v", created, err)
			}
			after, _, err := s.GetSession(ctx, sessionID)
			if err != nil || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("fixture must retain updated_at: before=%v after=%v err=%v", before.UpdatedAt, after.UpdatedAt, err)
			}
			applied, err := s.UpdateSessionFromActivitySignal(ctx, before, before.Revision)
			if err != nil || applied {
				t.Fatalf("stale projection overwrote human checkpoint: applied=%v err=%v", applied, err)
			}
			after, _, err = s.GetSession(ctx, sessionID)
			if err != nil || after.Metadata.LatestUserPrompt != "new prompt" || after.Metadata.LatestAssistantUpdate != "" {
				t.Fatalf("human checkpoint lost: %+v err=%v", after.Metadata, err)
			}
		})
	}
}

func liveReconnectActivityFixture(t *testing.T) (*sqlite.Store, domain.SessionID, string) {
	t.Helper()
	return liveReconnectActivityFixtureAt(t, t.TempDir())
}

func liveReconnectActivityFixtureAt(t *testing.T, dataDir string) (*sqlite.Store, domain.SessionID, string) {
	t.Helper()
	s := sqlitetest.MustOpenAt(t, dataDir)
	seedProject(t, s, "hist")
	rec := sampleRecord("hist")
	rec.Mode = domain.SessionModeChat
	owner, err := s.CreateSession(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := s.CreateConversation(context.Background(), "conv-1", domain.ConversationScopeSession, "hist", owner.ID, histClock)
	if err != nil {
		t.Fatal(err)
	}
	id, conversationID := owner.ID, conversation.ID
	branch := domain.ConversationBranch{ID: "live-root", ConversationID: conversationID, SessionID: id,
		ProviderConversationID: "live-provider", ProviderScopeID: "live-root", ProviderIDsScoped: true, CreatedAt: histClock}
	if err := s.CreateAndActivateConversationBranch(context.Background(), id, branch, "old-generation", histClock); err != nil {
		t.Fatal(err)
	}
	return s, id, conversationID
}

func exitedReconnectRecord(t *testing.T, s *sqlite.Store, id domain.SessionID) domain.SessionRecord {
	t.Helper()
	ctx := context.Background()
	rec, found, err := s.GetSession(ctx, id)
	if err != nil || !found {
		t.Fatalf("get owner: found=%v err=%v", found, err)
	}
	rec.Activity = domain.Activity{State: domain.ActivityExited, LastActivityAt: histClock.Add(time.Second)}
	rec.FirstSignalAt = histClock
	rec.UpdatedAt = histClock.Add(2 * time.Second)
	rec.Metadata.LatestUserPrompt = "preserved original assignment"
	rec.Metadata.LatestUserPromptAt = histClock
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimChatControllerGeneration(ctx, id, "current-generation"); err != nil {
		t.Fatal(err)
	}
	rec, _, err = s.GetSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestRestoreLiveChatReconnectActivityUsesCurrentDurableFacts(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		running, queued, auth, recovered bool
		pending                          domain.ActivityKind
		want                             domain.ActivityState
	}{
		{"quiescent", false, false, false, false, "", domain.ActivityIdle},
		{"retained_old_generation_running", true, false, false, false, "", domain.ActivityActive},
		{"queued_only_no_dispatch", false, true, false, false, "", domain.ActivityIdle},
		{"approval_over_running", true, false, false, false, domain.ActivityKindApproval, domain.ActivityWaitingInput},
		{"input_over_running", true, false, false, false, domain.ActivityKindUserInput, domain.ActivityWaitingInput},
		{"auth_over_running", true, false, true, false, "", domain.ActivityWaitingInput},
		{"explicit_account_recovery", false, false, true, true, "", domain.ActivityIdle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, id, conversationID := liveReconnectActivityFixture(t)
			if tc.running {
				if err := s.AdoptProviderTurn(ctx, conversationID, id, "old-generation", "retained-root", "provider-root", histClock); err != nil {
					t.Fatal(err)
				}
			}
			if tc.queued {
				created, err := s.AppendUserMessage(ctx, conversationID, id, "old-generation", domain.ConversationMessage{ID: "queued-message", Text: "queued assignment", Origin: domain.MessageOriginHuman}, "queued-turn", histClock)
				if err != nil || !created {
					t.Fatalf("queue created=%v err=%v", created, err)
				}
			}
			if tc.pending != "" {
				if err := s.UpsertActivity(ctx, conversationID, "", domain.ConversationActivity{ID: "pending", Kind: tc.pending, Status: domain.ActivityStatusPending, RequestID: "request", ProviderItemID: "pending-item"}, histClock); err != nil {
					t.Fatal(err)
				}
			}
			if tc.auth {
				at := histClock
				account := domain.ConversationAccount{ReauthRequiredAt: &at}
				if err := s.RecordAccount(ctx, conversationID, account, at); err != nil {
					t.Fatal(err)
				}
				if tc.recovered {
					if err := s.RecordAccount(ctx, conversationID, domain.ConversationAccount{}, at.Add(time.Second)); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := exitedReconnectRecord(t, s, id)
			rows, err := s.LoadConversationSnapshot(ctx, conversationID)
			if err != nil {
				t.Fatal(err)
			}
			applied, err := s.RestoreLiveChatReconnectActivity(ctx, before)
			if err != nil || !applied {
				t.Fatalf("repair applied=%v err=%v", applied, err)
			}
			after, _, err := s.GetSession(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			want := before
			want.Activity.State = tc.want
			want.Revision = after.Revision
			if !reflect.DeepEqual(after, want) || after.Revision != before.Revision+1 {
				t.Fatalf("changed unrelated facts: want=%+v got=%+v", want, after)
			}
			afterRows, err := s.LoadConversationSnapshot(ctx, conversationID)
			if err != nil || !reflect.DeepEqual(rows, afterRows) {
				t.Fatalf("repair changed conversation/turn/queue/pending/account: err=%v", err)
			}
			if applied, err := s.RestoreLiveChatReconnectActivity(ctx, before); err != nil || applied {
				t.Fatalf("duplicate stale repair applied=%v err=%v", applied, err)
			}
		})
	}
}

func TestRestoreLiveChatReconnectActivityRejectsStaleOwnerAndRevision(t *testing.T) {
	for _, change := range []string{"provider", "generation", "harness", "project", "revision", "mode", "terminated", "expected_activity", "empty_provider", "empty_generation", "new_human_message"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			s, id, conversationID := liveReconnectActivityFixture(t)
			expected := exitedReconnectRecord(t, s, id)
			switch change {
			case "provider":
				expected.Metadata.ProviderConversationID = "replacement"
			case "generation":
				expected.Metadata.ControllerGeneration = "replacement"
			case "harness":
				expected.Harness = domain.HarnessFake
			case "project":
				expected.ProjectID = "other"
			case "revision":
				expected.Revision--
			case "mode":
				expected.Mode = domain.SessionModeTUI
			case "terminated":
				expected.IsTerminated = true
			case "expected_activity":
				expected.Activity.State = domain.ActivityIdle
			case "empty_provider":
				expected.Metadata.ProviderConversationID = ""
			case "empty_generation":
				expected.Metadata.ControllerGeneration = ""
			case "new_human_message":
				if _, err := s.RecordSessionLatestUserPrompt(ctx, id, "new authoritative human prompt", expected.UpdatedAt); err != nil {
					t.Fatal(err)
				}
			}
			before, _, err := s.GetSession(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := s.LoadConversationSnapshot(ctx, conversationID)
			if err != nil {
				t.Fatal(err)
			}
			if applied, err := s.RestoreLiveChatReconnectActivity(ctx, expected); err != nil || applied {
				t.Fatalf("stale repair applied=%v err=%v", applied, err)
			}
			after, _, err := s.GetSession(ctx, id)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("stale repair mutated session: err=%v", err)
			}
			afterRows, err := s.LoadConversationSnapshot(ctx, conversationID)
			if err != nil || !reflect.DeepEqual(rows, afterRows) {
				t.Fatalf("stale repair mutated narrative: err=%v", err)
			}
		})
	}
}

func TestRestoreLiveChatReconnectActivityPreservesCurrentActivityAndRejectsUnknownFacts(t *testing.T) {
	for _, state := range []domain.ActivityState{domain.ActivityActive, domain.ActivityIdle, domain.ActivityBlocked, domain.ActivityWaitingInput} {
		t.Run(string(state), func(t *testing.T) {
			ctx := context.Background()
			s, id, _ := liveReconnectActivityFixture(t)
			rec := exitedReconnectRecord(t, s, id)
			rec.Activity.State = state
			if err := s.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			before, _, err := s.GetSession(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if applied, err := s.RestoreLiveChatReconnectActivity(ctx, before); err != nil || applied {
				t.Fatalf("ordinary activity changed: applied=%v err=%v", applied, err)
			}
			after, _, err := s.GetSession(ctx, id)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("ordinary activity changed")
			}
		})
	}
	t.Run("running_without_native_id", func(t *testing.T) {
		ctx := context.Background()
		s, id, conversationID := liveReconnectActivityFixture(t)
		if err := s.AdoptProviderTurn(ctx, conversationID, id, "old-generation", "unknown-root", "", histClock); err != nil {
			t.Fatal(err)
		}
		before := exitedReconnectRecord(t, s, id)
		if applied, err := s.RestoreLiveChatReconnectActivity(ctx, before); err == nil || applied {
			t.Fatalf("unknown running facts became available: applied=%v err=%v", applied, err)
		}
		after, _, err := s.GetSession(ctx, id)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("unknown repair changed session")
		}
	})
	t.Run("cancelled_context", func(t *testing.T) {
		s, id, _ := liveReconnectActivityFixture(t)
		before := exitedReconnectRecord(t, s, id)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if applied, err := s.RestoreLiveChatReconnectActivity(ctx, before); !errors.Is(err, context.Canceled) || applied {
			t.Fatalf("cancelled repair: applied=%v err=%v", applied, err)
		}
		after, _, err := s.GetSession(context.Background(), id)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("cancelled repair changed session")
		}
	})
}

// Raw SQL below only creates corrupt/read/write-failure fixtures in t.TempDir.
// It does not edit runtime data or modify the production schema/migrations.
func TestRestoreLiveChatReconnectActivityFailsClosedOnUnknownFactsAndStoreErrors(t *testing.T) {
	for _, failure := range []string{"account_parse", "branch_provider", "branch_owner", "missing_branch", "missing_conversation", "invisible_pending", "pending_without_request_id", "running_different_owner", "running_foreign_provider_lineage", "write_error", "closed_database"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			dataDir := t.TempDir()
			s, id, conversationID := liveReconnectActivityFixtureAt(t, dataDir)
			if failure == "invisible_pending" || failure == "pending_without_request_id" {
				requestID := "pending-request"
				if failure == "pending_without_request_id" {
					requestID = ""
				}
				if err := s.UpsertActivity(ctx, conversationID, "", domain.ConversationActivity{ID: "pending", Kind: domain.ActivityKindUserInput, Status: domain.ActivityStatusPending, RequestID: requestID, ProviderItemID: "pending-item"}, histClock); err != nil {
					t.Fatal(err)
				}
				if failure == "invisible_pending" {
					if err := s.CreateAndActivateConversationBranch(ctx, id, domain.ConversationBranch{ID: "independent-root", ConversationID: conversationID, SessionID: id, ProviderConversationID: "live-provider", ProviderScopeID: "independent-root", ProviderIDsScoped: true, CreatedAt: histClock}, "old-generation", histClock); err != nil {
						t.Fatal(err)
					}
				}
			}
			if failure == "running_different_owner" {
				if err := s.AdoptProviderTurn(ctx, conversationID, id, "old-generation", "retained-root", "provider-root", histClock); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "running_foreign_provider_lineage" {
				if err := s.AdoptProviderTurn(ctx, conversationID, id, "old-generation", "foreign-root", "provider-root", histClock); err != nil {
					t.Fatal(err)
				}
				if err := s.UpsertActivity(ctx, conversationID, "provider-root", domain.ConversationActivity{ID: "foreign-tool", Kind: domain.ActivityKindCommand, Status: domain.ActivityStatusRunning, ProviderItemID: "foreign-tool-item"}, histClock); err != nil {
					t.Fatal(err)
				}
				rows, err := s.LoadConversationSnapshot(ctx, conversationID)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.CreateAndActivateConversationBranch(ctx, id, domain.ConversationBranch{ID: "new-native", ConversationID: conversationID, SessionID: id, ProviderConversationID: "replacement-provider", ParentBranchID: "live-root", ForkAfterSequence: rows.Conversation.LatestSequence, ProviderScopeID: "new-native", ProviderIDsScoped: true, CreatedAt: histClock}, "current-generation", histClock); err != nil {
					t.Fatal(err)
				}
			}
			before := exitedReconnectRecord(t, s, id)
			fixtureDB, err := sql.Open("sqlite", filepath.Join(dataDir, "ao.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = fixtureDB.Close() })
			var statement string
			switch failure {
			case "account_parse":
				statement = "UPDATE conversations SET account_json='{' WHERE id='conv-1'"
			case "branch_provider":
				statement = "UPDATE conversation_branches SET provider_conversation_id='different-provider' WHERE id='live-root'"
			case "branch_owner":
				statement = "UPDATE conversation_branches SET session_id='different-owner' WHERE id='live-root'"
			case "missing_branch":
				statement = "UPDATE conversations SET active_branch_id='missing' WHERE id='conv-1'"
			case "missing_conversation":
				statement = "UPDATE conversations SET current_session_id=NULL WHERE id='conv-1'"
			case "running_different_owner":
				statement = "UPDATE conversation_turns SET handled_by_session_id='different-owner' WHERE id='retained-root'"
			case "write_error":
				statement = "CREATE TRIGGER round7_reconnect_write_fail BEFORE UPDATE OF activity_state ON sessions BEGIN SELECT RAISE(ABORT, 'fixture activity write unavailable'); END"
			}
			if statement != "" {
				if _, err := fixtureDB.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "closed_database" {
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if applied, err := s.RestoreLiveChatReconnectActivity(ctx, before); err == nil || applied {
				t.Fatalf("unknown/error facts became available: applied=%v err=%v", applied, err)
			}
			reader := s
			if failure == "closed_database" {
				reader, err = sqlite.OpenPreMigrated(dataDir)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reader.Close() })
			}
			after, _, err := reader.GetSession(ctx, id)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed repair changed session: err=%v before=%+v after=%+v", err, before, after)
			}
		})
	}
}

func reconnectRawSessionFacts(t *testing.T, db *sql.DB, id domain.SessionID) []string {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(sessions)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var expressions []string
	for rows.Next() {
		var index, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&index, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name != "activity_state" && name != "revision" {
			expressions = append(expressions, `quote("`+name+`")`)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	facts := make([]string, len(expressions))
	dest := make([]any, len(expressions))
	for i := range facts {
		dest[i] = &facts[i]
	}
	if err := db.QueryRow("SELECT "+strings.Join(expressions, ",")+" FROM sessions WHERE id=?", id).Scan(dest...); err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestRestoreLiveChatReconnectActivityPreservesRawLegacyColumns(t *testing.T) {
	for _, tc := range []struct {
		name       string
		offset     bool
		checkpoint string
		wantError  bool
	}{
		{"zero_time_legacy_checkpoint_null_fields", false, "legacy", false},
		{"offset_time_legacy_checkpoint_dated_fields", true, "legacy", false},
		{"corrupt_blank_checkpoint_fails_closed", false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dataDir := t.TempDir()
			s, id, _ := liveReconnectActivityFixtureAt(t, dataDir)
			_ = exitedReconnectRecord(t, s, id)
			db, err := sql.Open("sqlite", filepath.Join(dataDir, "ao.db"))
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = db.Close() })
			var at time.Time
			var receipt, identity, promptAt, assistantAt any
			if tc.offset {
				at = histClock.In(time.FixedZone("CEST", 2*60*60))
				receipt = at
				identity = at
				promptAt = at
				assistantAt = at
			}
			if tc.wantError {
				// Current schema forbids blank checkpoints. Only this external
				// fixture connection disables CHECK to model corrupt legacy data;
				// the production writer still must fail closed, never normalize it.
				if _, err := db.ExecContext(ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.ExecContext(ctx, "UPDATE sessions SET activity_last_at=?,first_signal_at=?,native_identity_observed_at=?,latest_user_prompt_at=?,latest_assistant_update_at=?,conversation_checkpoint_state=? WHERE id=?", at, receipt, identity, promptAt, assistantAt, tc.checkpoint, id); err != nil {
				t.Fatal(err)
			}
			expected, found, err := s.GetSession(ctx, id)
			if err != nil || !found {
				t.Fatalf("read legacy owner: found=%v err=%v", found, err)
			}
			before := reconnectRawSessionFacts(t, db, id)
			applied, err := s.RestoreLiveChatReconnectActivity(ctx, expected)
			if (err != nil) != tc.wantError || applied == tc.wantError {
				t.Fatalf("legacy repair: applied=%v err=%v wantError=%v", applied, err, tc.wantError)
			}
			after := reconnectRawSessionFacts(t, db, id)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("raw unrelated columns changed: before=%v after=%v", before, after)
			}
		})
	}
}

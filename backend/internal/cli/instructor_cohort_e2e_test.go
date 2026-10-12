package cli

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// Exercise the retained CLI scalar DTO over the real HTTP controller, service
// and SQLite queue. No synthetic approvals array is supplied by the client.
func TestInstructorCLILegacyScalarGoCreatesOneOrderedCohort(t *testing.T) {
	for _, literal := range []string{"GO", ""} {
		t.Run("literal-"+literal, func(t *testing.T) {
			cfg := setConfigEnv(t)
			t.Setenv("AO_SESSION_ID", "")
			ctx := context.Background()
			store := sqlitetest.MustOpen(t)
			now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
			if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "legacy", Path: t.TempDir(), RegisteredAt: now}); err != nil {
				t.Fatal(err)
			}
			svc := instructorsvc.New(instructorsvc.Deps{Store: store, Clock: func() time.Time { return now }})
			works := []domain.InstructorWork{}
			for _, name := range []string{"first", "second"} {
				w, err := svc.Register(ctx, instructorsvc.RegisterInput{ProjectID: "legacy", Name: name, Goal: "Legacy cohort", Source: "manual", SourceRef: name})
				if err != nil {
					t.Fatal(err)
				}
				expected := w.Revision
				w.State, w.Phase, w.Revision = "waiting_human", "triage", expected+1
				w.Gate = &domain.InstructorGate{ID: name, Kind: "transition", TransitionTo: "fix", ExactApproval: literal, OpenedAt: now}
				changed, err := store.SaveInstructorWork(ctx, w, expected, domain.InstructorEvent{ID: uuid.NewString(), WorkID: w.ID, Kind: "trusted_fixture", At: now})
				if err != nil || !changed {
					t.Fatalf("seed captured gate: changed=%v err=%v", changed, err)
				}
				works = append(works, w)
			}
			snapshot, err := svc.Snapshot(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			server := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Instructor: svc, InstructorEnabled: true}, httpd.ControlDeps{}))
			defer server.Close()
			writeRunFileFor(t, cfg, server)
			args := []string{"instructor", "command", "Go", "--snapshot", snapshot.ID, "--work", works[1].ID + "," + works[0].ID, "--key", "legacy-scalar-key"}
			if literal != "" {
				args = append(args, "--approval", literal)
			}
			for attempt := 0; attempt < 2; attempt++ {
				out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...)
				if err != nil {
					t.Fatalf("retained scalar CLI failed: %v", err)
				}
				var result domain.InstructorCommandResult
				if err := json.Unmarshal([]byte(out), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Dispatches) != 2 || result.Dispatches[0].WorkID != works[1].ID || result.Dispatches[1].WorkID != works[0].ID {
					t.Fatalf("legacy CLI lost ordered admission: %s", out)
				}
				durable, err := store.ListInstructorDispatches(ctx, result.BatchID, "")
				if err != nil || len(durable) != 2 {
					t.Fatalf("retry duplicated cohort: %+v err=%v", durable, err)
				}
				for _, original := range works {
					details, err := svc.Details(ctx, original.ID)
					if err != nil || details.Work.Gate != nil || details.Work.ApprovalDispatchID == "" {
						t.Fatalf("legacy batch bypassed cohort fence: %+v err=%v", details.Work, err)
					}
				}
				if !strings.Contains(out, result.BatchID) {
					t.Fatal("CLI omitted durable batch receipt")
				}
			}
		})
	}
}

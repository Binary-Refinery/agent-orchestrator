package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

// These are unmodified actual upstream migrations, not an approximation of the
// foreign schema. Shared migrations 1–164 are byte-identical at the recorded ref.
//
//go:embed testdata/upstream-c3e17362/*
var upstreamLineageFixtures embed.FS

func applyUpstreamLineage(t *testing.T, db *sql.DB, version int64) {
	t.Helper()
	fixtureFS := fstest.MapFS{}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		number, err := goose.NumericComponent(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if number > 164 {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		fixtureFS["migrations/"+entry.Name()] = &fstest.MapFile{Data: body}
	}
	entries, err = upstreamLineageFixtures.ReadDir("testdata/upstream-c3e17362")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := upstreamLineageFixtures.ReadFile("testdata/upstream-c3e17362/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		fixtureFS["migrations/"+entry.Name()] = &fstest.MapFile{Data: body}
	}
	gooseMu.Lock()
	defer gooseMu.Unlock()
	goose.SetBaseFS(fixtureFS)
	defer goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "migrations", version); err != nil {
		t.Fatalf("apply actual upstream schema %d: %v", version, err)
	}
}

func lineageDatabaseFacts(t *testing.T, db *sql.DB) string {
	t.Helper()
	var facts string
	if err := db.QueryRow(`SELECT json_object(
 'schema', (SELECT json_group_array(json_object('type',type,'name',name,'sql',sql)) FROM (SELECT type,name,sql FROM sqlite_master ORDER BY type,name)),
 'ledger', (SELECT json_group_array(json_object('id',id,'version',version_id,'applied',is_applied)) FROM (SELECT id,version_id,is_applied FROM goose_db_version ORDER BY id)),
 'project', (SELECT json_group_array(json_object('id',id,'path',path)) FROM projects),
 'session', (SELECT json_group_array(json_object('id',id,'project',project_id,'harness',harness,'prompt',latest_user_prompt)) FROM sessions)
)`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

func seedLineageWork(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES ('lineage-project','/repo/lineage',CURRENT_TIMESTAMP);
INSERT INTO sessions(id,project_id,num,harness,activity_last_at,created_at,updated_at,latest_user_prompt)
VALUES ('lineage-session','lineage-project',1,'opencode',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'retain this original work');`); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateForkLineagePreservesExistingWork(t *testing.T) {
	for _, version := range []int64{164, 170} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			db := openMigratedDatabaseCopy(t, version)
			seedLineageWork(t, db)
			var originalWork string
			if version == 170 {
				policy := instructorsvc.DefaultPolicy()
				createdAt := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
				selection := domain.InstructorAgentSelection{Harness: "opencode", Model: "provider/native-worker", Effort: "high"}
				body, err := json.Marshal(domain.InstructorWork{
					ID: "lineage-work", ProjectID: "lineage-project", Name: "Original Work", Goal: "Keep the recorded work through upgrade",
					Source: "issue", SourceRef: "351", IssueID: "351", SessionIDs: []domain.SessionID{"lineage-session"},
					Launches: []domain.InstructorLaunch{{SessionID: "lineage-session", Role: "worker", Requested: selection, Sent: &selection, Status: "launched", LaunchedAt: createdAt}},
					State:    "running", Phase: "implement", Revision: 7, Policy: policy, PolicyVersion: policy.Version,
					CreatedAt: createdAt, UpdatedAt: createdAt, ProgressAt: createdAt,
				})
				if err != nil {
					t.Fatal(err)
				}
				originalWork = string(body)
				if _, err := db.Exec(`INSERT INTO instructor_works(id,project_id,source,source_ref,revision,body)
VALUES ('lineage-work','lineage-project','issue','351',7,?)`, originalWork); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO instructor_sessions(session_id,work_id) VALUES ('lineage-session','lineage-work');
INSERT INTO instructor_snapshots(id,body) VALUES ('lineage-snapshot','{"marker":"original immutable decision"}');
INSERT INTO instructor_events(id,work_id,body) VALUES ('lineage-event','lineage-work','{"marker":"original audit"}');`); err != nil {
					t.Fatal(err)
				}
			}
			if err := migrate(db); err != nil {
				t.Fatalf("upgrade populated fork/common database: %v", err)
			}
			var prompt, harness string
			if err := db.QueryRow(`SELECT latest_user_prompt,harness FROM sessions WHERE id='lineage-session'`).Scan(&prompt, &harness); err != nil || prompt != "retain this original work" || harness != "opencode" {
				t.Fatalf("existing session changed: %q %q err=%v", prompt, harness, err)
			}
			if version == 170 {
				var body, snapshot, audit string
				var revision int
				if err := db.QueryRow(`SELECT revision,body FROM instructor_works WHERE id='lineage-work'`).Scan(&revision, &body); err != nil || revision != 7 || body != originalWork {
					t.Fatalf("stable work changed: revision=%d body=%q err=%v", revision, body, err)
				}
				if err := db.QueryRow(`SELECT body FROM instructor_snapshots WHERE id='lineage-snapshot'`).Scan(&snapshot); err != nil || snapshot != `{"marker":"original immutable decision"}` {
					t.Fatalf("snapshot changed: %q err=%v", snapshot, err)
				}
				if err := db.QueryRow(`SELECT body FROM instructor_events WHERE id='lineage-event'`).Scan(&audit); err != nil || audit != `{"marker":"original audit"}` {
					t.Fatalf("audit changed: %q err=%v", audit, err)
				}
			}
			before := lineageDatabaseFacts(t, db)
			if err := migrate(db); err != nil {
				t.Fatalf("repeat upgrade: %v", err)
			}
			if after := lineageDatabaseFacts(t, db); after != before {
				t.Fatal("repeated migration changed schema, history or existing data")
			}
			rows, err := db.Query(`PRAGMA foreign_key_check`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if rows.Next() || rows.Err() != nil {
				t.Fatalf("upgrade left a foreign-key violation: %v", rows.Err())
			}
		})
	}
}

func TestOpenRefusesUpstreamMigrationLineageWithoutMutation(t *testing.T) {
	for _, version := range []int64{165, 170, 194} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			dataDir := t.TempDir()
			db := openMigratedDatabaseCopyAt(t, dataDir, 164, pragmas)
			seedLineageWork(t, db)
			applyUpstreamLineage(t, db, version)
			before := lineageDatabaseFacts(t, db)
			store, err := Open(dataDir)
			if store != nil || !errors.Is(err, ErrIncompatibleMigrationLineage) {
				t.Fatalf("foreign schema accepted: store=%v err=%v", store, err)
			}
			if !strings.Contains(err.Error(), "separate AO_DATA_DIR") {
				t.Fatalf("missing actionable lineage error: %v", err)
			}
			if after := lineageDatabaseFacts(t, db); before != after {
				t.Fatal("refusing upstream changed schema, ledger or original data")
			}
		})
	}
}

func TestPreMigratedVersionCannotStandInForForkLineage(t *testing.T) {
	dataDir := t.TempDir()
	db := openMigratedDatabaseCopyAt(t, dataDir, 164, pragmas)
	applyUpstreamLineage(t, db, 165)
	latest, err := expectedMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO goose_db_version(version_id,is_applied) VALUES (?,1)`, latest); err != nil {
		t.Fatal(err)
	}
	if store, err := OpenPreMigrated(dataDir); store != nil || !errors.Is(err, ErrIncompatibleMigrationLineage) {
		t.Fatalf("matching max version accepted a foreign template: store=%v err=%v", store, err)
	}
}

func TestUpstreamLineageFixturesMatchRecordedSource(t *testing.T) {
	data, err := upstreamLineageFixtures.ReadFile("testdata/upstream-c3e17362/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SourceCommit string `json:"sourceCommit"`
		Migrations   []struct {
			File   string `json:"file"`
			SHA256 string `json:"sha256"`
		} `json:"migrations"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SourceCommit != "c3e17362a85f37d0b0154bf8d841ed4f633f8306" || len(manifest.Migrations) != 27 {
		t.Fatalf("unexpected upstream fixture manifest: commit=%s count=%d", manifest.SourceCommit, len(manifest.Migrations))
	}
	for _, migration := range manifest.Migrations {
		body, err := upstreamLineageFixtures.ReadFile(filepath.ToSlash(filepath.Join("testdata/upstream-c3e17362", migration.File)))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(body)
		if hex.EncodeToString(hash[:]) != migration.SHA256 {
			t.Errorf("actual upstream migration %s changed", migration.File)
		}
	}
}

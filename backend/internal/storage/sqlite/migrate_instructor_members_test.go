package sqlite

import (
	"encoding/json"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestMigration0195CapturesLegacyBindingAndRetainsDeletedHistory(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 170)
	mustExec(t, db, `INSERT INTO projects(id,path,registered_at) VALUES('member-project','/tmp/members','2026-10-12T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO sessions(id,project_id,num,kind,display_name,activity_last_at,created_at,updated_at) VALUES('member-project-1','member-project',1,'worker','Legacy worker','2026-10-12T00:00:00Z','2026-10-12T00:00:00Z','2026-10-12T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO instructor_works(id,project_id,source,source_ref,revision,body) VALUES('member-work','member-project','manual','legacy',1,'{"name":"Legacy work","sessionIds":["member-project-1","gone-42"]}')`)
	mustExec(t, db, `INSERT INTO instructor_sessions(session_id,work_id) VALUES('member-project-1','member-work')`)
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var old, body string
	if err := db.QueryRow(`SELECT instance_id,body FROM instructor_session_members WHERE work_id='member-work'`).Scan(&old, &body); err != nil {
		t.Fatal(err)
	}
	var binding domain.InstructorSessionMembership
	if err := json.Unmarshal([]byte(body), &binding); err != nil {
		t.Fatal(err)
	}
	if old == "" || binding.InstanceID != old || binding.SessionID != "member-project-1" || binding.Provenance != "legacy_bound" {
		t.Fatalf("legacy membership=%+v", binding)
	}
	var live string
	if err := db.QueryRow(`SELECT instance_id FROM instructor_sessions WHERE session_id='member-project-1'`).Scan(&live); err != nil || live != old {
		t.Fatalf("live key=%q err=%v", live, err)
	}
	// No invented incarnation is created for the old append-only display ID.
	var invented int
	if err := db.QueryRow(`SELECT count(*) FROM instructor_session_members WHERE session_id='gone-42'`).Scan(&invented); err != nil || invented != 0 {
		t.Fatalf("invented historical binding=%d %v", invented, err)
	}
	mustExec(t, db, `DELETE FROM change_log WHERE session_id='member-project-1'`)
	mustExec(t, db, `DELETE FROM sessions WHERE id='member-project-1'`)
	mustExec(t, db, `INSERT INTO sessions(id,project_id,num,kind,activity_last_at,created_at,updated_at) VALUES('member-project-1','member-project',1,'worker','2026-10-12T00:00:00Z','2026-10-12T00:00:00Z','2026-10-12T00:00:00Z')`)
	var replacement string
	if err := db.QueryRow(`SELECT instance_id FROM instructor_session_instances WHERE session_id='member-project-1'`).Scan(&replacement); err != nil || replacement == old {
		t.Fatalf("replacement=%q old=%q err=%v", replacement, old, err)
	}
	var history int
	if err := db.QueryRow(`SELECT count(*) FROM instructor_session_members WHERE instance_id=?`, old).Scan(&history); err != nil || history != 1 {
		t.Fatalf("history=%d err=%v", history, err)
	}
	var rebound int
	if err := db.QueryRow(`SELECT count(*) FROM instructor_sessions WHERE session_id='member-project-1'`).Scan(&rebound); err != nil || rebound != 0 {
		t.Fatalf("replacement rebound=%d err=%v", rebound, err)
	}
	if _, err := db.Exec(`UPDATE instructor_session_instances SET instance_id='tampered' WHERE session_id='member-project-1'`); err == nil {
		t.Fatal("instance identity was mutable")
	}
}

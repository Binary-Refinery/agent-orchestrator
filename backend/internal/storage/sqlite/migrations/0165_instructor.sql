-- +goose Up
-- +goose StatementBegin
CREATE TABLE instructor_works (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 source TEXT NOT NULL,
 source_ref TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision > 0),
 body TEXT NOT NULL CHECK(json_valid(body)),
 UNIQUE(project_id, source, source_ref)
);
CREATE TABLE instructor_sessions (
 session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
 work_id TEXT NOT NULL REFERENCES instructor_works(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX idx_instructor_unique_work_name ON instructor_works(lower(json_extract(body,'$.name')));
CREATE TABLE instructor_events (
 id TEXT PRIMARY KEY,
 work_id TEXT NOT NULL REFERENCES instructor_works(id) ON DELETE RESTRICT,
 body TEXT NOT NULL CHECK(json_valid(body))
);
CREATE TABLE instructor_snapshots (
 id TEXT PRIMARY KEY,
 body TEXT NOT NULL CHECK(json_valid(body))
);
CREATE TABLE instructor_policy (
 id INTEGER PRIMARY KEY CHECK(id=1),
 body TEXT NOT NULL CHECK(json_valid(body))
);
CREATE TABLE instructor_verifications (
 work_id TEXT PRIMARY KEY REFERENCES instructor_works(id) ON DELETE RESTRICT,
 body TEXT NOT NULL CHECK(json_valid(body))
);
CREATE TABLE instructor_heartbeats (
 work_id TEXT NOT NULL REFERENCES instructor_works(id) ON DELETE RESTRICT,
 session_id TEXT NOT NULL,
 body TEXT NOT NULL CHECK(json_valid(body)),
 PRIMARY KEY(work_id,session_id)
);
CREATE TABLE instructor_dispatches (
 id TEXT PRIMARY KEY,
 batch_id TEXT NOT NULL,
 work_id TEXT NOT NULL REFERENCES instructor_works(id) ON DELETE RESTRICT,
 position INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','leased','delivered','accepted','progress','failed','cancelled')),
 lease_until INTEGER NOT NULL DEFAULT 0,
 body TEXT NOT NULL CHECK(json_valid(body)),
 UNIQUE(batch_id,position)
);
CREATE INDEX idx_instructor_dispatch_queue ON instructor_dispatches(state,lease_until);
CREATE INDEX idx_instructor_event_work ON instructor_events(work_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER instructor_work_insert AFTER INSERT ON instructor_works BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload) VALUES(NEW.project_id,NULL,'session_updated',json_object('instructorWorkId',NEW.id));
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER instructor_work_update AFTER UPDATE ON instructor_works BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload) VALUES(NEW.project_id,NULL,'session_updated',json_object('instructorWorkId',NEW.id));
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER instructor_snapshots_immutable BEFORE UPDATE ON instructor_snapshots BEGIN SELECT RAISE(ABORT,'instructor snapshot is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER instructor_events_immutable BEFORE UPDATE ON instructor_events BEGIN SELECT RAISE(ABORT,'instructor event is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER instructor_snapshots_no_delete BEFORE DELETE ON instructor_snapshots BEGIN SELECT RAISE(ABORT,'instructor snapshot is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER instructor_events_no_delete BEFORE DELETE ON instructor_events BEGIN SELECT RAISE(ABORT,'instructor event is immutable'); END;
-- +goose StatementEnd
-- +goose Down
DROP TRIGGER instructor_events_no_delete;
DROP TRIGGER instructor_snapshots_no_delete;
DROP TRIGGER instructor_events_immutable;
DROP TRIGGER instructor_snapshots_immutable;
DROP TRIGGER instructor_work_update;
DROP TRIGGER instructor_work_insert;
DROP TABLE instructor_dispatches;
DROP TABLE instructor_policy;
DROP TABLE instructor_verifications;
DROP TABLE instructor_heartbeats;
DROP TABLE instructor_snapshots;
DROP TABLE instructor_events;
DROP TABLE instructor_sessions;
DROP TABLE instructor_works;

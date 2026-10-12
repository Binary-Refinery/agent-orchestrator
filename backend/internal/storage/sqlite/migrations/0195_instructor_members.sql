-- +goose Up
-- +goose StatementBegin
-- Session display IDs may be reused after deletion. This key belongs to one
-- inserted row, survives controller restarts, and is never reused with the ID.
CREATE TABLE instructor_session_instances (
 session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
 instance_id TEXT NOT NULL UNIQUE
);
INSERT INTO instructor_session_instances(session_id,instance_id)
 SELECT id,lower(hex(randomblob(16))) FROM sessions;
CREATE TRIGGER instructor_session_instance_insert AFTER INSERT ON sessions BEGIN
 INSERT INTO instructor_session_instances(session_id,instance_id) VALUES(NEW.id,lower(hex(randomblob(16))));
END;
CREATE TRIGGER instructor_session_instance_immutable BEFORE UPDATE ON instructor_session_instances BEGIN
 SELECT RAISE(ABORT,'session instance identity is immutable');
END;
ALTER TABLE instructor_sessions ADD COLUMN instance_id TEXT NOT NULL DEFAULT '';
UPDATE instructor_sessions SET instance_id=(SELECT instance_id FROM instructor_session_instances WHERE session_id=instructor_sessions.session_id);
-- Historical membership deliberately has no session FK: deleting an agent must
-- not delete the work's identity or transfer its association to a replacement.
CREATE TABLE instructor_session_members (
 work_id TEXT NOT NULL REFERENCES instructor_works(id) ON DELETE RESTRICT,
 session_id TEXT NOT NULL,
 instance_id TEXT NOT NULL,
 body TEXT NOT NULL CHECK(json_valid(body)),
 PRIMARY KEY(work_id,instance_id)
);
INSERT INTO instructor_session_members(work_id,session_id,instance_id,body)
 SELECT b.work_id,b.session_id,b.instance_id,json_object(
 'sessionId',b.session_id,'instanceId',b.instance_id,'projectId',s.project_id,
 'role',s.kind,'name',s.display_name,
 'provenance','legacy_bound') FROM instructor_sessions b JOIN sessions s ON s.id=b.session_id;
-- +goose StatementEnd

-- +goose Down
DROP TABLE instructor_session_members;
ALTER TABLE instructor_sessions DROP COLUMN instance_id;
DROP TRIGGER instructor_session_instance_immutable;
DROP TRIGGER instructor_session_instance_insert;
DROP TABLE instructor_session_instances;

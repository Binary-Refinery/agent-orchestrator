-- +goose Up
CREATE TABLE instructor_budget_entries (
 id TEXT PRIMARY KEY,
 work_id TEXT NOT NULL REFERENCES instructor_works(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('session','turn')),
 resource_key TEXT NOT NULL,
 session_id TEXT NOT NULL DEFAULT '',
 review_id TEXT NOT NULL DEFAULT '',
 delegated INTEGER NOT NULL CHECK(delegated IN (0,1)),
 state TEXT NOT NULL CHECK(state IN ('reserved','started','finished','cancelled','uncertain')),
 body TEXT NOT NULL CHECK(json_valid(body)),
 UNIQUE(work_id,kind,resource_key)
);
CREATE INDEX instructor_budget_work ON instructor_budget_entries(work_id,state,kind);
-- +goose StatementBegin
CREATE TRIGGER instructor_budget_insert AFTER INSERT ON instructor_budget_entries BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload) VALUES(NULL,NULL,'session_updated',json_object('instructorBudget',NEW.work_id));
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER instructor_budget_update AFTER UPDATE ON instructor_budget_entries BEGIN
 INSERT INTO change_log(project_id,session_id,event_type,payload) VALUES(NULL,NULL,'session_updated',json_object('instructorBudget',NEW.work_id));
END;
-- +goose StatementEnd
-- +goose Down
DROP TRIGGER instructor_budget_update;
DROP TRIGGER instructor_budget_insert;
DROP INDEX instructor_budget_work;
DROP TABLE instructor_budget_entries;

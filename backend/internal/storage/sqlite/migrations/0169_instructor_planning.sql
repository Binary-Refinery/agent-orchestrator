-- +goose Up
CREATE TABLE instructor_plans (
 id TEXT PRIMARY KEY,
 state TEXT NOT NULL CHECK(state IN ('queued','running','completed','failed','interrupted')),
 lease_until INTEGER NOT NULL DEFAULT 0,
 body TEXT NOT NULL CHECK(json_valid(body))
);
CREATE INDEX idx_instructor_plans_queue ON instructor_plans(state,lease_until);
-- +goose Down
DROP TABLE instructor_plans;

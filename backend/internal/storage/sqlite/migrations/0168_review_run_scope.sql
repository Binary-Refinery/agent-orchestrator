-- +goose Up
-- A governed delta phase can require a fresh pass at the same commit. Keep
-- earlier approved history and retain idempotency within each durable phase.
ALTER TABLE review_run ADD COLUMN scope_key TEXT NOT NULL DEFAULT '';
ALTER TABLE review ADD COLUMN launch_scope_key TEXT NOT NULL DEFAULT '';
DROP INDEX idx_review_run_session_pr_sha_harness;
CREATE UNIQUE INDEX idx_review_run_session_pr_sha_harness
    ON review_run (session_id, pr_url, target_sha, harness, scope_key)
    WHERE target_sha != ''
        AND status NOT IN ('failed', 'cancelled')
        AND (status = 'running' OR verdict NOT IN ('', 'changes_requested'));

-- +goose Down
-- This downgrade refuses duplicate approved phase history; it never deletes it.
DROP INDEX idx_review_run_session_pr_sha_harness;
CREATE UNIQUE INDEX idx_review_run_session_pr_sha_harness
    ON review_run (session_id, pr_url, target_sha, harness)
    WHERE target_sha != ''
        AND status NOT IN ('failed', 'cancelled')
        AND (status = 'running' OR verdict NOT IN ('', 'changes_requested'));
ALTER TABLE review_run DROP COLUMN scope_key;
ALTER TABLE review DROP COLUMN launch_scope_key;

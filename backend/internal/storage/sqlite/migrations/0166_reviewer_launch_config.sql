-- +goose Up
ALTER TABLE review ADD COLUMN launch_agent_config TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE review DROP COLUMN launch_agent_config;

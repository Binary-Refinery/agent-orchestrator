-- +goose Up
CREATE TABLE semantic_deliveries (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_message_id TEXT NOT NULL,
    runtime_launch_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'accepted', 'cancelled')),
    created_at TEXT NOT NULL,
    accepted_at TEXT,
    PRIMARY KEY (session_id, client_message_id)
);

-- +goose Down
DROP TABLE semantic_deliveries;

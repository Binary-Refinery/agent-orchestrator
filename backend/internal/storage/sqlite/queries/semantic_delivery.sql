-- name: BeginSemanticDelivery :execrows
INSERT OR IGNORE INTO semantic_deliveries
    (session_id, client_message_id, runtime_launch_id, state, created_at)
VALUES (?, ?, ?, 'pending', ?);

-- name: GetSemanticDelivery :one
SELECT * FROM semantic_deliveries
WHERE session_id = ? AND client_message_id = ?;

-- name: AcceptSemanticDelivery :execrows
UPDATE semantic_deliveries
SET state = 'accepted', accepted_at = ?
WHERE session_id = ? AND client_message_id = ? AND runtime_launch_id = ? AND state != 'cancelled';

-- name: ResolveSemanticDelivery :execrows
UPDATE semantic_deliveries SET state = sqlc.arg(state), accepted_at = sqlc.narg(accepted_at)
WHERE session_id = sqlc.arg(session_id) AND client_message_id = sqlc.arg(client_message_id)
  AND (state != 'accepted' OR sqlc.arg(state) = 'accepted');

-- Native acceptance is associated with the exact durable user message and the
-- controller generation that originally recorded it, including after restart.
-- name: AcceptSemanticChatTurn :execrows
UPDATE semantic_deliveries SET state = 'accepted', accepted_at = sqlc.arg(accepted_at)
WHERE state = 'pending' AND EXISTS (
 SELECT 1 FROM conversation_turns t JOIN conversation_messages m ON m.turn_id = t.id
 WHERE t.conversation_id = sqlc.arg(conversation_id) AND t.provider_turn_id = sqlc.arg(provider_turn_id)
   AND m.role = 'user' AND m.client_message_id = semantic_deliveries.client_message_id
   AND t.handled_by_session_id = semantic_deliveries.session_id
   AND semantic_deliveries.runtime_launch_id = 'chat:' || t.controller_generation
);

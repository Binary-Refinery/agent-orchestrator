-- name: InstructorDispatchesForNativeTurn :many
SELECT d.body,t.controller_generation,t.handled_by_session_id
FROM instructor_dispatches d
JOIN conversation_messages m ON m.client_message_id=d.id AND m.role='user'
JOIN conversation_turns t ON t.id=m.turn_id
JOIN instructor_session_instances i ON i.session_id=t.handled_by_session_id
WHERE t.conversation_id=? AND t.provider_turn_id=?
  AND json_extract(d.body,'$.targetSessionInstanceId')=i.instance_id;

-- name: SetInstructorDispatchBody :exec
UPDATE instructor_dispatches SET body=? WHERE id=?;

-- name: InsertInstructorNativeStartEvent :exec
INSERT OR IGNORE INTO instructor_events(id,work_id,body) VALUES(?,?,?);

-- name: AcknowledgeInstructorWorkerReport :exec
UPDATE reports SET delivery_state='acknowledged',acknowledged_at=?,last_error=''
WHERE id=? AND delivery_batch_id=?;

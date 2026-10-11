package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// PutInstructorHeartbeat records session availability separately from business progress.
func (s *Store) PutInstructorHeartbeat(ctx context.Context, heartbeat domain.InstructorHeartbeat) error {
	body, err := instructorJSON(heartbeat)
	if err != nil {
		return err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	_, err = s.writeDB.ExecContext(ctx, `INSERT INTO instructor_heartbeats(work_id,session_id,body) VALUES(?,?,?) ON CONFLICT(work_id,session_id) DO UPDATE SET body=excluded.body`, heartbeat.WorkID, heartbeat.SessionID, body)
	return err
}

// GetInstructorHeartbeat reads the latest recorded availability heartbeat for a work.
func (s *Store) GetInstructorHeartbeat(ctx context.Context, workID string) (domain.InstructorHeartbeat, bool, error) {
	var body string
	err := s.readDB.QueryRowContext(ctx, `SELECT body FROM instructor_heartbeats WHERE work_id=? ORDER BY json_extract(body,'$.at') DESC,session_id LIMIT 1`, workID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InstructorHeartbeat{}, false, nil
	}
	var heartbeat domain.InstructorHeartbeat
	if err == nil {
		err = json.Unmarshal([]byte(body), &heartbeat)
	}
	return heartbeat, err == nil, err
}

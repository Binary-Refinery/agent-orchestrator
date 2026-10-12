package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// CreateInstructorPlan binds a caller key to exactly one immutable request.
func (s *Store) CreateInstructorPlan(ctx context.Context, j domain.InstructorPlanJob) (domain.InstructorPlanJob, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return j, err
	}
	defer s.writeMu.Unlock()
	body, err := json.Marshal(j)
	if err != nil {
		return j, err
	}
	if _, err = s.writeDB.ExecContext(ctx, `INSERT INTO instructor_plans(id,state,body) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, j.ID, j.State, string(body)); err != nil {
		return j, err
	}
	var saved string
	if err := s.writeDB.QueryRowContext(ctx, `SELECT body FROM instructor_plans WHERE id=?`, j.ID).Scan(&saved); err != nil {
		return j, err
	}
	err = json.Unmarshal([]byte(saved), &j)
	return j, err
}

// GetInstructorPlan reads the global strategic job journal.
func (s *Store) GetInstructorPlan(ctx context.Context, id string) (domain.InstructorPlanJob, bool, error) {
	var body string
	var j domain.InstructorPlanJob
	err := s.readDB.QueryRowContext(ctx, `SELECT body FROM instructor_plans WHERE id=?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return j, false, nil
	}
	if err != nil {
		return j, false, err
	}
	err = json.Unmarshal([]byte(body), &j)
	return j, err == nil, err
}

// ListInstructorPlans retains complete ordered history; callers bound context.
func (s *Store) ListInstructorPlans(ctx context.Context) ([]domain.InstructorPlanJob, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT body FROM instructor_plans ORDER BY json_extract(body,'$.createdAt'),id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	jobs := []domain.InstructorPlanJob{}
	for rows.Next() {
		var body string
		var j domain.InstructorPlanJob
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(body), &j); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// ClaimInstructorPlan never repeats a native request whose completion is unknown.
func (s *Store) ClaimInstructorPlan(ctx context.Context, now time.Time) (domain.InstructorPlanJob, bool, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.InstructorPlanJob{}, false, err
	}
	defer s.writeMu.Unlock()
	_, err := s.writeDB.ExecContext(ctx, `UPDATE instructor_plans SET state='interrupted',lease_until=0,body=json_set(body,'$.state','interrupted','$.error','Daemon restart interrupted native planning; submit a new request key to retry','$.updatedAt',?) WHERE state='running' AND lease_until<=?`, now.UTC().Format(time.RFC3339Nano), now.Unix())
	if err != nil {
		return domain.InstructorPlanJob{}, false, err
	}
	var body string
	var j domain.InstructorPlanJob
	err = s.writeDB.QueryRowContext(ctx, `SELECT body FROM instructor_plans WHERE state='queued' ORDER BY json_extract(body,'$.createdAt'),id LIMIT 1`).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return j, false, nil
	}
	if err != nil {
		return j, false, err
	}
	if err := json.Unmarshal([]byte(body), &j); err != nil {
		return j, false, err
	}
	j.State, j.UpdatedAt = "running", now
	data, err := json.Marshal(j)
	if err != nil {
		return j, false, err
	}
	_, err = s.writeDB.ExecContext(ctx, `UPDATE instructor_plans SET state='running',lease_until=?,body=? WHERE id=? AND state='queued'`, now.Add(time.Duration(j.Input.TimeoutSeconds+30)*time.Second).Unix(), string(data), j.ID)
	return j, err == nil, err
}

// SaveInstructorPlan updates only a still-owned running native request.
func (s *Store) SaveInstructorPlan(ctx context.Context, j domain.InstructorPlanJob) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	body, err := json.Marshal(j)
	if err != nil {
		return err
	}
	result, err := s.writeDB.ExecContext(ctx, `UPDATE instructor_plans SET state=?,body=? WHERE id=? AND state='running'`, j.State, string(body), j.ID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("instructor planning lease no longer owned")
	}
	return nil
}

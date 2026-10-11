package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// GetInstructorVerification reads the persistent deep-verification result and retry budget.
func (s *Store) GetInstructorVerification(ctx context.Context, workID string) (domain.InstructorVerification, bool, error) {
	var body string
	err := s.readDB.QueryRowContext(ctx, `SELECT body FROM instructor_verifications WHERE work_id=?`, workID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InstructorVerification{}, false, nil
	}
	var v domain.InstructorVerification
	if err == nil {
		err = json.Unmarshal([]byte(body), &v)
	}
	if v.MaxPasses == 0 {
		v.MaxPasses = 2
	}
	return v, err == nil, err
}

// ClaimInstructorVerification leases one bounded deep-verification attempt for a trusted source key.
func (s *Store) ClaimInstructorVerification(ctx context.Context, workID, key string, at time.Time) (domain.InstructorVerification, bool, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.InstructorVerification{}, false, err
	}
	defer s.writeMu.Unlock()
	var v domain.InstructorVerification
	claimed := false
	err := s.inTxDB(ctx, "claim instructor verification", func(_ *gen.Queries, db gen.DBTX) error {
		var body string
		err := db.QueryRowContext(ctx, `SELECT body FROM instructor_verifications WHERE work_id=?`, workID).Scan(&body)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if err := json.Unmarshal([]byte(body), &v); err != nil {
				return err
			}
		}
		// One persistent lease fences local and restarted processes. Source
		// changes cannot bypass an in-flight job or the bounded retry delay.
		if at.Before(v.NextRunAt) {
			return nil
		}
		if v.Key != key {
			v = domain.InstructorVerification{Key: key, MaxPasses: 2}
		}
		v.MaxPasses = 2
		if v.Passes >= 2 || v.Status == "complete" {
			return nil
		}
		v.Passes++
		v.Status, v.Error, v.LastRunAt, v.NextRunAt = "running", "", at, at.Add(90*time.Second)
		body, err = instructorJSON(v)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO instructor_verifications(work_id,body) VALUES(?,?) ON CONFLICT(work_id) DO UPDATE SET body=excluded.body`, workID, body)
		claimed = err == nil
		return err
	})
	return v, claimed, err
}

// FinishInstructorVerification records a result only for the matching key and owned running attempt.
func (s *Store) FinishInstructorVerification(ctx context.Context, workID, key string, attempt int, status, detail string, claims, verified int, at time.Time, inspected bool) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "finish instructor verification", func(_ *gen.Queries, db gen.DBTX) error {
		var body string
		if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_verifications WHERE work_id=?`, workID).Scan(&body); err != nil {
			return err
		}
		var v domain.InstructorVerification
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			return err
		}
		if v.Key != key || v.Passes != attempt || v.Status != "running" {
			return nil
		}
		v.Status, v.Error, v.Claims, v.Verified, v.NextRunAt = status, detail, claims, verified, at.Add(30*time.Second)
		v.Inspected = inspected
		body, err := instructorJSON(v)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `UPDATE instructor_verifications SET body=? WHERE work_id=?`, body, workID)
		return err
	})
}

// AppendInstructorEvent appends an immutable business event to the work journal.
func (s *Store) AppendInstructorEvent(ctx context.Context, event domain.InstructorEvent) error {
	body, err := instructorJSON(event)
	if err != nil {
		return err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	_, err = s.writeDB.ExecContext(ctx, `INSERT INTO instructor_events(id,work_id,body) VALUES(?,?,?)`, event.ID, event.WorkID, body)
	return err
}

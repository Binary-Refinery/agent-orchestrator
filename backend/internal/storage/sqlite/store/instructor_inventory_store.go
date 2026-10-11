package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// GetInstructorInventoryCache reads captured external facts and retained discoveries.
// The daemon owns their bounded schema; derived session display status is not stored.
func (s *Store) GetInstructorInventoryCache(ctx context.Context) ([]byte, bool, error) {
	var body string
	err := s.readDB.QueryRowContext(ctx, `SELECT body FROM instructor_inventory_cache WHERE id=1`).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return []byte(body), err == nil, err
}

// PutInstructorInventoryCache persists the bounded captured inventory and retained discoveries.
func (s *Store) PutInstructorInventoryCache(ctx context.Context, body []byte) error {
	if len(body) > 8<<20 || !json.Valid(body) {
		return errors.New("inventory cache exceeds bounded JSON contract")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	_, err := s.writeDB.ExecContext(ctx, `INSERT INTO instructor_inventory_cache(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`, string(body))
	return err
}

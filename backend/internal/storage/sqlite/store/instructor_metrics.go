package store

import (
	"context"
	"time"
)

// InstructorEventCountsSince aggregates immutable business events without replaying every history in Status.
func (s *Store) InstructorEventCountsSince(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT json_extract(body,'$.kind'),COUNT(*) FROM instructor_events WHERE json_extract(body,'$.at')>=? GROUP BY json_extract(body,'$.kind')`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int{}
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		counts[kind] = count
	}
	return counts, rows.Err()
}

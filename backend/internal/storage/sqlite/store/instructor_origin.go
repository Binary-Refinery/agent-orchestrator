package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// InstructorWorkByOrigin resolves durable intake identity. It deliberately does
// not depend on a session that can be removed or replaced after the work starts.
func (s *Store) InstructorWorkByOrigin(ctx context.Context, projectID domain.ProjectID, source, sourceRef string) (domain.InstructorWork, bool, error) {
	var body string
	err := s.readDB.QueryRowContext(ctx, `SELECT body FROM instructor_works WHERE project_id=? AND source=? AND source_ref=?`, projectID, source, sourceRef).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InstructorWork{}, false, nil
	}
	if err != nil {
		return domain.InstructorWork{}, false, err
	}
	var work domain.InstructorWork
	err = json.Unmarshal([]byte(body), &work)
	return work, err == nil, err
}

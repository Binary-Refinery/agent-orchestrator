package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func instructorIssueWorkInTx(ctx context.Context, db gen.DBTX, projectID domain.ProjectID, repository string, number int) (domain.InstructorWork, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT body FROM instructor_works WHERE project_id=?`, projectID)
	if err != nil {
		return domain.InstructorWork{}, false, err
	}
	defer func() { _ = rows.Close() }()
	var linked domain.InstructorWork
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return linked, false, err
		}
		var work domain.InstructorWork
		if err := json.Unmarshal([]byte(body), &work); err != nil {
			return linked, false, err
		}
		issueNumber, valid := domain.InstructorWorkGitHubIssue(work, repository)
		if valid && issueNumber == number {
			if linked.ID != "" {
				return linked, false, fmt.Errorf("%w: issue belongs to several works", domain.ErrInstructorIntakeChanged)
			}
			linked = work
		}
	}
	return linked, linked.ID != "", rows.Err()
}

package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// SettleRecoveredControllerWork retires only calls admitted by the dead provider
// after its replacement has verified native history. Queued prompts have not
// been dispatched and must survive recovery; their execution admission remains
// the controller's responsibility. A durable uncertain admission still fences
// any attempt to resend a queued turn whose native acceptance was not recorded.
func (s *Store) SettleRecoveredControllerWork(ctx context.Context, session domain.SessionID, conversationID, generation string, now time.Time) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "settle recovered controller work", func(q *gen.Queries, db gen.DBTX) error {
		var owned int
		// Project conversations have no immutable session_id; the current owner
		// is authoritative for both session- and project-scoped narratives.
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions s JOIN conversations c ON c.current_session_id=s.id WHERE s.id=? AND s.controller_generation=? AND c.id=?`, session, generation, conversationID).Scan(&owned); err != nil {
			return err
		}
		if generation == "" || owned != 1 {
			return domain.ErrSessionRecoveryChanged
		}
		if _, err := db.ExecContext(ctx, `UPDATE conversation_activities SET status='failed',revision=revision+1,updated_at=? WHERE status='running' AND turn_id IN (SELECT id FROM conversation_turns WHERE conversation_id=? AND handled_by_session_id=? AND state='running')`, now, conversationID, session); err != nil {
			return fmt.Errorf("settle orphaned running activities: %w", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE conversation_turns SET state='failed',error_message='controller ended before the turn completed',completed_at=? WHERE conversation_id=? AND handled_by_session_id=? AND state='running'`, now, conversationID, session); err != nil {
			return fmt.Errorf("settle orphaned running turns: %w", err)
		}
		return q.FailPendingConversationRequestsForSession(ctx, gen.FailPendingConversationRequestsForSessionParams{
			UpdatedAt: now, TargetConversationID: conversationID, HandledBySessionID: session,
		})
	})
}

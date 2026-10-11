package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type chatAgentConfigStore interface {
	GetReviewByID(context.Context, string) (domain.Review, bool, error)
	ConversationForReview(context.Context, string) (domain.ConversationRecord, error)
}

// persistChatAgentConfig keeps session rebuild metadata synchronized. Reviewer
// choices already belong to their conversation settings, never to the worker's
// session row or the historical successful-launch snapshot.
func persistChatAgentConfig(ctx context.Context, store chatAgentConfigStore, manager any, id domain.SessionID, model, effort string) error {
	_, reviewer, err := store.GetReviewByID(ctx, string(id))
	if err != nil {
		return fmt.Errorf("resolve chat configuration owner: %w", err)
	}
	if reviewer {
		conversation, err := store.ConversationForReview(ctx, string(id))
		if err != nil {
			return fmt.Errorf("verify reviewer configuration persistence: %w", err)
		}
		if strings.TrimSpace(conversation.Settings.Model) != strings.TrimSpace(model) || strings.TrimSpace(conversation.Settings.ReasoningEffort) != strings.TrimSpace(effort) {
			return errors.New("reviewer conversation does not contain the requested native configuration")
		}
		return nil
	}
	persister, ok := manager.(interface {
		PersistChatAgentConfig(context.Context, domain.SessionID, string, string) error
	})
	if !ok {
		return errors.New("ChatUI native model/effort persistence is unavailable")
	}
	return persister.PersistChatAgentConfig(ctx, id, model, effort)
}

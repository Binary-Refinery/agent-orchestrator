package ports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrSemanticDeliveryUncertain requires reconciliation before native resubmission.
var ErrSemanticDeliveryUncertain = errors.New("native semantic delivery is uncertain; reconcile its exact receipt before resending")

// ErrSemanticDeliveryQueued reports durable enqueue without native acceptance.
var ErrSemanticDeliveryQueued = errors.New("native semantic delivery remains queued; provider acceptance has not been observed")

// ErrSemanticDeliveryRejected proves cancellation before native submission.
var ErrSemanticDeliveryRejected = errors.New("governed native submission was cancelled before provider dispatch")

// ErrSemanticDeliveryDeferred leaves an exact queued directive pending reinspection.
var ErrSemanticDeliveryDeferred = errors.New("governed native submission awaits fresh evidence inspection")

// SemanticDeliveryLedger reserves a native write durably and never grants a
// second write for the same session/message key. Accepted entries survive
// later checkpoints and runtime replacements.
type SemanticDeliveryLedger interface {
	GetSemanticDelivery(context.Context, domain.SessionID, string) (domain.SemanticDelivery, bool, error)
	BeginSemanticDelivery(context.Context, domain.SessionID, string, string) (domain.SemanticDelivery, bool, error)
	AcceptSemanticDelivery(context.Context, domain.SessionID, string, string) error
	ResolveSemanticDelivery(context.Context, domain.SessionID, string, bool) error
}

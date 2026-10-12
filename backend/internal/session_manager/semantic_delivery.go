package sessionmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// SetSemanticDeliveryLedger installs the durable governed-submission reservation store.
func (m *Manager) SetSemanticDeliveryLedger(ledger ports.SemanticDeliveryLedger) {
	m.semanticDelivery = ledger
}

// ReconcileSemanticDelivery cancels a queued Chat submission before marking it unaccepted.
func (m *Manager) ReconcileSemanticDelivery(ctx context.Context, id domain.SessionID, key string, accepted bool) error {
	rec, found, err := m.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	if domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeChat {
		return nil
	}
	reconciler, ok := m.chat.(interface {
		ReconcileSemanticChatTurn(context.Context, domain.SessionID, string, bool) error
	})
	if !ok {
		return ErrSemanticAcceptanceUnsupported
	}
	return reconciler.ReconcileSemanticChatTurn(ctx, id, key, accepted)
}

// sendSemanticTUI fences the write before touching a native pane. A crash or
// missing submission signal leaves a permanent uncertain reservation, never a
// reason to silently repeat the native write on a replacement runtime.
func (m *Manager) sendSemanticTUI(ctx context.Context, rec domain.SessionRecord, message, key string) error {
	if !m.harnessSemanticAcceptance(rec.Harness) || key == "" || m.semanticDelivery == nil {
		return ErrSemanticAcceptanceUnsupported
	}
	if rec.IsTerminated || rec.Activity.State == domain.ActivityExited {
		return ErrTerminated
	}
	if rec.Activity.State == domain.ActivityBlocked {
		return ErrAwaitingDecision
	}
	delivery, fresh, err := m.semanticDelivery.BeginSemanticDelivery(ctx, rec.ID, key, rec.Metadata.RuntimeLaunchID)
	if err != nil {
		return err
	}
	if delivery.State == "accepted" {
		return nil
	}
	if delivery.State == "pending" && delivery.RuntimeLaunchID == rec.Metadata.RuntimeLaunchID && semanticMessageAccepted(rec, key) {
		if err := m.semanticDelivery.AcceptSemanticDelivery(ctx, rec.ID, key, delivery.RuntimeLaunchID); err != nil {
			return fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryUncertain, err)
		}
		return nil
	}
	if !fresh {
		return ports.ErrSemanticDeliveryUncertain
	}
	wrapped := domain.WrapReportDelivery(key, message)
	if err := m.send(ctx, rec.ID, wrapped, key, false); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryUncertain, err)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(m.sendConfirm.pollInterval)
	defer ticker.Stop()
	for {
		current, found, err := m.store.GetSession(ctx, rec.ID)
		if err != nil {
			return fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryUncertain, err)
		}
		if !found || current.IsTerminated || current.Activity.State == domain.ActivityExited || current.Metadata.RuntimeLaunchID != delivery.RuntimeLaunchID {
			return fmt.Errorf("%w: target runtime changed", ports.ErrSemanticDeliveryUncertain)
		}
		if semanticMessageAccepted(current, key) {
			if err := m.semanticDelivery.AcceptSemanticDelivery(ctx, rec.ID, key, delivery.RuntimeLaunchID); err != nil {
				return fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryUncertain, err)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryUncertain, ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("%w: correlated prompt submission was not observed", ports.ErrSemanticDeliveryUncertain)
		case <-ticker.C:
		}
	}
}

package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (c *Controller) recordSemanticAcceptance(ctx context.Context, providerTurnID string) error {
	if store, ok := c.store.(interface {
		AcceptSemanticChatTurn(context.Context, string, string) error
	}); ok {
		return store.AcceptSemanticChatTurn(ctx, c.conversation.ID, providerTurnID)
	}
	return nil
}

func (c *Controller) nativeSemanticAcceptance(event ports.ChatEvent) bool {
	if event.ProviderTurnID == "" {
		return false
	}
	switch event.Kind {
	case ports.ChatEventMessageDelta, ports.ChatEventMessageCompleted, ports.ChatEventUserMessageCompleted,
		ports.ChatEventApprovalRequested, ports.ChatEventInputRequested, ports.ChatEventActivityStarted, ports.ChatEventActivityCompleted:
		return true
	case ports.ChatEventTurnCompleted:
		return event.Err == nil && event.TurnState == domain.TurnStateCompleted
	case ports.ChatEventTurnStarted:
		_, deferred := c.conv.(ports.ChatDeferredTurnStarter)
		return !deferred
	}
	return false
}

// RelaySemanticChatTurn waits for a durable native receipt for this exact key.
// Empty duplicate results and queued turns are never a provider receipt.
func (s *Service) RelaySemanticChatTurn(ctx context.Context, id domain.SessionID, text, key string) (domain.SemanticDelivery, error) {
	ledger, ok := s.store.(ports.SemanticDeliveryLedger)
	if !ok || key == "" {
		return domain.SemanticDelivery{}, ports.ErrChatUnsupported
	}
	reserved, found, err := ledger.GetSemanticDelivery(ctx, id, key)
	if err != nil {
		return reserved, err
	}
	if !found || !strings.HasPrefix(reserved.RuntimeLaunchID, "chat:") || reserved.State == "cancelled" {
		return reserved, ports.ErrSemanticDeliveryUncertain
	}
	if reserved.State == "accepted" {
		return reserved, nil
	}
	controller, err := s.Controller(id)
	if err != nil {
		return domain.SemanticDelivery{}, err
	}
	if reserved.RuntimeLaunchID != "chat:"+controller.generation {
		_, exists, err := s.store.ConversationMessageByClientID(ctx, controller.conversation.ID, key)
		if err != nil || !exists {
			return reserved, fmt.Errorf("%w: reserved Chat generation changed before durable enqueue", ports.ErrSemanticDeliveryUncertain)
		}
	}
	if _, err := controller.Send(ctx, ports.ChatUserMessage{Text: text, ClientMessageID: key, Origin: domain.MessageOriginAutomation}); err != nil {
		if errors.Is(err, ports.ErrSemanticDeliveryDeferred) {
			reserved.State = "queued"
			return reserved, ports.ErrSemanticDeliveryQueued
		}
		if errors.Is(err, ports.ErrSemanticDeliveryRejected) {
			return reserved, err
		}
		return domain.SemanticDelivery{}, fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryUncertain, err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	lastReceipt := reserved
	timedOut := func() (domain.SemanticDelivery, error) {
		return lastReceipt, fmt.Errorf("%w: native Chat acceptance not observed", ports.ErrSemanticDeliveryUncertain)
	}
	for {
		if waitCtx.Err() != nil {
			return timedOut()
		}
		receipt, found, err := ledger.GetSemanticDelivery(waitCtx, id, key)
		if err != nil {
			if waitCtx.Err() != nil {
				return timedOut()
			}
			return receipt, fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryUncertain, err)
		}
		if found && receipt.State == "accepted" {
			return receipt, nil
		}
		message, exists, err := s.store.ConversationMessageByClientID(waitCtx, controller.conversation.ID, key)
		if err != nil || !exists {
			if waitCtx.Err() != nil {
				return timedOut()
			}
			return receipt, fmt.Errorf("%w: exact Chat delivery not found", ports.ErrSemanticDeliveryUncertain)
		}
		turn, err := s.store.TurnByID(waitCtx, message.TurnID)
		if err == nil && turn.State == domain.TurnStateCancelled && turn.ProviderTurnID == "" {
			return receipt, ports.ErrSemanticDeliveryRejected
		}
		if err != nil {
			if waitCtx.Err() != nil {
				return timedOut()
			}
			return receipt, fmt.Errorf("%w: %w", ports.ErrSemanticDeliveryUncertain, err)
		}
		if turn.State.Terminal() {
			return receipt, fmt.Errorf("%w: Chat turn ended without observed native acceptance", ports.ErrSemanticDeliveryUncertain)
		}
		if turn.State == domain.TurnStateQueued {
			// A previously deferred idle queue has no completion event to wake it.
			// Stable-ID retries may retry its existing FIFO entry, never append one.
			if !controller.busy() {
				if err := controller.drain(ctx); err != nil && !errors.Is(err, ports.ErrSemanticDeliveryDeferred) {
					return receipt, err
				}
				if current, found, err := ledger.GetSemanticDelivery(ctx, id, key); err == nil && found && current.State == "accepted" {
					return current, nil
				}
			}
			receipt.State = "queued"
			return receipt, ports.ErrSemanticDeliveryQueued
		}
		lastReceipt = receipt
		select {
		case <-waitCtx.Done():
			return timedOut()
		case <-ticker.C:
		}
	}
}

// ReconcileSemanticChatTurn removes an AO-owned queue entry before the
// ledger is tombstoned. An unknown/in-flight native request is never cancelled
// by relabeling its ledger row.
func (s *Service) ReconcileSemanticChatTurn(ctx context.Context, id domain.SessionID, key string, accepted bool) error {
	if accepted {
		return nil
	}
	conversation, err := s.store.ConversationForSession(ctx, id)
	if err != nil {
		return err
	}
	message, found, err := s.store.ConversationMessageByClientID(ctx, conversation.ID, key)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	turn, err := s.store.TurnByID(ctx, message.TurnID)
	if err != nil {
		return err
	}
	if turn.State == domain.TurnStateCancelled {
		return nil
	}
	if turn.State != domain.TurnStateQueued {
		return fmt.Errorf("%w: stop and reconcile the in-flight native Chat request before declaring it unaccepted", ports.ErrSemanticDeliveryUncertain)
	}
	controller, err := s.Controller(id)
	if err != nil {
		return err
	}
	// Native dispatch owns sendMu across provider I/O. Never let a Stopp request
	// wait indefinitely for that I/O while claiming its queue was cancelled.
	if !controller.sendMu.TryLock() {
		return fmt.Errorf("%w: native queue is crossing a submission boundary", ports.ErrSemanticDeliveryUncertain)
	}
	defer controller.sendMu.Unlock()
	if controller.handoffActive() {
		return ErrControllerHandoff
	}
	return controller.store.CancelQueuedTurnByID(ctx, controller.conversation.ID, message.TurnID, controller.now())
}

// SetSemanticDispatchValidator installs admission checks before recovery starts;
// all future queues revalidate actual
// native admission against the durable directive and current work policy.
func (s *Service) SetSemanticDispatchValidator(validator func(context.Context, domain.SessionID, string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validateSemanticDispatch = validator
	for _, controller := range s.controllers {
		controller.mu.Lock()
		controller.validateSemanticDispatch = validator
		controller.mu.Unlock()
	}
	for _, controller := range s.ownerControllers {
		controller.mu.Lock()
		controller.validateSemanticDispatch = validator
		controller.mu.Unlock()
	}
}

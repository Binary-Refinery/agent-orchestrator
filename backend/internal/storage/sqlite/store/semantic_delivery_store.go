package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// GetSemanticDelivery reads the durable receipt for one native idempotency key.
func (s *Store) GetSemanticDelivery(ctx context.Context, id domain.SessionID, key string) (domain.SemanticDelivery, bool, error) {
	record, err := s.qr.GetSemanticDelivery(ctx, gen.GetSemanticDeliveryParams{SessionID: string(id), ClientMessageID: key})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SemanticDelivery{}, false, nil
	}
	if err != nil {
		return domain.SemanticDelivery{}, false, err
	}
	return domain.SemanticDelivery{SessionID: domain.SessionID(record.SessionID), ClientMessageID: record.ClientMessageID, RuntimeLaunchID: record.RuntimeLaunchID, State: record.State}, true, nil
}

// BeginSemanticDelivery reserves a key before submitting any native message.
func (s *Store) BeginSemanticDelivery(ctx context.Context, id domain.SessionID, key, generation string) (domain.SemanticDelivery, bool, error) {
	if id == "" || key == "" || generation == "" {
		return domain.SemanticDelivery{}, false, errors.New("native semantic delivery needs session, key and runtime generation")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.SemanticDelivery{}, false, err
	}
	defer s.writeMu.Unlock()
	count, err := s.qw.BeginSemanticDelivery(ctx, gen.BeginSemanticDeliveryParams{SessionID: string(id), ClientMessageID: key, RuntimeLaunchID: generation, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return domain.SemanticDelivery{}, false, err
	}
	record, err := s.qw.GetSemanticDelivery(ctx, gen.GetSemanticDeliveryParams{SessionID: string(id), ClientMessageID: key})
	if err != nil {
		return domain.SemanticDelivery{}, false, err
	}
	return domain.SemanticDelivery{SessionID: domain.SessionID(record.SessionID), ClientMessageID: record.ClientMessageID, RuntimeLaunchID: record.RuntimeLaunchID, State: record.State}, count == 1, nil
}

// AcceptSemanticDelivery records acceptance only for its reserved runtime generation.
func (s *Store) AcceptSemanticDelivery(ctx context.Context, id domain.SessionID, key, generation string) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	count, err := s.qw.AcceptSemanticDelivery(ctx, gen.AcceptSemanticDeliveryParams{SessionID: string(id), ClientMessageID: key, RuntimeLaunchID: generation, AcceptedAt: sql.NullString{String: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}})
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("native semantic acceptance does not match its reserved generation")
	}
	return nil
}

// ResolveSemanticDelivery records an explicit reconciliation without downgrading acceptance.
func (s *Store) ResolveSemanticDelivery(ctx context.Context, id domain.SessionID, key string, accepted bool) error {
	if id == "" || key == "" {
		return errors.New("semantic resolution needs session and dispatch identity")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	record, err := s.qw.GetSemanticDelivery(ctx, gen.GetSemanticDeliveryParams{SessionID: string(id), ClientMessageID: key})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} // An unreserved dispatch has not reached native submission.
	if err != nil {
		return err
	}
	if !accepted && record.State == "accepted" {
		return errors.New("accepted native delivery cannot be resolved as unsent")
	}
	state := "cancelled"
	at := sql.NullString{}
	if accepted {
		state = "accepted"
		at = sql.NullString{String: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}
	}
	_, err = s.qw.ResolveSemanticDelivery(ctx, gen.ResolveSemanticDeliveryParams{SessionID: string(id), ClientMessageID: key, State: state, AcceptedAt: at})
	return err
}

// AcceptSemanticChatTurn is called only for observed native acceptance, never
// for AO queue insertion, synthetic ACP started events or a successful paste.
func (s *Store) AcceptSemanticChatTurn(ctx context.Context, conversationID, providerTurnID string) error {
	if conversationID == "" || providerTurnID == "" {
		return nil
	}
	q, unlock := s.conversationWriter(ctx)
	defer unlock()
	_, err := q.AcceptSemanticChatTurn(ctx, gen.AcceptSemanticChatTurnParams{ConversationID: conversationID, ProviderTurnID: providerTurnID, AcceptedAt: sql.NullString{String: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}})
	return err
}

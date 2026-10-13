package sessionmanager

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// InterruptInstructorProducer is a native control operation, not a model turn.
// The operation lease and immutable row key prevent stopping a replacement.
func (m *Manager) InterruptInstructorProducer(ctx context.Context, id domain.SessionID, instance string) error {
	release, admitted := m.AcquireSessionInput(id)
	if !admitted {
		return errAgentOperationInProgress
	}
	defer release()
	reader, ok := m.store.(interface {
		InstructorSessionForMembership(context.Context, domain.SessionID) (domain.SessionRecord, string, bool, error)
	})
	if !ok {
		return fmt.Errorf("%w: immutable producer lookup unavailable", ErrSemanticAcceptanceUnsupported)
	}
	rec, key, found, err := reader.InstructorSessionForMembership(ctx, id)
	if err != nil {
		return err
	}
	if !found || key != instance {
		return domain.ErrInstructorProducerChanged
	}
	if rec.IsTerminated || rec.Activity.State == domain.ActivityExited {
		return nil
	}
	if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
		interrupter, ok := m.chat.(interface {
			InterruptChat(context.Context, domain.SessionID) error
		})
		if !ok {
			return ErrSemanticAcceptanceUnsupported
		}
		return interrupter.InterruptChat(ctx, id)
	}
	interrupter, ok := m.runtime.(runtimeInterrupter)
	if !ok || rec.Metadata.RuntimeHandleID == "" {
		return ErrSemanticAcceptanceUnsupported
	}
	return interrupter.Interrupt(ctx, ports.RuntimeHandle{ID: rec.Metadata.RuntimeHandleID})
}

// InstructorProducerQuiescent never treats a failed probe or a TUI idle hook as
// native termination proof. Chat requires the live controller's own quiet seam.
func (m *Manager) InstructorProducerQuiescent(ctx context.Context, id domain.SessionID, instance string) (bool, error) {
	release, admitted := m.AcquireSessionInput(id)
	if !admitted {
		return false, errAgentOperationInProgress
	}
	defer release()
	reader, ok := m.store.(interface {
		InstructorSessionForMembership(context.Context, domain.SessionID) (domain.SessionRecord, string, bool, error)
	})
	if !ok {
		return false, ErrSemanticAcceptanceUnsupported
	}
	rec, key, found, err := reader.InstructorSessionForMembership(ctx, id)
	if err != nil {
		return false, err
	}
	if !found || key != instance {
		return false, domain.ErrInstructorProducerChanged
	}
	if rec.IsTerminated || rec.Activity.State == domain.ActivityExited {
		return true, nil
	}
	if domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeChat {
		return false, nil
	}
	observer, ok := m.chat.(interface {
		InstructorChatQuiescent(context.Context, domain.SessionID) (bool, error)
	})
	if !ok {
		return false, ErrSemanticAcceptanceUnsupported
	}
	return observer.InstructorChatQuiescent(ctx, id)
}

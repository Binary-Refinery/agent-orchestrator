package domain

import (
	"context"
	"errors"
	"time"
)

// ErrSessionRecoveryChanged rejects consent captured for a different controller
// or a previous session instance that happened to use the same display ID.
var ErrSessionRecoveryChanged = errors.New("session changed since recovery was prepared")

type sessionRecoveryExpectation struct {
	id        SessionID
	createdAt time.Time
	owner     SessionControllerOwner
}

type sessionRecoveryExpectationKey struct{}

// WithSessionRecoveryExpectation binds a guided recovery to observed durable
// facts. The session manager checks it after acquiring the operation gate.
func WithSessionRecoveryExpectation(ctx context.Context, rec SessionRecord) context.Context {
	return context.WithValue(ctx, sessionRecoveryExpectationKey{}, sessionRecoveryExpectation{
		id: rec.ID, createdAt: rec.CreatedAt, owner: rec.ControllerOwner(),
	})
}

// SessionRecoveryRequested reports an explicit, identity-bound recovery. Its
// native continuity requirement also applies to older Instructor works whose
// project has not opted into managed mode.
func SessionRecoveryRequested(ctx context.Context) bool {
	_, ok := ctx.Value(sessionRecoveryExpectationKey{}).(sessionRecoveryExpectation)
	return ok
}

// ValidateSessionRecoveryExpectation preserves legacy direct callers while
// fencing guided recovery against a replaced session or controller generation.
func ValidateSessionRecoveryExpectation(ctx context.Context, rec SessionRecord) error {
	if expected, ok := ctx.Value(sessionRecoveryExpectationKey{}).(sessionRecoveryExpectation); ok {
		if rec.ID != expected.id || !rec.CreatedAt.Equal(expected.createdAt) || rec.ControllerOwner() != expected.owner {
			return ErrSessionRecoveryChanged
		}
	}
	return nil
}

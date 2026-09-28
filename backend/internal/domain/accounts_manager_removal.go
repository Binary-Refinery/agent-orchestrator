package domain

import (
	"errors"
	"time"
)

var (
	ErrAccountsManagerAccountInUse    = errors.New("account is in use; confirm the affected sessions before removal")
	ErrAccountsManagerAccountDeleting = errors.New("account removal is pending or completed")
	ErrAccountsManagerRemovalConflict = errors.New("account removal or its affected sessions changed")
)

type AccountsManagerRemovalPhase string

// Removal phases distinguish cancellable intent from irreversible recovery.
const (
	AccountsManagerRemovalRequested AccountsManagerRemovalPhase = "requested"
	AccountsManagerRemovalStopping  AccountsManagerRemovalPhase = "stopping"
	AccountsManagerRemovalRevoked   AccountsManagerRemovalPhase = "revoked"
	AccountsManagerRemovalComplete  AccountsManagerRemovalPhase = "complete"
	AccountsManagerRemovalRecovery  AccountsManagerRemovalPhase = "recovery_required"
	AccountsManagerRemovalCancelled AccountsManagerRemovalPhase = "cancelled"
)

// Terminal prevents completed or cancelled journals from admitting more work.
func (p AccountsManagerRemovalPhase) Terminal() bool {
	return p == AccountsManagerRemovalComplete || p == AccountsManagerRemovalCancelled
}

type AccountsManagerRemovalSession struct {
	SessionID       SessionID
	Provider        AccountsManagerProvider
	BindingRevision int64
	Owner           SessionControllerOwner
	RuntimeHandleID string
	Stopped         bool
}

// OwnsController excludes dormant provider bindings from active controller teardown.
func (s AccountsManagerRemovalSession) OwnsController() bool {
	return (s.Provider == AccountsManagerProviderCodex && s.Owner.Harness == HarnessCodex) ||
		(s.Provider == AccountsManagerProviderClaude && s.Owner.Harness == HarnessClaudeCode)
}

type AccountsManagerRemovalImpact struct {
	Revision int64
	Sessions []AccountsManagerRemovalSession
}

type AccountsManagerRemoval struct {
	ID              string
	AccountID       string
	Impact          AccountsManagerRemovalImpact
	Phase           AccountsManagerRemovalPhase
	ErrorCode       string
	StopStarted     bool
	BindingsRevoked bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

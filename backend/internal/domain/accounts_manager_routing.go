package domain

import (
	"errors"
	"time"
)

var ErrAccountsManagerBindingConflict = errors.New("accounts manager session binding changed")

type AccountsManagerConnectionMode string

const (
	AccountsManagerNative  AccountsManagerConnectionMode = "native"
	AccountsManagerManaged AccountsManagerConnectionMode = "managed"
)

// AccountsManagerProvider identifies a provider supported by AO's embedded
// Accounts Manager. It is intentionally narrower than AgentHarness.
type AccountsManagerProvider string

// AccountsManagerProviderCodex and the other provider constants restrict managed routing support.
const (
	AccountsManagerProviderCodex  AccountsManagerProvider = "codex"
	AccountsManagerProviderClaude AccountsManagerProvider = "claude"
)

// Valid rejects providers outside the managed-routing contract.
func (p AccountsManagerProvider) Valid() bool {
	return p == AccountsManagerProviderCodex || p == AccountsManagerProviderClaude
}

// AccountsManagerRoutingPolicy holds the explicit default for new sessions.
type AccountsManagerRoutingPolicy struct {
	Provider   AccountsManagerProvider
	Enabled    bool
	AccountIDs []string
}

// AccountsManagerSessionRoute pins one provider in one AO session to a single
// public-safe account identifier.
type AccountsManagerSessionRoute struct {
	Blocked   bool
	Mode      AccountsManagerConnectionMode
	Revision  int64
	SessionID SessionID
	Provider  AccountsManagerProvider
	AccountID string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AccountsManagerBindingSnapshot struct {
	Revision int64
	Bindings []AccountsManagerSessionRoute
}

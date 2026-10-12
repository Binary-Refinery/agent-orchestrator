package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// InstructorBudgetReservationID keeps retries stable across daemon generations.
func InstructorBudgetReservationID(workID, kind, resourceKey string) string {
	digest := sha256.Sum256([]byte(workID + "\x00" + kind + "\x00" + resourceKey))
	return "budget-" + hex.EncodeToString(digest[:])
}

// ErrInstructorBudgetExceeded rejects admission before a managed native effect.
var ErrInstructorBudgetExceeded = errors.New("instructor work budget exhausted")

// ErrInstructorBudgetClaimed refuses a repeated native effect, even while its
// first attempt has not finished. Reservation reuse is not execution authority.
var ErrInstructorBudgetClaimed = errors.New("native budget effect already claimed; reconcile before retry")

// InstructorBudgets separates live capacity from total lifetime consumption.
// A nil budgets policy preserves old pinned policies. Zero explicitly means
// unlimited for that individual limit; changing limits creates a new policy hash.
type InstructorBudgets struct {
	MaxConcurrentSessions int `json:"maxConcurrentSessions"`
	MaxSessions           int `json:"maxSessions"`
	MaxTurns              int `json:"maxTurns"`
	MaxDelegations        int `json:"maxDelegations"`
}

// InstructorBudgetEntry is a durable pre-effect reservation. Resource keys bind
// sessions to immutable instance IDs and turns to daemon-owned durable IDs.
// Unknown effects remain reserved/uncertain and cannot replenish the budget.
type InstructorBudgetEntry struct {
	ID            string    `json:"id"`
	WorkID        string    `json:"workId"`
	PolicyVersion string    `json:"policyVersion"`
	Kind          string    `json:"kind"`
	ResourceKey   string    `json:"resourceKey"`
	SessionID     SessionID `json:"sessionId,omitempty"`
	ReviewID      string    `json:"reviewId,omitempty"`
	Delegated     bool      `json:"delegated"`
	// Adoption already consumes a lifetime slot even before AO observes a start.
	// An unrelated, definitely unstarted target attempt cannot refund that source.
	AdoptedSession bool `json:"adoptedSession,omitempty"`
	// An already-ended controller can hold a new prelaunch reservation while
	// being restored. Its old exit fact must not release that pending slot.
	InactiveOnAdmission bool `json:"inactiveOnAdmission,omitempty"`
	// A replacement reservation preserves its already-consumed lifetime slot
	// when that replacement is positively proven never to have started.
	PriorSessionState string    `json:"priorSessionState,omitempty"`
	State             string    `json:"state"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// InstructorBudgetUsage is measured from reservations, never ephemeral counters.
type InstructorBudgetUsage struct {
	ConcurrentSessions int `json:"concurrentSessions"`
	Sessions           int `json:"sessions"`
	Turns              int `json:"turns"`
	Delegations        int `json:"delegations"`
}

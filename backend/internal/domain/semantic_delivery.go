package domain

// SemanticDelivery preserves native submission identity across controller
// generations. Pending is intentionally uncertain after a process failure.
type SemanticDelivery struct {
	SessionID       SessionID
	ClientMessageID string
	RuntimeLaunchID string
	State           string
}

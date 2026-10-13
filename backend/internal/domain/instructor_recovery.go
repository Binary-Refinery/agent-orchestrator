package domain

import "time"

// InstructorRecoveryStatus is a current observation of the exact project
// controller required by a work. It is not execution or publication authority.
type InstructorRecoveryStatus struct {
	WorkID                 string    `json:"workId"`
	WorkRevision           int64     `json:"workRevision"`
	PolicyVersion          string    `json:"policyVersion"`
	SessionID              SessionID `json:"sessionId,omitempty"`
	SessionCreatedAt       time.Time `json:"sessionCreatedAt,omitempty"`
	ProviderConversationID string    `json:"providerConversationId,omitempty"`
	ControllerGeneration   string    `json:"controllerGeneration,omitempty"`
	Ready                  bool      `json:"ready"`
	Action                 string    `json:"action" enum:"resume,restore,none"`
	ReasonCode             string    `json:"reasonCode"`
	Reason                 string    `json:"reason"`
}

// InstructorRecoveryInput captures the observed work and controller incarnation.
type InstructorRecoveryInput struct {
	WorkID                 string    `json:"workId"`
	WorkRevision           int64     `json:"workRevision"`
	PolicyVersion          string    `json:"policyVersion"`
	SessionID              SessionID `json:"sessionId"`
	SessionCreatedAt       time.Time `json:"sessionCreatedAt"`
	ProviderConversationID string    `json:"providerConversationId"`
	ControllerGeneration   string    `json:"controllerGeneration"`
}

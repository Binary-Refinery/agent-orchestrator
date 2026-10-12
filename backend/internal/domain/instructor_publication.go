package domain

import "time"

// InstructorPublicationRepository pins a daemon-read repository association at
// work registration; mutable project settings cannot erase this historical scope.
type InstructorPublicationRepository struct {
	Host       string `json:"host"`
	Repository string `json:"repository"`
}

// InstructorPublicationCapabilities reports enforced boundaries, never prompt
// instructions, environment flags, or a model's claimed permissions.
type InstructorPublicationCapabilities struct {
	Available           bool   `json:"available"`
	WorkerIsolation     bool   `json:"workerIsolation"`
	ProtectedOperator   bool   `json:"protectedOperator"`
	IsolatedCredentials bool   `json:"isolatedCredentials"`
	Reason              string `json:"reason,omitempty"`
}

// InstructorPublicationExecution fences a consumed grant before any external
// effect. An interrupted or failed attempt is not retried automatically.
type InstructorPublicationExecution struct {
	ID          string     `json:"id"`
	State       string     `json:"state"`
	ClaimedAt   time.Time  `json:"claimedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Ref         string     `json:"ref,omitempty"`
	Error       string     `json:"error,omitempty"`
}

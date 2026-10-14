package domain

import "time"

// InstructorDiagnosticScope is caller-captured membership evidence. An empty
// RuntimeGeneration is an exact unknown value, never a wildcard.
type InstructorDiagnosticScope struct {
	WorkID            string
	ProjectID         ProjectID
	MemberID          string
	SessionID         SessionID
	InstanceID        string
	RuntimeGeneration string
}

// InstructorDiagnosticCapture stays inside the daemon. Ownership and all of
// these persisted facts are read from the same database snapshot.
type InstructorDiagnosticCapture struct {
	WorkRevision   int64
	Kind           string
	Session        SessionRecord
	Review         *Review
	ReviewRun      *ReviewRun
	Conversation   ConversationRecord
	Turns          []ConversationTurn
	Messages       []ConversationMessage
	Activities     []ConversationActivity
	OldestSequence int64
	HasMoreBefore  bool
	TimelineScope  string
	TimelineReason string
}

// InstructorMemberDiagnostics deliberately contains persisted observations,
// not a claim that the controller is currently alive or ready.
type InstructorMemberDiagnostics struct {
	Schema            string                       `json:"schema"`
	WorkID            string                       `json:"workId"`
	ProjectID         ProjectID                    `json:"projectId"`
	WorkRevision      int64                        `json:"workRevision"`
	MemberID          string                       `json:"memberId"`
	Kind              string                       `json:"kind" enum:"session,review"`
	SessionID         SessionID                    `json:"sessionId"`
	InstanceID        string                       `json:"instanceId"`
	RuntimeGeneration string                       `json:"runtimeGeneration"`
	CapturedAt        time.Time                    `json:"capturedAt"`
	Atomic            bool                         `json:"atomic"`
	PersistedOnly     bool                         `json:"persistedOnly"`
	Session           InstructorDiagnosticSession  `json:"session"`
	Review            *InstructorDiagnosticReview  `json:"review,omitempty"`
	Timeline          InstructorDiagnosticTimeline `json:"timeline"`
}

// InstructorDiagnosticSession excludes launch prompts, credentials, paths and
// approval/replay payloads. Model and effort are recorded launch choices only.
type InstructorDiagnosticSession struct {
	Mode                   SessionMode           `json:"mode"`
	Harness                AgentHarness          `json:"harness"`
	ActivityState          ActivityState         `json:"activityState"`
	IsTerminated           bool                  `json:"isTerminated"`
	ControllerGeneration   string                `json:"controllerGeneration"`
	ControllerError        string                `json:"controllerError,omitempty"`
	ProvisionState         SessionProvisionState `json:"provisionState"`
	ProvisionError         string                `json:"provisionError,omitempty"`
	ProviderConversationID string                `json:"providerConversationId,omitempty"`
	RuntimeLaunchID        string                `json:"runtimeLaunchId,omitempty"`
	Model                  string                `json:"model,omitempty"`
	Effort                 string                `json:"effort,omitempty"`
	Truncated              bool                  `json:"truncated"`
	Redacted               bool                  `json:"redacted"`
}

// InstructorDiagnosticReview identifies the selected run independently of its
// review-wide conversation. It never turns a review claim into delivery proof.
type InstructorDiagnosticReview struct {
	ReviewID             string          `json:"reviewId"`
	ReviewRunID          string          `json:"reviewRunId"`
	Harness              ReviewerHarness `json:"harness"`
	Status               ReviewRunStatus `json:"status"`
	Verdict              ReviewVerdict   `json:"verdict"`
	ControllerGeneration string          `json:"controllerGeneration"`
	ControllerError      string          `json:"controllerError,omitempty"`
	Truncated            bool            `json:"truncated"`
	Redacted             bool            `json:"redacted"`
}

// InstructorDiagnosticTimeline is bounded persisted narrative. Activity
// details, commands, tool arguments/results, reasoning and replay are omitted.
type InstructorDiagnosticTimeline struct {
	Available             bool                       `json:"available"`
	Reason                string                     `json:"reason,omitempty"`
	Scope                 string                     `json:"scope" enum:"session,review,project"`
	ScopeNote             string                     `json:"scopeNote,omitempty"`
	ConversationID        string                     `json:"conversationId,omitempty"`
	LatestSequence        int64                      `json:"latestSequence"`
	ConversationUpdatedAt *time.Time                 `json:"conversationUpdatedAt,omitempty"`
	Items                 []InstructorDiagnosticItem `json:"items"`
	Turns                 []InstructorDiagnosticTurn `json:"turns"`
	OldestSequence        int64                      `json:"oldestSequence"`
	HasMoreBefore         bool                       `json:"hasMoreBefore"`
	NextBeforeSequence    int64                      `json:"nextBeforeSequence,omitempty"`
	Truncated             bool                       `json:"truncated"`
	Redacted              bool                       `json:"redacted"`
}

// InstructorDiagnosticItem is a safe projection, not a raw activity payload.
type InstructorDiagnosticItem struct {
	Kind         string    `json:"kind" enum:"message,activity"`
	ID           string    `json:"id"`
	Sequence     int64     `json:"sequence"`
	Revision     int64     `json:"revision"`
	Streaming    bool      `json:"streaming"`
	TurnID       string    `json:"turnId,omitempty"`
	Role         string    `json:"role,omitempty"`
	ActivityKind string    `json:"activityKind,omitempty"`
	Status       string    `json:"status,omitempty"`
	Text         string    `json:"text,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Truncated    bool      `json:"truncated"`
	Redacted     bool      `json:"redacted"`
}

// InstructorDiagnosticTurn retains error and lifecycle facts without prompts,
// diffs, plans, opaque replay identities or provider request capabilities.
type InstructorDiagnosticTurn struct {
	ID           string     `json:"id"`
	State        TurnState  `json:"state"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	RequestedAt  time.Time  `json:"requestedAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	Truncated    bool       `json:"truncated"`
	Redacted     bool       `json:"redacted"`
}

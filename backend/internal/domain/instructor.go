package domain

import "time"

// InstructorWork is durable business state, distinct from derived session status.
type InstructorWork struct {
	ID                   string                       `json:"id"`
	ProjectID            ProjectID                    `json:"projectId"`
	Name                 string                       `json:"name"`
	Goal                 string                       `json:"goal"`
	Source               string                       `json:"source"`
	SourceRef            string                       `json:"sourceRef"`
	IssueID              IssueID                      `json:"issueId,omitempty"`
	SessionIDs           []SessionID                  `json:"sessionIds"`
	Launches             []InstructorLaunch           `json:"launches"`
	State                string                       `json:"state"`
	Phase                string                       `json:"phase"`
	PhaseStartedAt       time.Time                    `json:"phaseStartedAt"`
	PhaseSequence        int64                        `json:"phaseSequence"`
	FixPhaseSequence     int64                        `json:"fixPhaseSequence"`
	DeltaPhaseSequence   int64                        `json:"deltaPhaseSequence"`
	Summary              string                       `json:"summary"`
	Next                 string                       `json:"next"`
	Branch               string                       `json:"branch,omitempty"`
	SHA                  string                       `json:"sha,omitempty"`
	PolicyVersion        string                       `json:"policyVersion"`
	Policy               InstructorPolicy             `json:"policy"`
	Gate                 *InstructorGate              `json:"gate,omitempty"`
	SuspendedGate        *InstructorGate              `json:"suspendedGate,omitempty"`
	UncertainDispatchIDs []string                     `json:"uncertainDispatchIds,omitempty"`
	Verification         *InstructorVerification      `json:"verification,omitempty"`
	Heartbeat            *InstructorHeartbeat         `json:"heartbeat,omitempty"`
	Evidence             []InstructorEvidence         `json:"evidence"`
	FixRounds            int                          `json:"fixRounds"`
	OpenCriticalFindings int                          `json:"openCriticalFindings"`
	WaitCondition        string                       `json:"waitCondition,omitempty"`
	WaitUntil            *time.Time                   `json:"waitUntil,omitempty"`
	NudgedAt             *time.Time                   `json:"nudgedAt,omitempty"`
	Revision             int64                        `json:"revision"`
	CreatedAt            time.Time                    `json:"createdAt"`
	UpdatedAt            time.Time                    `json:"updatedAt"`
	ProgressAt           time.Time                    `json:"progressAt"`
	Actions              []string                     `json:"actions"`
	ApprovedTransitions  []string                     `json:"approvedTransitions,omitempty"`
	Exceptions           []InstructorException        `json:"exceptions,omitempty"`
	PublicationGrants    []InstructorPublicationGrant `json:"publicationGrants,omitempty"`
}

// InstructorPublicationGrant records an operator decision for the exact captured commit.
// It does not claim that a push, pull request or remote check actually happened.
type InstructorPublicationGrant struct {
	GateID        string    `json:"gateId"`
	WorkID        string    `json:"workId"`
	PolicyVersion string    `json:"policyVersion"`
	SHA           string    `json:"sha"`
	Branch        string    `json:"branch"`
	SnapshotID    string    `json:"snapshotId"`
	WorkRevision  int64     `json:"workRevision"`
	Approval      string    `json:"approval"`
	ApprovedAt    time.Time `json:"approvedAt"`
}

// InstructorHeartbeat records availability without changing work progress or authority.
type InstructorHeartbeat struct {
	WorkID    string    `json:"workId"`
	SessionID SessionID `json:"sessionId"`
	Role      string    `json:"role"`
	At        time.Time `json:"at"`
	Summary   string    `json:"summary,omitempty"`
}

// InstructorVerification caches daemon-owned inspection separately from work revisions.
// Its source key and bounded attempts survive process restarts.
type InstructorVerification struct {
	Key       string    `json:"key"`
	Inspected bool      `json:"inspected"`
	Passes    int       `json:"passes"`
	MaxPasses int       `json:"maxPasses"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Claims    int       `json:"claims"`
	Verified  int       `json:"verified"`
	LastRunAt time.Time `json:"lastRunAt"`
	NextRunAt time.Time `json:"nextRunAt"`
}

// InstructorLaunch separates requested, sent and native-reported configuration.
// Requested and sent configuration never imply native provider confirmation.
type InstructorLaunch struct {
	SessionID     SessionID                 `json:"sessionId,omitempty"`
	ReviewID      string                    `json:"reviewId,omitempty"`
	ReviewRunID   string                    `json:"reviewRunId,omitempty"`
	SHA           string                    `json:"sha,omitempty"`
	Role          string                    `json:"role"`
	Phase         string                    `json:"phase"`
	PhaseSequence int64                     `json:"phaseSequence"`
	Requested     InstructorAgentSelection  `json:"requested"`
	Sent          *InstructorAgentSelection `json:"sent,omitempty"`
	Confirmed     *InstructorAgentSelection `json:"confirmed,omitempty"`
	Status        string                    `json:"status"`
	LaunchedAt    time.Time                 `json:"launchedAt"`
}

// InstructorAgentSelection stores exact provider-native harness, model and effort values.
type InstructorAgentSelection struct {
	Harness string `json:"harness"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
}

// InstructorGate holds the current decision or condition preventing work execution.
type InstructorGate struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Title           string    `json:"title"`
	ExactApproval   string    `json:"exactApproval,omitempty"`
	OpenedAt        time.Time `json:"openedAt"`
	TransitionTo    string    `json:"transitionTo,omitempty"`
	DispatchID      string    `json:"dispatchId,omitempty"`
	TargetSessionID SessionID `json:"targetSessionId,omitempty"`
}

// InstructorEvidence binds a claim or daemon attestation to one work, policy and commit.
type InstructorEvidence struct {
	ID               string    `json:"id"`
	WorkID           string    `json:"workId"`
	PolicyVersion    string    `json:"policyVersion"`
	SHA              string    `json:"sha"`
	Kind             string    `json:"kind"`
	Phase            string    `json:"phase"`
	PhaseSequence    int64     `json:"phaseSequence"`
	Profile          string    `json:"profile,omitempty"`
	Producer         SessionID `json:"producer"`
	Role             string    `json:"role"`
	Harness          string    `json:"harness,omitempty"`
	Model            string    `json:"model,omitempty"`
	Effort           string    `json:"effort,omitempty"`
	Ref              string    `json:"ref"`
	Verified         bool      `json:"verified"`
	Revoked          bool      `json:"revoked,omitempty"`
	RevocationReason string    `json:"revocationReason,omitempty"`
	Outcome          string    `json:"outcome,omitempty"`
	CriticalFindings int       `json:"criticalFindings,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
}

// InstructorPolicy pins role choices, workflow prerequisites and executable check profiles.
type InstructorPolicy struct {
	Schema               int                          `json:"schema"`
	Version              string                       `json:"version"`
	InitialPhase         string                       `json:"initialPhase"`
	Roles                map[string]InstructorRole    `json:"roles"`
	Profiles             map[string]InstructorProfile `json:"profiles"`
	Transitions          []InstructorTransition       `json:"transitions"`
	CloseoutRequires     []string                     `json:"closeoutRequires"`
	MaxFixRounds         int                          `json:"maxFixRounds"`
	NudgeAfterSeconds    int                          `json:"nudgeAfterSeconds"`
	EscalateAfterSeconds int                          `json:"escalateAfterSeconds"`
	Commands             []InstructorCommandSpec      `json:"commands,omitempty"`
	Attribution          InstructorAttribution        `json:"attribution"`
	Traceability         InstructorTraceability       `json:"traceability"`
	ExceptionVersion     string                       `json:"exceptionVersion,omitempty"`
}

// InstructorException records a bounded operator-approved deviation for one work.
type InstructorException struct {
	ID                  string                    `json:"id"`
	Kind                string                    `json:"kind"`
	Role                string                    `json:"role,omitempty"`
	Selection           *InstructorAgentSelection `json:"selection,omitempty"`
	MaxFixRounds        int                       `json:"maxFixRounds,omitempty"`
	ProposalHash        string                    `json:"proposalHash"`
	SnapshotID          string                    `json:"snapshotId"`
	SHA                 string                    `json:"sha,omitempty"`
	Approval            string                    `json:"approval"`
	Reason              string                    `json:"reason"`
	BeforePolicyVersion string                    `json:"beforePolicyVersion"`
	AfterPolicyVersion  string                    `json:"afterPolicyVersion"`
	GrantedAt           time.Time                 `json:"grantedAt"`
	ExpiresAt           time.Time                 `json:"expiresAt"`
	Superseded          bool                      `json:"superseded"`
}

// InstructorAttribution is verified against the immutable commit; it never rewrites
// existing history or substitutes the agent identity for the operator.
type InstructorAttribution struct {
	AuthorName     string `json:"authorName"`
	AuthorEmail    string `json:"authorEmail"`
	CoAuthorName   string `json:"coAuthorName"`
	CoAuthorEmail  string `json:"coAuthorEmail"`
	SessionTrailer string `json:"sessionTrailer"`
	IssueTrailer   string `json:"issueTrailer"`
}

// InstructorTraceability declares exact destinations and tracker evidence. An empty
// destination list is not an unrestricted grant.
type InstructorTraceability struct {
	Required            bool              `json:"required"`
	RequireAssignee     bool              `json:"requireAssignee"`
	PhaseLabels         map[string]string `json:"phaseLabels,omitempty"`
	ProjectNumber       int               `json:"projectNumber"`
	ProjectOwner        string            `json:"projectOwner,omitempty"`
	BoardStatuses       map[string]string `json:"boardStatuses,omitempty"`
	RequiredPhases      []string          `json:"requiredPhases"`
	AllowedRepositories []string          `json:"allowedRepositories"`
	AllowedBaseBranches []string          `json:"allowedBaseBranches"`
}

// InstructorRole configures native choices and explicit per-model effort mappings.
type InstructorRole struct {
	Harness       string            `json:"harness"`
	Model         string            `json:"model"`
	Effort        string            `json:"effort"`
	Independent   bool              `json:"independent,omitempty"`
	EffortByModel map[string]string `json:"effortByModel,omitempty"`
}

// InstructorProfile declares approved check argv and its independent time budget.
type InstructorProfile struct {
	Command        []string `json:"command"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
}

// InstructorTransition declares the role, evidence and human authority for a phase change.
type InstructorTransition struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Role      string   `json:"role"`
	Requires  []string `json:"requires"`
	HumanGate bool     `json:"humanGate,omitempty"`
}

// InstructorCommandSpec describes one implemented operation and its admission contract.
type InstructorCommandSpec struct {
	Name             string                       `json:"name"`
	Aliases          []string                     `json:"aliases"`
	Action           string                       `json:"action"`
	Description      string                       `json:"description"`
	Mutates          bool                         `json:"mutates"`
	SnapshotRequired bool                         `json:"snapshotRequired"`
	Parameters       []InstructorCommandParameter `json:"parameters"`
	Scopes           []string                     `json:"scopes"`
	Authority        string                       `json:"authority"`
	Preconditions    []string                     `json:"preconditions"`
}

// InstructorCommandParameter describes a typed input advertised by the command registry.
type InstructorCommandParameter struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Enum     []string `json:"enum,omitempty"`
}

// InstructorMetrics counts business outcomes and attention items from persisted facts.
type InstructorMetrics struct {
	ClosedLast7Days         int `json:"closedLast7Days"`
	GatesClearedLast24Hours int `json:"gatesClearedLast24Hours"`
	Active                  int `json:"active"`
	Gated                   int `json:"gated"`
	Stalled                 int `json:"stalled"`
	Parked                  int `json:"parked"`
	RecordedViolations      int `json:"recordedViolations"`
	VerificationFailures    int `json:"verificationFailures"`
}

// InstructorEvent retains an immutable work lifecycle or authority observation.
type InstructorEvent struct {
	ID     string    `json:"id"`
	WorkID string    `json:"workId"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

// InstructorSnapshot freezes the observed work cohort used for command admission.
type InstructorSnapshot struct {
	ID               string                  `json:"id"`
	BaseID           string                  `json:"baseId,omitempty"`
	CreatedAt        time.Time               `json:"createdAt"`
	Works            []InstructorWork        `json:"works"`
	Commands         []InstructorCommandSpec `json:"commands"`
	Metrics          InstructorMetrics       `json:"metrics"`
	CommandConflicts []string                `json:"commandConflicts"`
}

// InstructorDispatch journals an ordered directive to an existing project orchestrator.
type InstructorDispatch struct {
	ID              string     `json:"id"`
	BatchID         string     `json:"batchId"`
	WorkID          string     `json:"workId"`
	ProjectID       ProjectID  `json:"projectId"`
	TargetSessionID SessionID  `json:"targetSessionId,omitempty"`
	SnapshotID      string     `json:"snapshotId"`
	WorkRevision    int64      `json:"workRevision"`
	Position        int        `json:"position"`
	Action          string     `json:"action"`
	Message         string     `json:"message"`
	CommandDigest   string     `json:"commandDigest,omitempty"`
	State           string     `json:"state"`
	Error           string     `json:"error,omitempty"`
	Attempts        int        `json:"attempts"`
	QueuedAt        *time.Time `json:"queuedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// InstructorCommandResult returns admitted dispatches, skipped work and read projections.
type InstructorCommandResult struct {
	BatchID    string               `json:"batchId,omitempty"`
	Snapshot   *InstructorSnapshot  `json:"snapshot,omitempty"`
	Dispatches []InstructorDispatch `json:"dispatches"`
	Skipped    []string             `json:"skipped"`
	Details    []InstructorDetails  `json:"details,omitempty"`
}

// InstructorDetails combines durable work, event history and native delivery receipts.
type InstructorDetails struct {
	Work       InstructorWork       `json:"work"`
	Events     []InstructorEvent    `json:"events"`
	Dispatches []InstructorDispatch `json:"dispatches"`
}

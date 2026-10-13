package domain

import "time"

// InstructorPolicyRebind preserves the exact operator-approved replacement and
// the authority it invalidated. Earlier evidence and budget entries keep their
// original policy binding; this receipt never grants permission to execute.
type InstructorPolicyRebind struct {
	ProposalHash               string           `json:"proposalHash"`
	SnapshotID                 string           `json:"snapshotId"`
	WorkRevision               int64            `json:"workRevision"`
	BeforePolicy               InstructorPolicy `json:"beforePolicy"`
	TargetPolicyVersion        string           `json:"targetPolicyVersion"`
	PolicySource               string           `json:"policySource"`
	Reason                     string           `json:"reason"`
	Approval                   string           `json:"approval"`
	PreviousState              string           `json:"previousState"`
	PreviousGate               *InstructorGate  `json:"previousGate,omitempty"`
	PreviousSuspendedGate      *InstructorGate  `json:"previousSuspendedGate,omitempty"`
	InvalidatedTransitions     []string         `json:"invalidatedTransitions,omitempty"`
	PreviousApprovalDispatchID string           `json:"previousApprovalDispatchId,omitempty"`
	SupersededExceptionIDs     []string         `json:"supersededExceptionIds,omitempty"`
	AppliedAt                  time.Time        `json:"appliedAt"`
}

package domain

import "time"

// InstructorPlanInput is a strategic request over an already observed portfolio.
type InstructorPlanInput struct {
	Request        string `json:"request"`
	SnapshotID     string `json:"snapshotId"`
	IdempotencyKey string `json:"idempotencyKey"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// InstructorProposal never contains human authority or a publication signature.
type InstructorProposal struct {
	Command string   `json:"command"`
	WorkIDs []string `json:"workIds"`
	Reason  string   `json:"reason"`
}

// InstructorPlanOutput is advisory; admission uses the existing command service.
type InstructorPlanOutput struct {
	Summary   string                 `json:"summary"`
	Questions []string               `json:"questions"`
	Proposals []InstructorProposal   `json:"proposals"`
	Intake    []InstructorPlanIntake `json:"intake"`
}

// InstructorPlanIntake captures an intake draft for explicit project admission.
type InstructorPlanIntake struct {
	ProjectID ProjectID `json:"projectId"`
	Name      string    `json:"name"`
	Goal      string    `json:"goal"`
	IssueID   IssueID   `json:"issueId,omitempty"`
}

// InstructorPlanJob is global durable dialogue, independent of project sessions.
type InstructorPlanJob struct {
	ID            string                    `json:"id"`
	RequestHash   string                    `json:"requestHash"`
	Input         InstructorPlanInput       `json:"input"`
	PolicyVersion string                    `json:"policyVersion"`
	Requested     InstructorAgentSelection  `json:"requested"`
	Sent          *InstructorAgentSelection `json:"sent,omitempty"`
	Confirmed     *InstructorAgentSelection `json:"confirmed,omitempty"`
	State         string                    `json:"state"`
	Output        *InstructorPlanOutput     `json:"output,omitempty"`
	Error         string                    `json:"error,omitempty"`
	CreatedAt     time.Time                 `json:"createdAt"`
	UpdatedAt     time.Time                 `json:"updatedAt"`
}

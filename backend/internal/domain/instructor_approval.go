package domain

// InstructorWorkApproval is one explicit gate decision in a captured, ordered
// cohort. A single command/snapshot/idempotency key journals the whole cohort.
type InstructorWorkApproval struct {
	WorkID               string `json:"workId"`
	Approval             string `json:"approval"`
	PublicationOperation string `json:"publicationOperation,omitempty"`
}

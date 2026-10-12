package domain

import (
	"errors"
	"time"
)

// ErrInstructorSessionInstanceChanged fences admission against a replaced row.
var ErrInstructorSessionInstanceChanged = errors.New("instructor session instance changed")

// InstructorSessionMembership preserves the admitted row identity independently
// of the reusable session display ID and its current controller generation.
type InstructorSessionMembership struct {
	// Prelaunch is set only by the manager before creating a native controller.
	Prelaunch       bool      `json:"prelaunch,omitempty"`
	ParentSessionID SessionID `json:"parentSessionId,omitempty"`
	SessionID       SessionID `json:"sessionId"`
	InstanceID      string    `json:"instanceId"`
	ProjectID       ProjectID `json:"projectId"`
	Role            string    `json:"role"`
	Name            string    `json:"name"`
	CreatedAt       time.Time `json:"createdAt"`
	Provenance      string    `json:"provenance"`
}

// InstructorMembers is a daemon-owned on-demand projection. Atomic is false:
// work and runtime facts can advance independently during collection.
type InstructorMembers struct {
	WorkID       string                `json:"workId"`
	ProjectID    ProjectID             `json:"projectId"`
	WorkRevision int64                 `json:"workRevision"`
	CapturedAt   time.Time             `json:"capturedAt"`
	Atomic       bool                  `json:"atomic"`
	Coordinator  InstructorCoordinator `json:"coordinator"`
	Members      []InstructorMember    `json:"members"`
}

// InstructorCoordinator separates the stable project role from its current
// runtime. A dispatch's historical target never becomes this role's identity.
type InstructorCoordinator struct {
	Key     string            `json:"key"`
	Current *InstructorMember `json:"current,omitempty"`
}

// InstructorMember does not infer provider confirmation from launch settings.
type InstructorMember struct {
	ID                string                      `json:"id"`
	Kind              string                      `json:"kind" enum:"session,review"`
	Role              string                      `json:"role"`
	Association       string                      `json:"association" enum:"current,historical,legacy_unknown"`
	Status            string                      `json:"status"`
	SessionID         SessionID                   `json:"sessionId,omitempty"`
	ReviewID          string                      `json:"reviewId,omitempty"`
	ReviewRunID       string                      `json:"reviewRunId,omitempty"`
	InstanceID        string                      `json:"instanceId,omitempty"`
	RuntimeGeneration string                      `json:"runtimeGeneration,omitempty"`
	Name              string                      `json:"name"`
	Phase             string                      `json:"phase,omitempty"`
	CreatedAt         *time.Time                  `json:"createdAt,omitempty"`
	Requested         *InstructorAgentSelection   `json:"requested,omitempty"`
	Sent              *InstructorAgentSelection   `json:"sent,omitempty"`
	Confirmed         *InstructorAgentSelection   `json:"confirmed,omitempty"`
	OpenTarget        *InstructorMemberOpenTarget `json:"openTarget,omitempty"`
	Reason            string                      `json:"reason,omitempty"`
}

// InstructorMemberOpenTarget only names existing AO session/reviewer surfaces.
// Clients must not manufacture a session ID for a reviewer-only launch.
type InstructorMemberOpenTarget struct {
	Kind        string    `json:"kind" enum:"session,review"`
	ProjectID   ProjectID `json:"projectId"`
	SessionID   SessionID `json:"sessionId"`
	ReviewID    string    `json:"reviewId,omitempty"`
	ReviewRunID string    `json:"reviewRunId,omitempty"`
}

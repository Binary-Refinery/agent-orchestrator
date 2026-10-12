package session

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// InstructorWorkReader supplies accepted business state, rather than deriving
// successful completion from an agent process stopping.
type InstructorWorkReader interface {
	WorkForSession(context.Context, domain.SessionID) (domain.InstructorWork, bool, error)
}

// SetInstructorWorkReader is installed before automation reconciliation.
func (s *Service) SetInstructorWorkReader(reader InstructorWorkReader) {
	s.instructor = reader
}

// AutomationWorkAccepted distinguishes unmanaged compatibility mode from a
// governed run whose actual work has (or has not) passed closeout.
func (s *Service) AutomationWorkAccepted(ctx context.Context, id domain.SessionID) (managed, accepted bool, err error) {
	if s.instructor == nil {
		return false, false, nil
	}
	work, found, err := s.instructor.WorkForSession(ctx, id)
	if err != nil {
		return true, false, err
	}
	if !found {
		// Enabling the Instructor never retroactively certifies a legacy session.
		return false, false, nil
	}
	return true, work.State == "done", nil
}

package instructor

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type heartbeatStore interface {
	PutInstructorHeartbeat(context.Context, domain.InstructorHeartbeat) error
	GetInstructorHeartbeat(context.Context, string) (domain.InstructorHeartbeat, bool, error)
}

// DecorateHeartbeat projects producer availability without changing work progress.
func (s *Service) DecorateHeartbeat(ctx context.Context, w *domain.InstructorWork) error {
	store, ok := s.deps.Store.(heartbeatStore)
	if !ok {
		return nil
	}
	heartbeat, found, err := store.GetInstructorHeartbeat(ctx, w.ID)
	if err != nil {
		return err
	}
	if found {
		w.Heartbeat = &heartbeat
	}
	return nil
}

func (s *Service) heartbeat(ctx context.Context, w domain.InstructorWork, in ReportInput, role string) (domain.InstructorWork, error) {
	if in.SHA != "" || in.Phase != "" || in.Branch != "" || len(in.Evidence) > 0 || in.Gate != nil || in.OpenCriticalFindings != nil || in.WaitCondition != "" || in.WaitUntil != nil || in.Next != "" {
		return w, invalid("heartbeat reports availability only; use progress/evidence for work changes")
	}
	if len(in.Summary) > 2048 {
		return w, invalid("heartbeat summary is too long")
	}
	store, ok := s.deps.Store.(heartbeatStore)
	if !ok {
		return w, invalid("heartbeat persistence unavailable")
	}
	heartbeat := domain.InstructorHeartbeat{WorkID: w.ID, SessionID: in.SessionID, Role: role, At: s.now(), Summary: in.Summary}
	if err := store.PutInstructorHeartbeat(ctx, heartbeat); err != nil {
		return w, err
	}
	w.Heartbeat = &heartbeat
	return w, nil
}

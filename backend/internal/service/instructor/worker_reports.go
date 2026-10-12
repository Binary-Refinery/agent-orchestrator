package instructor

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// QueueWorkerReport admits a worker completion claim, not business completion.
// The store commits report + phase-correlated wake-up together, so a crash after
// the HTTP response cannot lose the required coordinator turn.
func (s *Service) QueueWorkerReport(ctx context.Context, report domain.ReportRecord) (domain.ReportRecord, bool, error) {
	if report.State != domain.ReportDone {
		return report, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w, managed, err := s.deps.Store.InstructorWorkForSession(ctx, report.SessionID)
	if err != nil || !managed {
		return report, false, err
	}
	if w.ProjectID != report.ProjectID {
		return report, true, invalid("worker report project does not match work")
	}
	writer, ok := s.deps.Store.(interface {
		CreateInstructorWorkerReport(context.Context, domain.ReportRecord, domain.InstructorWork) (domain.ReportRecord, error)
	})
	if !ok {
		return report, true, invalid("atomic managed report outbox is unavailable")
	}
	created, err := writer.CreateInstructorWorkerReport(ctx, report, w)
	return created, true, err
}

// BindDispatchRuntime pins the actual target incarnation before native I/O.
// A reused human-readable session id cannot receive an old directive.
func (s *Service) BindDispatchRuntime(ctx context.Context, id string, target domain.SessionRecord) error {
	writer, ok := s.deps.Store.(interface {
		BindInstructorDispatchRuntime(context.Context, string, domain.SessionRecord) error
	})
	if !ok {
		return fmt.Errorf("%w: native target incarnation persistence is unavailable", ErrValidation)
	}
	return writer.BindInstructorDispatchRuntime(ctx, id, target)
}

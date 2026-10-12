package instructor

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func (s *Service) saveProducer(ctx context.Context, w domain.InstructorWork, expected int64, kind, detail string, a domain.InstructorReportAdmission, receiptState string) (domain.InstructorWork, error) {
	writer, ok := s.deps.Store.(interface {
		SaveInstructorReport(context.Context, domain.InstructorWork, int64, domain.InstructorEvent, domain.InstructorReportAdmission, string) (bool, error)
	})
	if !ok {
		return w, invalid("atomic producer write admission unavailable")
	}
	w.Revision++
	w.UpdatedAt = s.now()
	s.actions(&w)
	saved, err := writer.SaveInstructorReport(ctx, w, expected, s.event(w, kind, detail), a, receiptState)
	if errors.Is(err, domain.ErrInstructorProducerChanged) {
		return w, fmt.Errorf("%w: %w", ErrConflict, err)
	}
	if err != nil {
		return w, err
	}
	if !saved {
		return w, ErrConflict
	}
	return w, nil
}

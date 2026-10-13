package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type quietInstructorLauncher struct {
	fakeLauncher
	quiet bool
}

func (l *quietInstructorLauncher) InstructorReviewerQuiescent(context.Context, domain.Review) (bool, error) {
	return l.quiet, nil
}

func TestInstructorCancellationReviewerIsExactAndInterruptIsNotCompletion(t *testing.T) {
	now := time.Now().UTC()
	original := domain.Review{ID: "review", SessionID: "worker", ProjectID: "p", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-chat:review", ReviewerLaunchID: "launch", ControllerGeneration: "controller", InterfaceMode: domain.ReviewerInterfaceChat, CreatedAt: now}
	st := &fakeStore{review: &original, reviews: map[domain.ReviewerHarness]domain.Review{original.Harness: original}}
	launcher := &quietInstructorLauncher{}
	engine := New(Deps{Store: st, Launcher: launcher})
	if err := engine.InterruptInstructorReviewer(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	if !launcher.cancelled || launcher.cancelledHandle != original.ReviewerHandleID {
		t.Fatal("native captured reviewer was not interrupted")
	}
	if quiet, err := engine.InstructorReviewerQuiescent(context.Background(), original); err != nil || quiet {
		t.Fatalf("interrupt admission was terminal proof: %v %v", quiet, err)
	}
	launcher.quiet = true
	if quiet, err := engine.InstructorReviewerQuiescent(context.Background(), original); err != nil || !quiet {
		t.Fatalf("provider ready proof lost: %v %v", quiet, err)
	}
	replaced := original
	replaced.ControllerGeneration = "replacement-controller"
	st.reviews[original.Harness] = replaced
	launcher.cancelled = false
	if err := engine.InterruptInstructorReviewer(context.Background(), original); !errors.Is(err, domain.ErrInstructorProducerChanged) || launcher.cancelled {
		t.Fatalf("cancel hit replacement: %v %v", launcher.cancelled, err)
	}
	if quiet, err := engine.InstructorReviewerQuiescent(context.Background(), original); quiet || !errors.Is(err, domain.ErrInstructorProducerChanged) {
		t.Fatalf("replacement inherited proof: %v %v", quiet, err)
	}
}

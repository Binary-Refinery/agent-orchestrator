package instructor_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorParentDelegatesWorkersThroughOrchestrator(t *testing.T) {
	for _, parentRole := range []string{"instructor", "orchestrator"} {
		for _, childRole := range []string{"worker", "reviewer", "triage", "orchestrator"} {
			t.Run(parentRole+"/"+childRole, func(t *testing.T) {
				f := newInstructorFixture(t)
				work := f.register(t, "control-parent", adoptionPolicy())
				parent := f.attachWorker(t, work.ID)
				f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
					w.Launches[0].Role = parentRole
				})
				_, err := f.service.ResolveSpawnWork(context.Background(), parent, work.ID, childRole)
				if parentRole == "instructor" && childRole != "orchestrator" {
					if !errors.Is(err, instructorsvc.ErrValidation) || !strings.Contains(err.Error(), "through a project orchestrator") {
						t.Fatalf("instructor directly delegated %s: %v", childRole, err)
					}
				} else if err != nil {
					t.Fatalf("allowed delegation refused: %v", err)
				}
			})
		}
	}
}

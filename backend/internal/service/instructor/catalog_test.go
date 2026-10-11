package instructor_test

import (
	"context"
	"strings"
	"testing"

	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorNamesResolveOnlyWithinFrozenScope(t *testing.T) {
	f := newInstructorFixture(t)
	first := f.register(t, "Layout One", instructorsvc.DefaultPolicy())
	f.register(t, "Layout Two", instructorsvc.DefaultPolicy())
	snapshot := f.snapshot(t)
	_, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Weiter Layout", SnapshotID: snapshot.ID, IdempotencyKey: "ambiguous"})
	if err == nil || !strings.Contains(err.Error(), "Layout One") || !strings.Contains(err.Error(), "Layout Two") {
		t.Fatalf("missing ambiguous choices: %v", err)
	}
	result, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Weiter Layout O", SnapshotID: snapshot.ID, IdempotencyKey: "unique"})
	if err != nil || len(result.Dispatches) != 1 || result.Dispatches[0].WorkID != first.ID {
		t.Fatalf("unique prefix result=%+v err=%v", result, err)
	}
}

func TestInstructorReadCommandsAndRegistryDescribeActualEffects(t *testing.T) {
	f := newInstructorFixture(t)
	work := f.register(t, "Read Work", instructorsvc.DefaultPolicy())
	result, err := f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Details Read"})
	if err != nil || len(result.Details) != 1 || result.Details[0].Work.ID != work.ID {
		t.Fatalf("details=%+v err=%v", result, err)
	}
	if _, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Status Delta"}); err == nil {
		t.Fatal("delta allowed without base snapshot")
	}
	if _, err = f.service.Command(context.Background(), instructorsvc.CommandInput{Command: "Unknown"}); err == nil || !strings.Contains(err.Error(), "choose: Status") {
		t.Fatalf("unknown choices: %v", err)
	}
	for _, command := range f.service.Commands() {
		if len(command.Scopes) == 0 || command.Authority == "" || command.Parameters == nil {
			t.Fatalf("untyped command: %+v", command)
		}
		if command.Mutates && command.Authority != "operator" {
			t.Fatalf("mutation authority: %+v", command)
		}
	}
}

func TestInstructorDeltaKeepsPortfolioMetrics(t *testing.T) {
	f := newInstructorFixture(t)
	f.register(t, "Stable Work", instructorsvc.DefaultPolicy())
	snapshot := f.snapshot(t)
	delta, err := f.service.Snapshot(context.Background(), snapshot.ID)
	if err != nil || len(delta.Works) != 0 || delta.Metrics.Active != 1 {
		t.Fatalf("delta metrics=%+v err=%v", delta, err)
	}
}

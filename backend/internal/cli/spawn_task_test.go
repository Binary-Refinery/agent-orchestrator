package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestSpawnExplicitGovernedWorkerRequiresTaskBeforeDaemonLookup(t *testing.T) {
	for _, args := range [][]string{
		{"--work", "work-1"},
		{"--work-role", "reviewer", "--prompt", " \t\n"},
	} {
		_, _, err := executeCLI(t, Deps{}, append([]string{"spawn", "--name", "worker"}, args...)...)
		var usage usageError
		if !errors.As(err, &usage) || !strings.Contains(err.Error(), "--prompt or --issue is required") {
			t.Fatalf("missing governed task reached daemon lookup: %v", err)
		}
	}
}

func TestSpawnHelpSeparatesSubmissionFromAcceptance(t *testing.T) {
	out, _, err := executeCLI(t, Deps{}, "spawn", "--help")
	if err != nil || !strings.Contains(out, "does not confirm that the agent has accepted or begun work") {
		t.Fatalf("help overstates successful spawn: %v\n%s", err, out)
	}
}

package instructor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Command metadata is derived from implemented operations. Repository aliases
// cannot invent privileges, relax snapshots or advertise unsupported effects.
func commandContract(c domain.InstructorCommandSpec) domain.InstructorCommandSpec {
	c.Parameters = []domain.InstructorCommandParameter{}
	c.Preconditions = []string{}
	c.Scopes = []string{"portfolio"}
	c.Authority = "reader"
	if c.Action == "details" {
		c.Scopes = []string{"work"}
		c.Parameters = append(c.Parameters, domain.InstructorCommandParameter{Name: "work", Type: "work-reference", Required: true})
	}
	if c.Action == "delta" {
		c.SnapshotRequired = true
		c.Parameters = append(c.Parameters, domain.InstructorCommandParameter{Name: "snapshotId", Type: "snapshot-reference", Required: true})
	}
	if c.Mutates {
		c.Authority = "operator"
		c.Scopes = []string{"work", "snapshot-cohort"}
		c.Parameters = append(c.Parameters, domain.InstructorCommandParameter{Name: "work", Type: "work-reference"})
		c.Preconditions = []string{"current-work-revision", "action-eligible"}
		if c.SnapshotRequired {
			c.Parameters = append(c.Parameters, domain.InstructorCommandParameter{Name: "snapshotId", Type: "snapshot-reference", Required: true}, domain.InstructorCommandParameter{Name: "idempotencyKey", Type: "string", Required: true})
		}
		if c.Action == "go" {
			c.Preconditions = append(c.Preconditions, "open-gate", "exact-publication-approval-if-required")
			c.Parameters = append(c.Parameters, domain.InstructorCommandParameter{Name: "approval", Type: "exact-approval"})
			c.Parameters = append(c.Parameters, domain.InstructorCommandParameter{Name: "approvals", Type: "work-approvals"})
		}
		if c.Action == "neu" {
			c.Scopes = []string{"project"}
			c.Parameters = []domain.InstructorCommandParameter{{Name: "new", Type: "work-registration", Required: true}}
			c.Preconditions = []string{"valid-project", "valid-governance-policy"}
		}
	}
	return c
}

func commandSpellings(c domain.InstructorCommandSpec) []string {
	return append([]string{c.Name}, c.Aliases...)
}

func validatePolicyCommands(commands []domain.InstructorCommandSpec) error {
	claimed := map[string]string{}
	for _, c := range (&Service{}).Commands() {
		for _, spelling := range commandSpellings(c) {
			claimed[strings.ToLower(strings.TrimSpace(spelling))] = c.Action
		}
	}
	for _, c := range commands {
		for _, spelling := range commandSpellings(c) {
			key := strings.ToLower(strings.TrimSpace(spelling))
			if key == "" {
				return invalid("command spelling cannot be empty")
			}
			if action, exists := claimed[key]; exists && action != c.Action {
				return invalid("command spelling has conflicting effects: " + spelling)
			}
			claimed[key] = c.Action
		}
	}
	return nil
}

// Conflicting repository spellings are never resolved by iteration order. A
// selected work can use its own registry; a mixed cohort must disambiguate.
func (s *Service) registryForWorks(works []domain.InstructorWork) ([]domain.InstructorCommandSpec, []string) {
	base := s.Commands()
	candidates := append([]domain.InstructorCommandSpec{}, base...)
	for _, w := range works {
		for _, c := range w.Policy.Commands {
			candidates = append(candidates, commandContract(c))
		}
	}
	claimed, conflicts := map[string]string{}, map[string]bool{}
	for _, c := range candidates {
		for _, spelling := range commandSpellings(c) {
			key := strings.ToLower(strings.TrimSpace(spelling))
			if action, exists := claimed[key]; exists && action != c.Action {
				conflicts[key] = true
			}
			claimed[key] = c.Action
		}
	}
	result := append([]domain.InstructorCommandSpec{}, base...)
	seen := map[string]int{}
	for index, c := range result {
		seen[strings.ToLower(c.Name)] = index
	}
	for _, c := range candidates[len(base):] {
		conflicted := false
		for _, spelling := range commandSpellings(c) {
			conflicted = conflicted || conflicts[strings.ToLower(strings.TrimSpace(spelling))]
		}
		if conflicted {
			continue
		}
		key := strings.ToLower(c.Name)
		if index, exists := seen[key]; exists {
			result[index].Aliases = append(result[index].Aliases, c.Aliases...)
			continue
		}
		seen[key] = len(result)
		result = append(result, c)
	}
	warnings := make([]string, 0, len(conflicts))
	for spelling := range conflicts {
		warnings = append(warnings, spelling)
	}
	sort.Strings(warnings)
	return result, warnings
}

func conflictingCommand(line string, conflicts []string) bool {
	line = strings.ToLower(strings.TrimSpace(line))
	for _, spelling := range conflicts {
		if line == spelling || strings.HasPrefix(line, spelling+" ") {
			return true
		}
	}
	return false
}

func resolveCommandLine(line string, registry []domain.InstructorCommandSpec) (domain.InstructorCommandSpec, string, error) {
	if c, ok := resolveCommand(line, registry); ok {
		return commandContract(c), "", nil
	}
	var best domain.InstructorCommandSpec
	selector, length := "", 0
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, c := range registry {
		for _, name := range append([]string{c.Name}, c.Aliases...) {
			name = strings.ToLower(strings.TrimSpace(name))
			if len(name) > length && strings.HasPrefix(lower, name+" ") {
				best, length = c, len(name)
				selector = strings.TrimSpace(strings.TrimSpace(line)[length:])
			}
		}
	}
	if length > 0 {
		selector = strings.TrimSpace(strings.Trim(selector, "[]"))
		if selector == "" {
			return best, "", invalid("explicit work selector cannot be empty")
		}
		return commandContract(best), selector, nil
	}
	names := make([]string, 0, len(registry))
	for _, c := range registry {
		names = append(names, c.Name)
	}
	return best, "", invalid("unknown command; choose: " + strings.Join(names, ", "))
}

func resolveWorkReferences(refs []string, works []domain.InstructorWork) ([]string, error) {
	ids, seen := []string{}, map[string]bool{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		matches := []domain.InstructorWork{}
		for _, w := range works {
			if w.ID == ref {
				matches = []domain.InstructorWork{w}
				break
			}
		}
		if len(matches) == 0 {
			for _, w := range works {
				if strings.EqualFold(w.Name, ref) {
					matches = append(matches, w)
				}
			}
		}
		if len(matches) == 0 && ref != "" {
			for _, w := range works {
				if strings.HasPrefix(strings.ToLower(w.Name), strings.ToLower(ref)) {
					matches = append(matches, w)
				}
			}
		}
		if len(matches) != 1 {
			choices := make([]string, 0, len(matches))
			if len(matches) == 0 {
				matches = works
			}
			for _, w := range matches {
				choices = append(choices, fmt.Sprintf("%s (%s)", w.Name, w.ID))
			}
			sort.Strings(choices)
			return nil, invalid("unknown or ambiguous work " + ref + "; choose: " + strings.Join(choices, ", "))
		}
		if !seen[matches[0].ID] {
			ids = append(ids, matches[0].ID)
			seen[matches[0].ID] = true
		}
	}
	return ids, nil
}

func (s *Service) snapshotMetrics(ctx context.Context, works []domain.InstructorWork) (domain.InstructorMetrics, error) {
	m := domain.InstructorMetrics{}
	now := s.now()
	for _, w := range works {
		switch w.State {
		case "done":
			if !w.UpdatedAt.Before(now.Add(-7 * 24 * time.Hour)) {
				m.ClosedLast7Days++
			}
		case "cancelled":
		case "waiting_human":
			m.Gated++
		case "parked":
			m.Parked++
		case "blocked":
			m.Stalled++
		default:
			m.Active++
		}
	}
	if reader, ok := s.deps.Store.(interface {
		InstructorEventCountsSince(context.Context, time.Time) (map[string]int, error)
	}); ok {
		counts, err := reader.InstructorEventCountsSince(ctx, now.Add(-24*time.Hour))
		if err != nil {
			return m, err
		}
		m.GatesClearedLast24Hours = counts["human_approved"]
		m.RecordedViolations = counts["policy_rejected"]
		m.VerificationFailures = counts["verification_failed"] + counts["check_failed"]
	}
	return m, nil
}

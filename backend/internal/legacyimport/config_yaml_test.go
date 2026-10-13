package legacyimport

import (
	"os"
	"strings"
	"testing"
)

func TestLoadLegacyConfigPreservesOrchestratorAliasesAndMerges(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
	}{
		{"scalar aliases", `rules: &rules Coordinate with workers.
file: &file rules.md
task: &task kickoff
projects:
  demo:
    path: /repo
    orchestratorRules: *rules
    orchestratorRulesFile: *file
    orchestratorPrompt: *task
`},
		{"merged defaults", `defaults: &defaults
  orchestratorRules: Coordinate with workers.
  orchestratorRulesFile: rules.md
  orchestratorPrompt: kickoff
projects:
  demo:
    <<: *defaults
    path: /repo
`},
		{"nested merges and explicit override", `base: &base
  path: /wrong
  orchestratorRules: Coordinate with workers.
  orchestratorRulesFile: stale.md
  orchestratorPrompt: stale kickoff
defaults: &defaults
  <<: *base
  orchestratorRulesFile: rules.md
projects:
  demo:
    <<: *defaults
    path: /repo
    orchestratorPrompt: kickoff
`},
		{"sequence first wins and explicit override", `first: &first
  orchestratorRules: Coordinate with workers.
  orchestratorRulesFile: rules.md
  orchestratorPrompt: stale kickoff
second: &second
  orchestratorRules: Wrong merged rule.
  orchestratorRulesFile: wrong.md
  orchestratorPrompt: wrong kickoff
  path: /repo
projects:
  demo:
    <<: [*first, *second]
    orchestratorPrompt: kickoff
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(globalConfigPath(root), []byte(tc.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			legacy, err := loadLegacyConfig(root)
			if err != nil {
				t.Fatal(err)
			}
			project := legacy.Projects["demo"]
			var notes []string
			cfg := buildProjectConfig(project, &notes)
			if project.Path != "/repo" || cfg.OrchestratorRules != "Coordinate with workers." || cfg.OrchestratorRulesFile != "rules.md" || cfg.OrchestratorPrompt != "kickoff" || len(notes) != 0 {
				t.Fatalf("loaded project path=%q config=%#v notes=%v", project.Path, cfg, notes)
			}
		})
	}
}

func TestLoadLegacyConfigReportsInvalidMergedPromptValues(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(globalConfigPath(root), []byte(`defaults: &defaults
  orchestratorRulesFile: [not, a, string]
  orchestratorPrompt: false
projects:
  demo:
    <<: *defaults
    path: /repo
`), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy, err := loadLegacyConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	var notes []string
	cfg := buildProjectConfig(legacy.Projects["demo"], &notes)
	joined := strings.Join(notes, "\n")
	if cfg.OrchestratorRulesFile != "" || cfg.OrchestratorPrompt != "" || !strings.Contains(joined, "rules") || !strings.Contains(joined, "orchestratorPrompt") {
		t.Fatalf("invalid merged values silently lost: config=%#v notes=%v", cfg, notes)
	}
}

func TestLoadLegacyConfigSurfacesMalformedMergeAndDuplicateRules(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, want string
	}{
		{"invalid merge", "projects:\n  demo:\n    path: /repo\n    <<: 42\n", "merge"},
		{"duplicate inherited raw field", "defaults: &defaults\n  orchestratorRulesFile: first.md\n  orchestratorRulesFile: second.md\nprojects:\n  demo:\n    path: /repo\n    <<: *defaults\n", "orchestratorRulesFile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(globalConfigPath(root), []byte(tc.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := loadLegacyConfig(root)
			if err == nil || !strings.Contains(err.Error(), "parse legacy config.yaml") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("structural error = %v, want visible %s error", err, tc.want)
			}
		})
	}
}

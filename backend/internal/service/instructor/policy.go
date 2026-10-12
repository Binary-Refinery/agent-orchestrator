package instructor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// DefaultPolicy returns the locked software workflow with explicit unconfigured choices.
func DefaultPolicy() domain.InstructorPolicy {
	p := domain.InstructorPolicy{Schema: 1, InitialPhase: "intake", Roles: map[string]domain.InstructorRole{
		"worker": {Harness: "opencode", Effort: "maximum"}, "reviewer": {Harness: "codex", Effort: "maximum", Independent: true}, "orchestrator": {Harness: "codex", Effort: "configured"}, "instructor": {Harness: "codex", Effort: "configured"},
	}, Profiles: map[string]domain.InstructorProfile{"push": {TimeoutSeconds: 900}, "full": {TimeoutSeconds: 900}}, Transitions: []domain.InstructorTransition{
		{From: "intake", To: "implement", Role: "worker"}, {From: "implement", To: "review", Role: "reviewer", Requires: []string{"push", "full"}},
		{From: "review", To: "triage", Role: "orchestrator", Requires: []string{"review-report"}},
		{From: "triage", To: "fix", Role: "worker", Requires: []string{"triage"}, HumanGate: true},
		{From: "fix", To: "delta", Role: "reviewer", Requires: []string{"push", "full"}},
		{From: "delta", To: "closeout", Role: "orchestrator", Requires: []string{"delta"}},
		{From: "delta", To: "triage", Role: "orchestrator", Requires: []string{"delta-changes"}},
		{From: "closeout", To: "triage", Role: "orchestrator", Requires: []string{"ci-failed"}},
	}, CloseoutRequires: []string{"task-packet", "push", "full", "ci", "review-report", "triage", "delta", "attribution", "traceability"}, MaxFixRounds: 2, NudgeAfterSeconds: 300, EscalateAfterSeconds: 900,
		Attribution:  domain.InstructorAttribution{CoAuthorName: "AO Worker", SessionTrailer: "AO-session", IssueTrailer: "Issue"},
		Traceability: domain.InstructorTraceability{Required: true, RequireAssignee: true, RequiredPhases: []string{"start", "commit", "push", "pr", "ci", "review", "fix", "delta", "closeout"}, AllowedBaseBranches: []string{"main", "staging"}},
	}
	p.Version = PolicyVersion(p)
	return p
}

// PolicyVersion hashes the complete canonical policy excluding its version field.
func PolicyVersion(p domain.InstructorPolicy) string {
	p.Version = ""
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ValidatePolicy checks the executable schema and bounded role/profile contracts.
func ValidatePolicy(p domain.InstructorPolicy) error {
	if p.Schema != 1 {
		return invalid("unsupported instructor policy schema")
	}
	if p.InitialPhase == "" || len(p.Roles) == 0 || p.MaxFixRounds < 0 || p.MaxFixRounds > 10 {
		return invalid("policy needs an initial phase, roles and maxFixRounds between 0 and 10")
	}
	if p.NudgeAfterSeconds < 1 || p.EscalateAfterSeconds <= p.NudgeAfterSeconds {
		return invalid("policy escalation must follow a positive nudge window")
	}
	for name, r := range p.Roles {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(r.Harness) == "" {
			return invalid("every role needs a name and harness")
		}
		if r.Effort == "" {
			return invalid(fmt.Sprintf("role %s needs explicit effort", name))
		}
		for model, effort := range r.EffortByModel {
			if model == "" || effort == "" {
				return invalid("effortByModel needs native model and effort keys")
			}
		}
	}
	for name, pf := range p.Profiles {
		if name == "" || pf.TimeoutSeconds < 1 || pf.TimeoutSeconds > 900 {
			return invalid("profile timeout must be between 1 and 900 seconds")
		}
		if reservedEvidenceKind(name) {
			return invalid("check profile cannot replace business evidence: " + name)
		}
		if len(pf.Command) > 0 && strings.TrimSpace(pf.Command[0]) == "" {
			return invalid("configured check profile needs a nonempty executable")
		}
		for _, arg := range pf.Command {
			if strings.ContainsRune(arg, 0) {
				return invalid("profile command contains NUL")
			}
		}
	}
	for _, t := range p.Transitions {
		if t.From == "" || t.To == "" || t.From == t.To || t.Role == "" {
			return invalid("transition requires distinct phases and a role")
		}
		if _, ok := p.Roles[t.Role]; !ok {
			return invalid(fmt.Sprintf("transition references undeclared role %s", t.Role))
		}
	}
	for _, repo := range p.Traceability.AllowedRepositories {
		parts := strings.Split(repo, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(repo, "*?\\ :\t\r\n") {
			return invalid("traceability destinations must be explicit owner/repository names")
		}
	}
	for _, branch := range p.Traceability.AllowedBaseBranches {
		if strings.TrimSpace(branch) == "" || strings.ContainsAny(branch, "*? :\t\r\n") {
			return invalid("traceability base branches must be explicit nonempty names")
		}
	}
	for phase, label := range p.Traceability.PhaseLabels {
		if strings.TrimSpace(phase) == "" || strings.TrimSpace(label) == "" {
			return invalid("traceability phase label names cannot be empty")
		}
	}
	if p.Traceability.ProjectNumber < 0 || strings.ContainsAny(p.Traceability.ProjectOwner, "*?/\\ :\t\r\n") {
		return invalid("traceability project owner/number is invalid")
	}
	for phase, status := range p.Traceability.BoardStatuses {
		if strings.TrimSpace(phase) == "" || strings.TrimSpace(status) == "" {
			return invalid("traceability board status names cannot be empty")
		}
	}
	for _, phase := range p.Traceability.RequiredPhases {
		if strings.TrimSpace(phase) == "" {
			return invalid("traceability required phase cannot be empty")
		}
	}
	names := map[string]bool{}
	if err := validatePolicyCommands(p.Commands); err != nil {
		return err
	}
	for _, c := range p.Commands {
		if c.Name == "" || names[strings.ToLower(c.Name)] {
			return invalid("command name missing or duplicated")
		}
		names[strings.ToLower(c.Name)] = true
		switch c.Action {
		case "status", "delta", "tore", "details":
			if c.Mutates {
				return invalid("read command cannot mutate")
			}
		case "weiter", "go", "pruefen", "stopp":
			if !c.Mutates || !c.SnapshotRequired {
				return invalid("mutation command must require snapshot")
			}
		default:
			return invalid("command can only compose supported instructor actions")
		}
	}
	return nil
}

// ValidateLockedPolicy prevents repository or GUI overrides from weakening global constraints.
func ValidateLockedPolicy(p domain.InstructorPolicy) error {
	if p.ExceptionVersion != "" || p.MaxFixRounds > 2 {
		return invalid("global/repository policy cannot contain work exceptions or a fix budget above 2")
	}
	for name, harness := range map[string]string{"worker": "opencode", "reviewer": "codex"} {
		r, ok := p.Roles[name]
		if !ok || r.Harness != harness || r.Effort != "maximum" {
			return invalid(fmt.Sprintf("locked %s role requires %s and explicitly mapped maximum native effort", name, harness))
		}
	}
	if !p.Roles["reviewer"].Independent {
		return invalid("reviewer must remain independent")
	}
	for _, required := range DefaultPolicy().CloseoutRequires {
		found := false
		for _, r := range p.CloseoutRequires {
			if r == required {
				found = true
			}
		}
		if !found {
			return invalid(fmt.Sprintf("locked closeout evidence %s is required", required))
		}
	}
	if !p.Traceability.Required || !p.Traceability.RequireAssignee {
		return invalid("locked traceability requires issue/PR linkage and an assigned operator")
	}
	for _, phase := range DefaultPolicy().Traceability.RequiredPhases {
		found := false
		for _, declared := range p.Traceability.RequiredPhases {
			if declared == phase {
				found = true
			}
		}
		if !found {
			return invalid("locked traceability phase missing: " + phase)
		}
	}
	if len(p.Traceability.AllowedBaseBranches) == 0 {
		return invalid("traceability needs explicit allowed base branches")
	}
	return nil
}

// LoadPolicy accepts JSON or exactly one fenced ao-policy JSON block in Markdown.
// Resolving symlinks prevents repo-relative policy paths escaping the repository.
func LoadPolicy(projectPath, relativePath string) (domain.InstructorPolicy, error) {
	root, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return domain.InstructorPolicy{}, err
	}
	if filepath.IsAbs(relativePath) {
		return domain.InstructorPolicy{}, invalid("policy path must be repository relative")
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, relativePath))
	if err != nil {
		return domain.InstructorPolicy{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return domain.InstructorPolicy{}, invalid("policy path escapes repository")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.InstructorPolicy{}, err
	}
	if len(data) > 256*1024 {
		return domain.InstructorPolicy{}, invalid("policy file too large")
	}
	return ParsePolicy(data)
}

// ParsePolicy reads strict JSON or one ao-policy Markdown block and pins its content hash.
func ParsePolicy(data []byte) (domain.InstructorPolicy, error) {
	text := strings.TrimSpace(string(data))
	if !strings.HasPrefix(text, "{") {
		parts := strings.Split(text, "```ao-policy")
		if len(parts) != 2 {
			return domain.InstructorPolicy{}, invalid("Markdown must contain exactly one ao-policy block")
		}
		end := strings.Index(parts[1], "```")
		if end < 0 {
			return domain.InstructorPolicy{}, invalid("unclosed ao-policy block")
		}
		text = strings.TrimSpace(parts[1][:end])
	}
	decoder := json.NewDecoder(bytes.NewBufferString(text))
	decoder.DisallowUnknownFields()
	var p domain.InstructorPolicy
	if err := decoder.Decode(&p); err != nil {
		return p, invalid("invalid policy JSON: " + err.Error())
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return p, invalid("policy must contain exactly one JSON document")
	}
	if err := ValidatePolicy(p); err != nil {
		return p, err
	}
	if err := ValidateLockedPolicy(p); err != nil {
		return p, err
	}
	p.Version = PolicyVersion(p)
	return p, nil
}

// GetPolicy loads the persisted global policy or the configured portable policy file.
func (s *Service) GetPolicy(ctx context.Context) (domain.InstructorPolicy, error) {
	if store, ok := s.deps.Store.(interface {
		GetInstructorPolicy(context.Context) (domain.InstructorPolicy, bool, error)
	}); ok {
		p, found, e := store.GetInstructorPolicy(ctx)
		if e != nil {
			return p, e
		}
		if found {
			return p, nil
		}
	}
	if s.deps.PolicyPath != "" {
		data, err := os.ReadFile(s.deps.PolicyPath)
		if err != nil {
			return domain.InstructorPolicy{}, err
		}
		return ParsePolicy(data)
	}
	return s.policy, nil
}

// SetPolicy validates a global replacement without altering policies pinned to existing work.
func (s *Service) SetPolicy(ctx context.Context, p domain.InstructorPolicy) (domain.InstructorPolicy, error) {
	current, err := s.GetPolicy(ctx)
	if err != nil {
		return p, err
	}
	if p.Version != "" && p.Version != current.Version {
		return p, ErrConflict
	}
	if err := ValidatePolicy(p); err != nil {
		return p, err
	}
	if err := ValidateLockedPolicy(p); err != nil {
		return p, err
	}
	p.Version = PolicyVersion(p)
	store, ok := s.deps.Store.(interface {
		PutInstructorPolicy(context.Context, domain.InstructorPolicy) error
	})
	if !ok {
		return p, invalid("policy storage unavailable")
	}
	return p, store.PutInstructorPolicy(ctx, p)
}

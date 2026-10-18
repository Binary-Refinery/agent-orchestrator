package backbone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Binding is adopted through the existing Instructor policy digest, not from an
// automatically discovered candidate file. Qualification is operator evidence;
// the admission service validates its exact runtime/model/interface contract.
type Binding struct {
	SchemaVersion string                 `json:"schemaVersion"`
	AdapterSHA256 string                 `json:"adapterSha256"`
	Contexts      map[string]RoleBinding `json:"contexts"`
}

// RoleBinding pins a mapped role, explicit packet and complete producer receipt.
type RoleBinding struct {
	Role          string          `json:"role"`
	TaskPacket    string          `json:"taskPacket"`
	Context       json.RawMessage `json:"context"`
	Qualification json.RawMessage `json:"qualification,omitempty"`
}

// VersionedID identifies a versioned producer catalog entry.
type VersionedID struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// CheckBinding links a catalog rule to declared check IDs, without executing it.
type CheckBinding struct {
	RuleID   string   `json:"rule_id"`
	CheckIDs []string `json:"check_ids"`
}

// Check is a data-only check declaration copied from the producer receipt.
type Check struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Argv           []string `json:"argv"`
	Cwd            string   `json:"cwd"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Phases         []string `json:"phases"`
}

// Document hashes the entire file, including when Path names a heading anchor.
type Document struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ContextReceipt is the complete resolver output. Its structural-context-only
// claim deliberately does not assert native loading or model qualification.
type ContextReceipt struct {
	SchemaVersion            string                    `json:"schema_version"`
	Claim                    string                    `json:"claim"`
	Revision                 string                    `json:"revision"`
	TaskID                   string                    `json:"task_id"`
	Role                     string                    `json:"role"`
	RequestSHA256            string                    `json:"request_sha256"`
	NavigationSHA256         string                    `json:"navigation_sha256"`
	QualityProfileSHA256     string                    `json:"quality_profile_sha256"`
	BackboneRef              string                    `json:"backbone_ref"`
	QualityCatalogVersion    string                    `json:"quality_catalog_version"`
	TechnologyCatalogVersion string                    `json:"technology_catalog_version"`
	TechnologyCatalogSHA256  string                    `json:"technology_catalog_sha256"`
	Components               []string                  `json:"components"`
	TechnologyProfiles       []VersionedID             `json:"technology_profiles"`
	TechnologyRules          []VersionedID             `json:"technology_rules"`
	TechnologyCheckBindings  map[string][]CheckBinding `json:"technology_check_bindings"`
	QualityProfiles          []VersionedID             `json:"quality_profiles"`
	QualityRules             []VersionedID             `json:"quality_rules"`
	QualityCheckBindings     []CheckBinding            `json:"quality_check_bindings"`
	Checks                   []Check                   `json:"checks"`
	Documents                []Document                `json:"documents"`
	RuntimeLoading           string                    `json:"runtime_loading"`
	ContextSHA256            string                    `json:"context_sha256"`
}

// VerifiedContext carries verified bytes for the actual standing prompt.
type VerifiedContext struct {
	Context    ContextReceipt
	TaskPacket string
	Documents  map[string][]byte
	Prompt     string
	// Values distinguish receipt hashes from raw blobs at the approved commit.
	NativeDocumentEvidence map[string]string
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var idPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

func hash(data []byte) string { digest := sha256.Sum256(data); return hex.EncodeToString(digest[:]) }

func knownRole(role string) bool {
	switch role {
	case "planner", "implementer", "reviewer", "quality_checker", "triage":
		return true
	}
	return false
}

// ValidateBinding validates the full binding. Non-nil roles restrict its keys to
// the enclosing policy; nil validates intrinsic shape before role selection.
func ValidateBinding(raw json.RawMessage, roles []string) (*Binding, error) {
	var binding Binding
	if err := DecodeStrict(raw, &binding); err != nil {
		return nil, err
	}
	if binding.SchemaVersion != "1" || !sha256Pattern.MatchString(binding.AdapterSHA256) || len(binding.Contexts) == 0 || len(binding.Contexts) > 32 {
		return nil, fmt.Errorf("invalid Backbone binding version, adapter digest or contexts")
	}
	allowed := map[string]bool{}
	for _, role := range roles {
		allowed[role] = true
	}
	for role, bound := range binding.Contexts {
		if roles != nil && !allowed[role] || validText(role, 128) != nil || !knownRole(bound.Role) {
			return nil, fmt.Errorf("unknown Backbone role binding %q", role)
		}
		if _, _, err := relative(bound.TaskPacket, false); err != nil {
			return nil, err
		}
		if _, err := parseContext(bound.Context, bound.Role); err != nil {
			return nil, fmt.Errorf("backbone context %q: %w", role, err)
		}
		if len(bound.Qualification) > 0 {
			if _, err := parseJSON(bound.Qualification); err != nil {
				return nil, err
			}
		}
	}
	return &binding, nil
}

func parseContext(raw []byte, role string) (*ContextReceipt, error) {
	var receipt ContextReceipt
	if err := DecodeStrict(raw, &receipt); err != nil {
		return nil, err
	}
	if receipt.SchemaVersion != "1" || receipt.Claim != "structural-context-only" || receipt.RuntimeLoading != "unverified" || receipt.Role != role || !knownRole(role) || !revisionPattern.MatchString(receipt.Revision) {
		return nil, fmt.Errorf("invalid Backbone context identity or claim")
	}
	for _, digest := range []string{receipt.RequestSHA256, receipt.NavigationSHA256, receipt.QualityProfileSHA256, receipt.TechnologyCatalogSHA256, receipt.ContextSHA256} {
		if !sha256Pattern.MatchString(digest) {
			return nil, fmt.Errorf("invalid Backbone context digest")
		}
	}
	for _, value := range []string{receipt.TaskID, receipt.BackboneRef, receipt.QualityCatalogVersion, receipt.TechnologyCatalogVersion} {
		if err := validText(value, 4096); err != nil {
			return nil, err
		}
	}
	if err := sequence(receipt.Components, true, 256); err != nil {
		return nil, err
	}
	for _, entries := range [][]VersionedID{receipt.TechnologyProfiles, receipt.TechnologyRules, receipt.QualityProfiles, receipt.QualityRules} {
		if len(entries) > 1024 {
			return nil, fmt.Errorf("too many Backbone profile/rule entries")
		}
		seen := map[string]bool{}
		for _, entry := range entries {
			if !idPattern.MatchString(entry.ID) || seen[entry.ID] {
				return nil, fmt.Errorf("invalid or duplicate Backbone profile/rule ID")
			}
			seen[entry.ID] = true
			if err := validText(entry.Version, 128); err != nil {
				return nil, err
			}
		}
	}
	if len(receipt.Documents) == 0 || len(receipt.Documents) > MaxDocuments {
		return nil, fmt.Errorf("invalid Backbone document count")
	}
	seen := map[string]bool{}
	for _, document := range receipt.Documents {
		if _, _, err := relative(document.Path, true); err != nil {
			return nil, err
		}
		if !sha256Pattern.MatchString(document.SHA256) || seen[document.Path] {
			return nil, fmt.Errorf("invalid or duplicate Backbone document")
		}
		seen[document.Path] = true
	}
	if len(receipt.Checks) > 256 {
		return nil, fmt.Errorf("too many Backbone checks")
	}
	checkIDs := map[string]bool{}
	for _, check := range receipt.Checks {
		if !idPattern.MatchString(check.ID) || checkIDs[check.ID] || check.TimeoutSeconds < 1 || check.TimeoutSeconds > 1800 {
			return nil, fmt.Errorf("invalid or duplicate Backbone check")
		}
		checkIDs[check.ID] = true
		if !oneOf(check.Kind, "docs", "build", "lint", "test", "typecheck", "browser", "api", "migration", "sast", "dependency", "container", "iac", "release-integrity", "provenance", "signature", "sbom", "deployment-smoke", "rollback", "monitoring") {
			return nil, fmt.Errorf("invalid Backbone check kind")
		}
		if check.Cwd != "." {
			if _, _, err := relative(check.Cwd, false); err != nil {
				return nil, err
			}
		}
		if len(check.Argv) == 0 || len(check.Argv) > 64 || strings.TrimSpace(check.Argv[0]) != check.Argv[0] || check.Argv[0] == "" {
			return nil, fmt.Errorf("invalid Backbone check argv")
		}
		for _, arg := range check.Argv {
			if len(arg) > 4096 || hasControl(arg) {
				return nil, fmt.Errorf("invalid Backbone check argument")
			}
		}
		if err := sequence(check.Phases, true, 4); err != nil {
			return nil, err
		}
		for _, phase := range check.Phases {
			if !oneOf(phase, "pr", "maintenance", "release", "deploy") {
				return nil, fmt.Errorf("invalid Backbone check phase")
			}
		}
	}
	bindings := [][]CheckBinding{receipt.QualityCheckBindings}
	if len(receipt.TechnologyCheckBindings) > 256 {
		return nil, fmt.Errorf("too many Backbone technology bindings")
	}
	for component, items := range receipt.TechnologyCheckBindings {
		if !idPattern.MatchString(component) || !oneOf(component, receipt.Components...) {
			return nil, fmt.Errorf("unknown Backbone component binding")
		}
		bindings = append(bindings, items)
	}
	for _, items := range bindings {
		if len(items) > 1024 {
			return nil, fmt.Errorf("too many Backbone rule bindings")
		}
		rules := map[string]bool{}
		for _, item := range items {
			if !idPattern.MatchString(item.RuleID) || rules[item.RuleID] {
				return nil, fmt.Errorf("invalid Backbone rule binding")
			}
			rules[item.RuleID] = true
			if err := sequence(item.CheckIDs, true, 256); err != nil {
				return nil, err
			}
			for _, id := range item.CheckIDs {
				if !checkIDs[id] {
					return nil, fmt.Errorf("backbone binding references absent check")
				}
			}
		}
	}
	value, err := parseJSON(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid backbone context object")
	}
	delete(object, "context_sha256")
	if hash(canonicalJSON(object)) != receipt.ContextSHA256 {
		return nil, fmt.Errorf("backbone context digest mismatch")
	}
	return &receipt, nil
}

// Verify rechecks bytes, current HEAD and the explicit Task Packet immediately
// before admission. Semantic routing completeness belongs to the approved
// producer and operator adoption; candidate code is never run to infer it.
func Verify(ctx context.Context, workspace string, binding *Binding, instructorRole string) (*VerifiedContext, error) {
	if binding == nil || binding.SchemaVersion != "1" || !sha256Pattern.MatchString(binding.AdapterSHA256) {
		return nil, fmt.Errorf("missing or invalid Backbone binding")
	}
	bound, ok := binding.Contexts[instructorRole]
	if !ok {
		return nil, fmt.Errorf("no Backbone context for Instructor role")
	}
	receipt, err := parseContext(bound.Context, bound.Role)
	if err != nil {
		return nil, err
	}
	root, err := safeRoot(workspace)
	if err != nil {
		return nil, err
	}
	head, err := gitRead(ctx, root, 128, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || strings.TrimSpace(string(head)) != receipt.Revision {
		return nil, fmt.Errorf("backbone context does not match actual Git HEAD")
	}
	verified := &VerifiedContext{Context: *receipt, TaskPacket: bound.TaskPacket, Documents: map[string][]byte{}, NativeDocumentEvidence: map[string]string{}}
	total := 0
	for _, document := range receipt.Documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, anchor, err := relative(document.Path, true)
		if err != nil {
			return nil, err
		}
		data, exists := verified.Documents[path]
		if !exists {
			data, err = readDocument(root, path)
			if err != nil {
				return nil, err
			}
			total += len(data)
			if total > MaxPromptBytes {
				return nil, fmt.Errorf("backbone context document bytes exceed limit")
			}
			verified.Documents[path] = data
		}
		if hash(data) != document.SHA256 {
			return nil, fmt.Errorf("backbone document hash mismatch: %s", path)
		}
		if anchor != "" && !hasHeading(data, anchor) {
			return nil, fmt.Errorf("backbone heading absent or unsupported: %s", document.Path)
		}
	}
	companion, ok := verified.Documents[".engineering-backbone/opencode.json"]
	if !ok || hash(companion) != binding.AdapterSHA256 {
		return nil, fmt.Errorf("backbone adapter companion missing or stale")
	}
	var adapter struct {
		SchemaVersion   string `json:"schema_version"`
		AdapterVersion  string `json:"adapter_version"`
		Enabled         bool   `json:"enabled"`
		OpenCodeVersion string `json:"opencode_version"`
		NativeConfig    string `json:"native_config"`
	}
	if err := DecodeStrict(companion, &adapter); err != nil {
		return nil, err
	}
	if adapter.SchemaVersion != "1" || adapter.AdapterVersion != "1.0.0" || !adapter.Enabled || adapter.OpenCodeVersion != "1.19.0" || adapter.NativeConfig != "opencode.json" {
		return nil, fmt.Errorf("backbone adapter is inactive or incompatible")
	}
	packetData, ok := verified.Documents[bound.TaskPacket]
	if !ok {
		return nil, fmt.Errorf("explicit Backbone Task Packet absent from receipt")
	}
	if err := validatePacket(packetData, receipt); err != nil {
		return nil, err
	}
	if err := mandatoryDocuments(verified); err != nil {
		return nil, err
	}
	if err := VerifyCanonicalSkills(ctx, root, verified); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(verified.Documents))
	for path := range verified.Documents {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var prompt strings.Builder
	prompt.WriteString("Approved Engineering Backbone context (structural receipt; native/model qualification is a separate gate).\n")
	for _, path := range paths {
		fmt.Fprintf(&prompt, "\n--- BEGIN BACKBONE DOCUMENT %s ---\n", path)
		prompt.Write(verified.Documents[path])
		fmt.Fprintf(&prompt, "\n--- END BACKBONE DOCUMENT %s ---\n", path)
	}
	if prompt.Len() > MaxPromptBytes {
		return nil, fmt.Errorf("backbone standing prompt exceeds limit")
	}
	verified.Prompt = prompt.String()
	return verified, nil
}

var roleSkills = map[string][]string{
	"planner":         {"task-intake", "implementation-plan"},
	"implementer":     {"task-intake", "bug-reproduction", "test-and-quality", "docs-traceability"},
	"reviewer":        {"task-intake", "implementation-plan", "code-review", "delta-review"},
	"quality_checker": {"task-intake", "test-and-quality"},
	"triage":          {"task-intake", "finding-triage"},
}

func mandatoryDocuments(verified *VerifiedContext) error {
	required := []string{"AGENTS.md", "opencode.json", ".engineering-backbone/opencode.json", ".opencode/agents/backbone-" + strings.ReplaceAll(verified.Context.Role, "_", "-") + ".md"}
	// Source and installed repositories use the same native boundary but distinct
	// locations for their shared guide and runner contract.
	if _, source := verified.Documents["docs/opencode/workflow.md"]; source {
		required = append(required, "docs/opencode/workflow.md", "docs/opencode/opencode-runner-contract.md")
	} else {
		required = append(required, ".engineering-backbone/opencode-guide.md", ".engineering-backbone/opencode-runner-contract.md")
	}
	for _, skill := range roleSkills[verified.Context.Role] {
		required = append(required, ".opencode/skills/"+skill+"/SKILL.md")
	}
	config, ok := verified.Documents["opencode.json"]
	if !ok {
		return fmt.Errorf("backbone native config absent")
	}
	var native struct {
		Schema       string          `json:"$schema"`
		Instructions []string        `json:"instructions"`
		Permission   json.RawMessage `json:"permission"`
	}
	if err := DecodeStrict(config, &native); err != nil {
		return err
	}
	if native.Schema != "https://opencode.ai/config.json" {
		return fmt.Errorf("invalid Backbone native config schema")
	}
	if err := sequence(native.Instructions, true, 64); err != nil {
		return err
	}
	for _, instruction := range native.Instructions {
		if _, _, err := relative(instruction, false); err != nil {
			return err
		}
		required = append(required, instruction)
	}
	for _, path := range required {
		if _, ok := verified.Documents[path]; !ok {
			return fmt.Errorf("required Backbone document absent: %s", path)
		}
	}
	return nil
}

func oneOf(value string, values ...string) bool {
	for _, item := range values {
		if value == item {
			return true
		}
	}
	return false
}
func hasControl(value string) bool {
	for _, r := range value {
		if r < 32 || r >= 127 && r <= 159 {
			return true
		}
	}
	return false
}
func validText(value string, limit int) error {
	if strings.TrimSpace(value) == "" || len(value) > limit || hasControl(value) {
		return fmt.Errorf("invalid Backbone text value")
	}
	return nil
}
func sequence(values []string, nonempty bool, maximum int) error {
	if len(values) > maximum || nonempty && len(values) == 0 {
		return fmt.Errorf("invalid Backbone sequence size")
	}
	seen := map[string]bool{}
	for _, value := range values {
		if err := validText(value, 4096); err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("duplicate Backbone sequence entry")
		}
		seen[value] = true
	}
	return nil
}

package instructor

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/backbone"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// qualification is an operator attestation, not a signature or a claim that AO
// independently ran a model. No record is synthesized from stub tests.
type qualification struct {
	SchemaVersion         string                       `json:"schemaVersion"`
	Harness               string                       `json:"harness"`
	RuntimeVersion        string                       `json:"runtimeVersion"`
	Model                 string                       `json:"model"`
	ModelSnapshot         string                       `json:"modelSnapshot,omitempty"`
	Role                  string                       `json:"role"`
	Platform              string                       `json:"platform"`
	Interface             string                       `json:"interface"`
	ConfigurationContract string                       `json:"configurationContract"`
	ObservedAt            string                       `json:"observedAt"`
	Status                string                       `json:"status"`
	EvidenceRef           string                       `json:"evidenceRef"`
	EvidenceSHA256        string                       `json:"evidenceSha256"`
	Cases                 map[string]qualificationCase `json:"cases"`
}

type qualificationCase struct {
	Result      string `json:"result"`
	EvidenceRef string `json:"evidenceRef"`
}

var qualificationCases = []string{"context_loading", "stale_context", "tool_boundaries", "failed_check_reporting", "delegation_context", "resume_context"}

func policyBackbone(p domain.InstructorPolicy) (*backbone.Binding, error) {
	if len(p.Backbone) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(p.Roles))
	for name := range p.Roles {
		names = append(names, name)
	}
	binding, err := backbone.ValidateBinding(p.Backbone, names)
	if err != nil {
		return nil, invalid("Backbone binding: " + err.Error())
	}
	return binding, nil
}

func validateQualification(raw json.RawMessage, role, model string) error {
	var q qualification
	if len(raw) == 0 {
		return invalid("OpenCode Backbone execution requires an explicit qualification")
	}
	if err := backbone.DecodeStrict(raw, &q); err != nil {
		return invalid("invalid OpenCode qualification: " + err.Error())
	}
	if q.SchemaVersion != "1" || q.Harness != "opencode" || q.RuntimeVersion != "1.19.0" || q.Model != model || q.Model == "" || q.Role != role || q.Platform != runtime.GOOS || q.Interface != "chat" || q.ConfigurationContract != "ao-opencode-backbone-v1" || q.Status != "qualified" {
		return invalid("OpenCode qualification does not match the exact runtime, model, role, platform, interface and configuration contract")
	}
	if _, err := time.Parse(time.RFC3339, q.ObservedAt); err != nil {
		return invalid("OpenCode qualification observedAt must be RFC3339")
	}
	if !nonemptyEvidence(q.EvidenceRef) || len(q.EvidenceSHA256) != 64 || strings.ToLower(q.EvidenceSHA256) != q.EvidenceSHA256 {
		return invalid("OpenCode qualification evidence is incomplete")
	}
	if _, err := hex.DecodeString(q.EvidenceSHA256); err != nil {
		return invalid("OpenCode qualification evidence digest is invalid")
	}
	if len(q.Cases) != len(qualificationCases) {
		return invalid("OpenCode qualification requires all six exact cases")
	}
	for _, name := range qualificationCases {
		c, ok := q.Cases[name]
		if !ok || c.Result != "pass" || !nonemptyEvidence(c.EvidenceRef) {
			return invalid("OpenCode qualification case is missing or not passing: " + name)
		}
	}
	return nil
}

func nonemptyEvidence(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 4096 && !strings.ContainsAny(value, "\x00\r\n")
}

// validateBackboneSelection performs no provider I/O. Qualified native choices
// are read back only inside the controlled ACP launch after static input checks.
func validateBackboneSelection(p domain.InstructorPolicy, name, harness, model string) (bool, error) {
	binding, err := policyBackbone(p)
	if err != nil || binding == nil {
		return false, err
	}
	role, found := binding.Contexts[name]
	if harness != "opencode" {
		if found {
			return true, invalid("Backbone execution binding currently supports OpenCode ACP only")
		}
		return false, nil
	}
	if !found {
		return true, invalid("OpenCode role has no pinned Backbone context")
	}
	if role.Role != "implementer" {
		return true, invalid("governed OpenCode currently supports only implementer; read-only OS isolation is unavailable")
	}
	return true, validateQualification(role.Qualification, role.Role, model)
}

// BackbonePreflight identifies governed launches without invoking a provider or
// registering work. It runs before general discovery can load native plugins.
func (s *Service) BackbonePreflight(ctx context.Context, in SpawnInput, project domain.ProjectRecord, mode domain.SessionMode, permissions ports.PermissionMode) (bool, error) {
	workID, err := s.ResolveSpawnWork(ctx, in.ParentSessionID, in.WorkID, in.Role)
	if err != nil {
		return false, err
	}
	var p domain.InstructorPolicy
	if workID != "" {
		w, e := s.get(ctx, workID)
		if e != nil {
			return false, e
		}
		p = w.Policy
		if w.PolicyVersion != PolicyVersion(p) {
			return false, invalid("pinned work policy hash mismatch")
		}
	} else {
		var e error
		p, _, e = s.ProjectPolicyTemplate(ctx, project)
		if e != nil {
			return false, e
		}
	}
	governed, err := validateBackboneSelection(p, in.Role, in.Harness, in.Model)
	if err != nil || !governed {
		return governed, err
	}
	if in.Role == "orchestrator" && workID == "" {
		return true, invalid("governed OpenCode requires an explicit durable work; standalone project controllers cannot adopt the binding")
	}
	if domain.NormalizeSessionMode(mode) != domain.SessionModeChat || ports.NormalizePermissionMode(permissions) != ports.PermissionModeDefault {
		return true, invalid("governed OpenCode requires fresh Chat ACP and default permissions; TUI is not qualified")
	}
	if in.Model == "" || in.Effort == "" {
		return true, invalid("governed OpenCode requires explicit model and effort")
	}
	return true, nil
}

// PrepareBackboneController rechecks the actual session workspace and returns a
// detached immutable launch binding. The caller appends prompt as its final
// system-prompt suffix, then the adapter verifies it again before native I/O.
func (s *Service) PrepareBackboneController(ctx context.Context, id domain.SessionID, workspace string, mode domain.SessionMode, model string, permissions ports.PermissionMode) (*ports.ChatBackboneConfig, string, error) {
	w, managed, err := s.WorkForSession(ctx, id)
	if err != nil || !managed {
		return nil, "", err
	}
	if w.PolicyVersion != PolicyVersion(w.Policy) {
		return nil, "", invalid("pinned work policy hash mismatch")
	}
	role, rec, err := s.sessionRole(ctx, w, id)
	if err != nil {
		return nil, "", err
	}
	if rec == nil {
		reader, ok := s.deps.Store.(interface {
			GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
		})
		if !ok {
			return nil, "", invalid("session reader unavailable")
		}
		r, found, e := reader.GetSession(ctx, id)
		if e != nil {
			return nil, "", e
		}
		if !found {
			return nil, "", invalid("session missing")
		}
		rec = &r
	}
	governed, err := validateBackboneSelection(w.Policy, role, string(rec.Harness), model)
	if err != nil || !governed {
		return nil, "", err
	}
	if mode != domain.SessionModeChat || ports.NormalizePermissionMode(permissions) != ports.PermissionModeDefault {
		return nil, "", invalid("governed OpenCode requires fresh Chat ACP and default permissions")
	}
	binding, err := policyBackbone(w.Policy)
	if err != nil {
		return nil, "", err
	}
	verified, err := backbone.Verify(ctx, workspace, binding, role)
	if err != nil {
		return nil, "", invalid("Backbone context: " + err.Error())
	}
	return &ports.ChatBackboneConfig{Binding: bytes.Clone(w.Policy.Backbone), InstructorRole: role, PolicyVersion: w.PolicyVersion, Model: model, Effort: rec.Metadata.Effort}, verified.Prompt, nil
}

func (s *Service) validateBackboneSession(ctx context.Context, w domain.InstructorWork, role string, rec domain.SessionRecord) error {
	governed, err := validateBackboneSelection(w.Policy, role, string(rec.Harness), rec.Metadata.Model)
	if err != nil || !governed {
		return err
	}
	_, _, err = s.PrepareBackboneController(ctx, rec.ID, rec.Metadata.WorkspacePath, domain.NormalizeSessionMode(rec.Mode), rec.Metadata.Model, rec.Metadata.Permissions)
	return err
}

// ValidateBackboneControllerBinding fences queue dispatch against policy rebind:
// the running provider must carry the exact binding admitted for this work.
func (s *Service) ValidateBackboneControllerBinding(ctx context.Context, id domain.SessionID, binding *ports.ChatBackboneConfig) error {
	w, managed, err := s.WorkForSession(ctx, id)
	if err != nil {
		return err
	}
	if !managed {
		if binding != nil {
			return invalid("Backbone controller lost its work binding")
		}
		return nil
	}
	role, rec, err := s.sessionRole(ctx, w, id)
	if err != nil {
		return err
	}
	if rec == nil {
		r, ok := s.deps.Store.(interface {
			GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
		})
		if !ok {
			return invalid("session reader unavailable")
		}
		value, found, e := r.GetSession(ctx, id)
		if e != nil {
			return e
		}
		if !found {
			return invalid("session missing")
		}
		rec = &value
	}
	governed, err := validateBackboneSelection(w.Policy, role, string(rec.Harness), rec.Metadata.Model)
	if err != nil {
		return err
	}
	if !governed {
		if binding != nil {
			return invalid("Backbone binding changed; replace controller")
		}
		return nil
	}
	if w.PolicyVersion != PolicyVersion(w.Policy) {
		return invalid("pinned work policy hash mismatch")
	}
	if binding == nil || binding.PolicyVersion != w.PolicyVersion || binding.InstructorRole != role || binding.Model != rec.Metadata.Model || binding.Effort != rec.Metadata.Effort || !bytes.Equal(binding.Binding, w.Policy.Backbone) {
		return invalid("Backbone binding changed; replace controller before dispatch")
	}
	return nil
}

func backboneReplacementError() error {
	return fmt.Errorf("%w: governed OpenCode configuration is immutable; replace its controlled process", ports.ErrChatConfigOptionInvalid)
}

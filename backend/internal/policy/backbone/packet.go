package backbone

import (
	"fmt"
	"strings"
	"unicode"
)

type taskPacket struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	Objective     string `json:"objective"`
	Role          string `json:"role"`
	Risk          string `json:"risk"`
	Repository    struct {
		BaseBranch   string  `json:"base_branch"`
		BaseRevision *string `json:"base_revision" bbnullable:"true"`
		HeadRevision *string `json:"head_revision" bbnullable:"true"`
	} `json:"repository"`
	ApprovedSources []string `json:"approved_sources"`
	Scope           struct {
		Include []string `json:"include"`
		Exclude []string `json:"exclude"`
	} `json:"scope"`
	Acceptance []struct {
		ID           string   `json:"id"`
		Description  string   `json:"description"`
		Verification string   `json:"verification"`
		Status       string   `json:"status"`
		Evidence     []string `json:"evidence"`
	} `json:"acceptance"`
	Validation struct {
		Commands       []string `json:"commands"`
		ManualEvidence []string `json:"manual_evidence"`
	} `json:"validation"`
	ExecutionBudget struct {
		MaxIterations int `json:"max_iterations"`
	} `json:"execution_budget"`
	StopConditions   []string `json:"stop_conditions"`
	ForbiddenActions []string `json:"forbidden_actions"`
	Status           string   `json:"status"`
}

func validatePacket(data []byte, receipt *ContextReceipt) error {
	var packet taskPacket
	if err := DecodeStrict(data, &packet); err != nil {
		return err
	}
	if packet.SchemaVersion != "1" || packet.ID != receipt.TaskID || packet.Role != receipt.Role || !knownRole(packet.Role) {
		return fmt.Errorf("backbone Task Packet identity mismatch")
	}
	for _, value := range []string{packet.ID, packet.Title, packet.Objective} {
		if err := validText(value, 4096); err != nil {
			return err
		}
	}
	if !oneOf(packet.Risk, "low", "medium", "high", "critical") || !oneOf(packet.Status, "planned", "active", "blocked", "ready_for_review", "completed") {
		return fmt.Errorf("invalid Backbone Task Packet risk/status")
	}
	if packet.ExecutionBudget.MaxIterations < 1 || packet.ExecutionBudget.MaxIterations > 10 {
		return fmt.Errorf("invalid Backbone Task Packet execution budget")
	}
	for _, list := range [][]string{packet.ApprovedSources, packet.StopConditions, packet.ForbiddenActions, packet.Scope.Include} {
		if err := sequence(list, true, 256); err != nil {
			return err
		}
	}
	for _, list := range [][]string{packet.Validation.Commands, packet.Validation.ManualEvidence, packet.Scope.Exclude} {
		if err := sequence(list, false, 256); err != nil {
			return err
		}
	}
	for _, patterns := range [][]string{packet.Scope.Include, packet.Scope.Exclude} {
		for _, pattern := range patterns {
			if len(pattern) > 512 || strings.HasPrefix(pattern, "/") || strings.ContainsAny(pattern, `\:`) {
				return fmt.Errorf("invalid Backbone task scope")
			}
			for _, part := range strings.Split(pattern, "/") {
				if oneOf(part, "", ".", "..") {
					return fmt.Errorf("invalid Backbone task scope")
				}
			}
		}
	}
	if len(packet.Acceptance) == 0 || len(packet.Acceptance) > 256 {
		return fmt.Errorf("invalid Backbone Task Packet acceptance count")
	}
	ids := map[string]bool{}
	for _, criterion := range packet.Acceptance {
		if err := validText(criterion.ID, 4096); err != nil {
			return err
		}
		if ids[criterion.ID] {
			return fmt.Errorf("duplicate Backbone acceptance ID")
		}
		ids[criterion.ID] = true
		if err := validText(criterion.Description, 4096); err != nil {
			return err
		}
		if !oneOf(criterion.Verification, "automatic", "manual", "hybrid") || !oneOf(criterion.Status, "pending", "pass", "fail", "blocked") {
			return fmt.Errorf("invalid Backbone acceptance criterion")
		}
		if err := sequence(criterion.Evidence, criterion.Status == "pass", 256); err != nil {
			return err
		}
		if packet.Status == "completed" && criterion.Status != "pass" {
			return fmt.Errorf("completed Backbone task has unresolved acceptance")
		}
	}
	branch := packet.Repository.BaseBranch
	if err := validText(branch, 4096); err != nil {
		return err
	}
	if branch == "HEAD" || strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "/") || strings.HasPrefix(branch, ".") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") || strings.ContainsAny(branch, `~^:?*[\`) || strings.Contains(branch, "..") || strings.Contains(branch, "//") || strings.Contains(branch, "@{") || strings.IndexFunc(branch, unicode.IsSpace) >= 0 {
		return fmt.Errorf("invalid Backbone task base branch")
	}
	for _, part := range strings.Split(branch, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return fmt.Errorf("invalid Backbone task base branch")
		}
	}
	frozen := oneOf(packet.Status, "completed", "ready_for_review") || packet.Status == "active" && packet.Role == "reviewer"
	for _, revision := range []*string{packet.Repository.BaseRevision, packet.Repository.HeadRevision} {
		if revision == nil {
			if frozen {
				return fmt.Errorf("frozen Backbone task requires revisions")
			}
			continue
		}
		if !revisionPattern.MatchString(*revision) {
			return fmt.Errorf("invalid Backbone task revision")
		}
	}
	if packet.Role == "reviewer" && packet.Repository.HeadRevision == nil {
		return fmt.Errorf("reviewer Backbone context requires frozen head")
	}
	if packet.Repository.HeadRevision != nil && *packet.Repository.HeadRevision != receipt.Revision {
		return fmt.Errorf("backbone Task Packet reviewed head differs from receipt")
	}
	return nil
}

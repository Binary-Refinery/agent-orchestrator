package modelcatalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// OpenCode's read-only `models --verbose` prints provider/model followed by
// that model's JSON metadata. The variants are already resolved by OpenCode,
// including custom provider config; AO does not invent a shared effort enum.
func parseOpenCodeModels(output []byte) ([]ports.AgentModelInfo, error) {
	remaining := strings.TrimSpace(ansiPattern.ReplaceAllString(string(output), ""))
	var models []ports.AgentModelInfo
	for remaining != "" {
		line, rest, _ := strings.Cut(remaining, "\n")
		remaining = strings.TrimLeft(rest, "\r\n\t ")
		id := strings.TrimSpace(line)
		if !strings.Contains(id, "/") || !looksLikeModelID(id) || len(strings.Fields(id)) != 1 {
			continue
		}
		item := ports.AgentModelInfo{ID: id, Label: id, EffortSupport: "unknown"}
		if strings.HasPrefix(remaining, "{") {
			var metadata struct {
				Name           string          `json:"name"`
				Variants       json.RawMessage `json:"variants"`
				DefaultVariant string          `json:"defaultVariant"`
				IsDefault      bool            `json:"isDefault"`
			}
			decoder := json.NewDecoder(strings.NewReader(remaining))
			if err := decoder.Decode(&metadata); err != nil {
				return nil, fmt.Errorf("OpenCode metadata for %q: %w", id, err)
			}
			remaining = strings.TrimLeft(remaining[decoder.InputOffset():], "\r\n\t ")
			if metadata.Name != "" {
				item.Label = metadata.Name
			}
			item.IsDefault = metadata.IsDefault
			if len(metadata.Variants) != 0 && string(metadata.Variants) != "null" {
				var err error
				item.Efforts, err = openCodeVariants(metadata.Variants)
				if err != nil {
					return nil, fmt.Errorf("OpenCode variants for %q: %w", id, err)
				}
				item.EffortSupport = "none"
				if len(item.Efforts) != 0 {
					item.EffortSupport = "supported"
				}
				for _, variant := range item.Efforts {
					if variant == metadata.DefaultVariant {
						item.DefaultEffort = variant
					}
				}
			}
		}
		models = append(models, item)
	}
	return normalize(models), nil
}

func openCodeVariants(raw json.RawMessage) ([]string, error) {
	var variants []string
	appendVariant := func(id string, disabled bool) {
		if id = strings.TrimSpace(id); id != "" && !disabled {
			variants = append(variants, id)
		}
	}
	// V2 exposes an array with native IDs; V1 uses ordered object keys. Preserve
	// those names verbatim, including custom names such as deep or contributor.
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		var entries []struct {
			ID       string `json:"id"`
			Disabled bool   `json:"disabled"`
		}
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, err
		}
		for _, entry := range entries {
			appendVariant(entry.ID, entry.Disabled)
		}
		return variants, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected variant object or array")
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		var settings struct {
			Disabled bool `json:"disabled"`
		}
		if err := decoder.Decode(&settings); err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("variant name is not a string")
		}
		appendVariant(name, settings.Disabled)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after variants")
	}
	return variants, nil
}

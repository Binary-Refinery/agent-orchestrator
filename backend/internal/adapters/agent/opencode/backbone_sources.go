package opencode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/hookutil"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/processenv"
	"github.com/aoagents/agent-orchestrator/backend/internal/policy/backbone"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// PrepareBackboneEnvironment isolates native configuration and account state.
// The binding remains the service's pinned authority; these paths are inputs.
func PrepareBackboneEnvironment(dataDir, sessionID string, binding *ports.ChatBackboneConfig, overlay map[string]string) (map[string]string, error) {
	if binding == nil || sessionID == "" || !filepath.IsAbs(dataDir) {
		return nil, fmt.Errorf("OpenCode Backbone: missing owned process context")
	}
	// Directory names never incorporate unchecked session or model path syntax.
	scope := fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID)))
	raw, _ := json.Marshal(binding)
	identity := fmt.Sprintf("%x", sha256.Sum256(raw))
	base := filepath.Join(dataDir, "opencode-backbone", scope, identity)
	if err := backboneMakeDirectory(base); err != nil {
		return nil, err
	}
	marker := filepath.Join(base, "owner.json")
	identityBytes := []byte(`{"schemaVersion":"1","bindingSha256":"` + identity + `"}`)
	if data, err := os.ReadFile(marker); err == nil {
		if !bytes.Equal(data, identityBytes) {
			return nil, fmt.Errorf("OpenCode Backbone: native state ownership mismatch")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(base)
		if err != nil || len(entries) != 0 {
			return nil, fmt.Errorf("OpenCode Backbone: native state has no ownership record")
		}
		f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("OpenCode Backbone: cannot establish native state ownership")
		}
		_, writeErr := f.Write(identityBytes)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			return nil, fmt.Errorf("OpenCode Backbone: cannot write native state ownership")
		}
	} else {
		return nil, fmt.Errorf("OpenCode Backbone: cannot read native state ownership")
	}
	if err := backboneRegular(marker); err != nil {
		return nil, err
	}
	inherited := map[string]string{}
	for _, entry := range processenv.Merge(overlay) {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			if runtime.GOOS == "windows" {
				key = strings.ToUpper(key)
			}
			inherited[key] = value
		}
	}
	provider, _, _ := strings.Cut(binding.Model, "/")
	credentials := map[string][]string{
		"anthropic": {"ANTHROPIC_API_KEY"}, "openai": {"OPENAI_API_KEY"}, "google": {"GOOGLE_GENERATIVE_AI_API_KEY", "GEMINI_API_KEY"},
		"openrouter": {"OPENROUTER_API_KEY"}, "opencode": {"OPENCODE_API_KEY"}, "opencode-go": {"OPENCODE_API_KEY"},
		"xai": {"XAI_API_KEY"}, "deepseek": {"DEEPSEEK_API_KEY"}, "groq": {"GROQ_API_KEY"}, "mistral": {"MISTRAL_API_KEY"},
	}
	allowed := map[string]bool{"PATH": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true, "PROGRAMDATA": true, "LANG": true, "LC_ALL": true, "TZ": true}
	for _, key := range credentials[provider] {
		allowed[key] = true
	}
	result := map[string]string{}
	for key, value := range inherited {
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "OPENCODE_") && upper != "OPENCODE_API_KEY" && value != "" {
			return nil, fmt.Errorf("OpenCode Backbone: inherited native overrides are not qualified")
		}
		// Empty overlays also remove unsafe inherited variables when processenv
		// merges this exact environment for probes and the persistent child host.
		result[key] = ""
		if allowed[upper] {
			result[key] = value
		}
	}
	dirs := map[string]string{"HOME": "home", "USERPROFILE": "home", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache", "APPDATA": "appdata", "LOCALAPPDATA": "localappdata", "TEMP": "tmp", "TMP": "tmp", "TMPDIR": "tmp"}
	for key, name := range dirs {
		path := filepath.Join(base, name)
		if err := backboneMakeDirectory(path); err != nil {
			return nil, err
		}
		result[key] = path
	}
	result["OPENCODE_PURE"] = "1"
	result["OPENCODE_DISABLE_AUTOUPDATE"] = "1"
	result["OPENCODE_DISABLE_AUTOCOMPACT"] = "1"
	result["OPENCODE_DISABLE_MODELS_FETCH"] = "1"
	result["OPENCODE_DISABLE_TERMINAL_TITLE"] = "1"
	result["GIT_CONFIG_NOSYSTEM"] = "1"
	result["GIT_CONFIG_GLOBAL"] = os.DevNull
	// Native npm installs use ignoreScripts. Offline mode additionally prevents
	// unbound registry downloads; it is not a network sandbox claim.
	result["npm_config_offline"] = "true"
	result["npm_config_userconfig"] = os.DevNull
	result["npm_config_globalconfig"] = os.DevNull
	result["npm_config_audit"] = "false"
	result["npm_config_fund"] = "false"
	result["NO_COLOR"] = "1"
	return result, nil
}

func backboneMakeDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("OpenCode Backbone: state path must be absolute")
	}
	clean := filepath.Clean(path)
	var missing []string
	for current := clean; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, current)
		} else if err != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("OpenCode Backbone: unsafe native state directory")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("OpenCode Backbone: cannot create native state directory")
		}
	}
	return nil
}
func backboneRegular(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("OpenCode Backbone: unsafe native input file")
	}
	return nil
}
func backboneAbsent(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("OpenCode Backbone: unbound native input source")
}

// ValidateBackboneSources runs before every executable native probe. It reads
// candidate files as bounded data and never imports their code.
func ValidateBackboneSources(ctx context.Context, workspace string, env map[string]string, verified *backbone.VerifiedContext) error {
	if verified == nil || verified.Context.Role != "implementer" {
		return fmt.Errorf("OpenCode Backbone: implementer context is required")
	}
	if _, err := backboneProject(verified.Documents["opencode.json"], verified.Documents); err != nil {
		return err
	}
	for current := filepath.Dir(workspace); ; current = filepath.Dir(current) {
		for _, name := range []string{".opencode", ".agents", ".claude", "AGENTS.md", "CLAUDE.md", "CONTEXT.md", "opencode.json", "opencode.jsonc", ".npmrc", "bunfig.toml"} {
			if err := backboneAbsent(filepath.Join(current, name)); err != nil {
				return err
			}
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	count := 0
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("OpenCode Backbone: cannot inspect native input sources")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 100000 {
			return fmt.Errorf("OpenCode Backbone: native source scan exceeds limit")
		}
		relative, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			return nil
		}
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		lower := strings.ToLower(entry.Name())
		if entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("OpenCode Backbone: symbolic or special source path is unsupported")
		}
		if entry.IsDir() {
			if lower == ".agents" || lower == ".claude" || (lower == ".opencode" && relative != ".opencode") {
				return fmt.Errorf("OpenCode Backbone: alternate native source directory")
			}
			if strings.HasPrefix(relative, ".opencode/") {
				parts := strings.Split(relative, "/")
				if len(parts) == 2 && parts[1] != "agents" && parts[1] != "skills" && parts[1] != "node_modules" {
					return fmt.Errorf("OpenCode Backbone: unsupported native namespace")
				}
				if len(parts) == 2 && parts[1] == "node_modules" {
					if err := backboneEmptyDirectory(path); err != nil {
						return err
					}
					return filepath.SkipDir
				}
			}
			return nil
		}
		if lower == ".npmrc" || lower == "bunfig.toml" {
			return fmt.Errorf("OpenCode Backbone: unbound native package configuration")
		}
		if lower == "opencode.json" || lower == "opencode.jsonc" {
			if relative != "opencode.json" && relative != ".engineering-backbone/opencode.json" {
				return fmt.Errorf("OpenCode Backbone: alternate native configuration")
			}
		}
		if lower == "agents.md" || lower == "claude.md" || lower == "context.md" {
			if _, ok := verified.Documents[relative]; !ok {
				return fmt.Errorf("OpenCode Backbone: unbound implicit instructions")
			}
		}
		if strings.HasPrefix(relative, ".opencode/agents/") && strings.HasSuffix(lower, ".md") {
			if _, ok := verified.NativeDocumentEvidence[relative]; !ok {
				return fmt.Errorf("OpenCode Backbone: unbound native agent")
			}
		}
		if strings.HasPrefix(relative, ".opencode/") && lower == "skill.md" {
			if _, ok := verified.NativeDocumentEvidence[relative]; !ok {
				return fmt.Errorf("OpenCode Backbone: unbound recursive native skill")
			}
		}
		if strings.HasPrefix(relative, ".opencode/") && (lower == "package.json" || lower == "package-lock.json" || lower == "bun.lock" || lower == "bun.lockb") {
			if err := backbonePackageMetadata(path); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, home := range []string{env["HOME"], env["USERPROFILE"], env["XDG_CONFIG_HOME"]} {
		if home == "" {
			return fmt.Errorf("OpenCode Backbone: missing isolated native home")
		}
		for _, name := range []string{".opencode", ".agents", ".claude", "AGENTS.md", "CLAUDE.md", ".npmrc", "bunfig.toml"} {
			if err := backboneAbsent(filepath.Join(home, name)); err != nil {
				return err
			}
		}
	}
	global := filepath.Join(env["XDG_CONFIG_HOME"], "opencode")
	for _, name := range []string{"config", "config.json", "opencode.json", "opencode.jsonc", "AGENTS.md", "CLAUDE.md", "agents", "agent", "skills", "skill", "modes", "mode", "plugins", "plugin", "tools", "tool", "commands", "command", ".npmrc", "bunfig.toml"} {
		if err := backboneAbsent(filepath.Join(global, name)); err != nil {
			return err
		}
	}
	for _, name := range []string{"package.json", "package-lock.json", "bun.lock", "bun.lockb"} {
		if err := backbonePackageMetadata(filepath.Join(global, name)); err != nil {
			return err
		}
	}
	if err := backboneEmptyDirectory(filepath.Join(global, "node_modules")); err != nil {
		return err
	}
	data := filepath.Join(env["XDG_DATA_HOME"], "opencode")
	if err := backboneAbsent(filepath.Join(data, "auth.json")); err != nil {
		return err
	}
	if err := backboneAccountState(ctx, filepath.Join(data, "opencode.db")); err != nil {
		return err
	}
	managed := "/etc/opencode"
	if runtime.GOOS == "windows" {
		programData := env["ProgramData"]
		if programData == "" {
			programData = env["PROGRAMDATA"]
		}
		if programData == "" {
			programData = `C:\ProgramData`
		}
		managed = filepath.Join(programData, "opencode")
	}
	if runtime.GOOS == "darwin" {
		managed = "/Library/Application Support/opencode"
		current, err := user.Current()
		if err != nil {
			return fmt.Errorf("OpenCode Backbone: cannot verify managed preference locations")
		}
		managedPreferences := "/Library/Managed Preferences"
		for _, dir := range []string{managedPreferences, filepath.Join(managedPreferences, current.Username)} {
			if err := backboneAbsent(filepath.Join(dir, "ai.opencode.managed.plist")); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"opencode.json", "opencode.jsonc"} {
		if err := backboneAbsent(filepath.Join(managed, name)); err != nil {
			return err
		}
	}
	return nil
}

func backboneAccountState(ctx context.Context, path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return fmt.Errorf("OpenCode Backbone: invalid native account store")
	}
	queryCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro&_pragma=busy_timeout(250)"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return fmt.Errorf("OpenCode Backbone: cannot inspect native account state")
	}
	defer func() { _ = db.Close() }()
	for _, table := range []string{"account", "control_account"} {
		var exists int
		if err := db.QueryRowContext(queryCtx, "SELECT count(*) FROM sqlite_master WHERE name = ?", table).Scan(&exists); err != nil {
			return fmt.Errorf("OpenCode Backbone: cannot inspect native account state")
		}
		if exists == 0 {
			continue
		}
		var count int
		if err := db.QueryRowContext(queryCtx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			return fmt.Errorf("OpenCode Backbone: native organization accounts are not qualified")
		}
	}
	return nil
}

// WriteBackboneConfig avoids OS per-variable argument limits for the complete
// verified standing prompt. The unique primary cannot be replaced by a project
// config: that source permits only schema, explicit instructions and permission.
func WriteBackboneConfig(env map[string]string, content string) (string, error) {
	path := filepath.Join(filepath.Dir(env["HOME"]), "runtime.json")
	if _, err := os.Lstat(path); err == nil {
		if err := backboneRegular(path); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("OpenCode Backbone: cannot inspect owned runtime config")
	}
	if err := hookutil.AtomicWriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("OpenCode Backbone: cannot write owned runtime config")
	}
	return path, nil
}

// CheckBackboneConfig confirms the owned launch configuration still matches.
func CheckBackboneConfig(env map[string]string, content string) error {
	path := filepath.Join(filepath.Dir(env["HOME"]), "runtime.json")
	if env["OPENCODE_CONFIG"] != path || env["OPENCODE_CONFIG_CONTENT"] != "" {
		return fmt.Errorf("OpenCode Backbone: native runtime config source mismatch")
	}
	if err := backboneRegular(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != int64(len(content)) {
		return fmt.Errorf("OpenCode Backbone: owned runtime config changed")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, []byte(content)) {
		return fmt.Errorf("OpenCode Backbone: owned runtime config changed")
	}
	return nil
}

func backboneEmptyDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("OpenCode Backbone: unqualified native package directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("OpenCode Backbone: installed native package code is not qualified")
	}
	return nil
}
func backbonePackageMetadata(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return fmt.Errorf("OpenCode Backbone: invalid native package metadata")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return fmt.Errorf("OpenCode Backbone: invalid native package metadata")
	}
	var value map[string]json.RawMessage
	if err := backbone.DecodeStrict(data, &value); err != nil {
		return fmt.Errorf("OpenCode Backbone: unbound native package metadata")
	}
	for key := range value {
		if key != "dependencies" && key != "name" && key != "version" && key != "lockfileVersion" && key != "requires" && key != "packages" {
			return fmt.Errorf("OpenCode Backbone: unsupported native package metadata")
		}
	}
	if raw, ok := value["dependencies"]; ok {
		var dependencies map[string]string
		if json.Unmarshal(raw, &dependencies) != nil || len(dependencies) != 1 || dependencies["@opencode-ai/plugin"] != backboneRuntimeVersion {
			return fmt.Errorf("OpenCode Backbone: unqualified native dependencies")
		}
	}
	if raw, ok := value["packages"]; ok {
		var packages map[string]json.RawMessage
		if json.Unmarshal(raw, &packages) != nil {
			return fmt.Errorf("OpenCode Backbone: invalid native package lock")
		}
		for key := range packages {
			if key != "" {
				return fmt.Errorf("OpenCode Backbone: locked native package code is not qualified")
			}
		}
	}
	return nil
}

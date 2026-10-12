package modelcatalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const openCodeConfigOutputLimit = modelConfigReadLimit

// The model service has a 30s discovery budget. An optional config lookup must
// fit after the existing 20s model command without blocking explicit choices.
const openCodeConfigTimeout = 5 * time.Second
const openCodeDefaultUnknown = "OpenCode configured default is unavailable; select an explicit OpenCode model when an effort is required"

var errOpenCodeConfigOutputLimit = errors.New("OpenCode configuration output exceeds its size bound")

// openCodeConfiguredModel uses the supported resolved-config surface in the
// catalog's exact cwd/environment. Unlike debug agent, debug config exposes
// cfg.model. It does not expose implicit recent/fallback model selection.
// Native CLI bootstrap may initialize config/plugins, as for models discovery;
// AO never requests a server, session, tool or inference to resolve this value.
func openCodeConfiguredModel(ctx context.Context, binary, workingDir string, env map[string]string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, openCodeConfigTimeout)
	defer cancel()
	cmd := modelCommand(runCtx, binary, []string{"debug", "config"}, workingDir, env)
	var output openCodeConfigOutput
	cmd.Stdout, cmd.Stderr = &output, io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if output.exceeded {
			return "", errOpenCodeConfigOutputLimit
		}
		if runCtx.Err() != nil {
			return "", errors.New("OpenCode resolved configuration lookup timed out")
		}
		// Config output may contain credentials despite native redaction. Neither
		// stdout, stderr nor an ExitError with captured output may reach a log.
		return "", errors.New("OpenCode resolved configuration lookup failed")
	}
	if output.exceeded {
		return "", errOpenCodeConfigOutputLimit
	}
	return parseOpenCodeConfiguredModel(output.Bytes())
}

func parseOpenCodeConfiguredModel(output []byte) (string, error) {
	if len(output) > openCodeConfigOutputLimit {
		return "", errOpenCodeConfigOutputLimit
	}
	if !bytes.HasPrefix(bytes.TrimSpace(output), []byte("{")) {
		return "", errors.New("OpenCode resolved configuration is not a JSON object")
	}
	var config struct {
		Model string `json:"model"`
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	if err := decoder.Decode(&config); err != nil {
		return "", errors.New("OpenCode resolved configuration is not a JSON object")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", errors.New("OpenCode resolved configuration contains extra output")
	}
	if config.Model == "" {
		return "", nil
	}
	provider, model, ok := strings.Cut(config.Model, "/")
	if !ok || provider == "" || model == "" || strings.TrimSpace(config.Model) != config.Model || !looksLikeModelID(config.Model) {
		return "", errors.New("OpenCode resolved configuration has no concrete provider/model selection")
	}
	return config.Model, nil
}

type openCodeConfigOutput struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (b *openCodeConfigOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *openCodeConfigOutput) Write(data []byte) (int, error) {
	remaining := openCodeConfigOutputLimit - b.buffer.Len()
	if len(data) > remaining {
		b.exceeded = true
		n, _ := b.buffer.Write(data[:remaining])
		return n, errOpenCodeConfigOutputLimit
	}
	return b.buffer.Write(data)
}

// openCodeConfigFingerprint hashes only known direct native configuration
// inputs. It does not duplicate native merge/default resolution or scan trees.
// Launch validation still requests a fresh project-scoped catalog.
func openCodeConfigFingerprint(ctx context.Context, workingDir string, env map[string]string) string {
	hash := sha256.New()
	value := func(key string) string {
		if v, ok := env[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
	for _, key := range []string{"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_CONTENT", "OPENCODE_DISABLE_PROJECT_CONFIG", "XDG_CONFIG_HOME", "HOME", "USERPROFILE"} {
		_, _ = hash.Write([]byte(key + "\x00" + value(key) + "\x00"))
	}
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	_, _ = hash.Write([]byte("cwd\x00" + workingDir + "\x00"))
	home := value("HOME")
	if home == "" {
		home = value("USERPROFILE")
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	configHome := value("XDG_CONFIG_HOME")
	if configHome == "" && home != "" {
		configHome = filepath.Join(home, ".config")
	}
	var files []string
	if configHome != "" {
		for _, name := range []string{"config.json", "opencode.json", "opencode.jsonc"} {
			files = append(files, filepath.Join(configHome, "opencode", name))
		}
	}
	if workingDir != "" {
		for _, name := range []string{"opencode.json", "opencode.jsonc"} {
			files = append(files, filepath.Join(workingDir, name), filepath.Join(workingDir, ".opencode", name))
		}
	}
	if path := value("OPENCODE_CONFIG"); path != "" {
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		files = append(files, path)
	}
	if dir := value("OPENCODE_CONFIG_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(workingDir, dir)
		}
		for _, name := range []string{"opencode.json", "opencode.jsonc"} {
			files = append(files, filepath.Join(dir, name))
		}
	}
	for _, path := range files {
		if ctx.Err() != nil {
			break
		}
		_, _ = hash.Write([]byte(path + "\x00"))
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			_, _ = hash.Write([]byte{0})
			continue
		}
		raw, err := readModelConfig(path)
		if err == nil {
			_, _ = hash.Write(raw)
		}
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("%x", hash.Sum(nil)[:8])
}

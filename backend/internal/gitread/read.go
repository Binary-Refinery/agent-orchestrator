// Package gitread reads facts from explicitly registered AO repositories.
package gitread

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

const outputLimit = 2 << 20

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > outputLimit {
		return 0, errors.New("git read output exceeds limit")
	}
	return b.Buffer.Write(data)
}

// Run is only for read operations on a registered project/workspace root. It
// retries ownership refusal once, trusting only that canonical root for this
// invocation. It never writes safe.directory to any persistent Git config.
// Both attempts share a deadline; provider diagnostics are never returned.
func Run(ctx context.Context, registeredRoot string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	base := []string{"--no-replace-objects", "--no-optional-locks", "-c", "core.fsmonitor=false"}
	output, stderr, err := run(ctx, registeredRoot, append(base, args...))
	if err == nil {
		return output, nil
	}
	var exit *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 128 ||
		!bytes.Contains(stderr, []byte("detected dubious ownership in repository")) {
		return nil, readError(ctx, err)
	}
	canonical, pathErr := filepath.Abs(registeredRoot)
	if pathErr == nil {
		canonical, pathErr = filepath.EvalSymlinks(canonical)
	}
	if pathErr != nil {
		return nil, readError(ctx, err)
	}
	// Ignore Git's suggested path in stderr: it is not an authorized root.
	retry := append([]string{"-c", "safe.directory=" + filepath.ToSlash(canonical)}, base...)
	output, _, err = run(ctx, registeredRoot, append(retry, args...))
	if err != nil {
		return nil, readError(ctx, err)
	}
	return output, nil
}

func run(ctx context.Context, dir string, args []string) ([]byte, []byte, error) {
	command := aoprocess.CommandContext(ctx, "git", args...)
	command.Dir = dir
	var stdout, stderr boundedOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func readError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("git read timed out or was cancelled: %w", ctx.Err())
	}
	return fmt.Errorf("git read failed: %w", err)
}

package gitread

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(output))
}

func repository(t *testing.T) (string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	root := t.TempDir()
	config := filepath.Join(root, "global.gitconfig")
	if err := os.WriteFile(config, []byte("# operator settings\n[user]\n\tname = Test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_TEST_ASSUME_DIFFERENT_OWNER", "0")
	repo := filepath.Join(root, "registered repo [1]")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	git(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	return repo, git(t, repo, "rev-parse", "HEAD"), config
}

func TestOwnershipRetryReadsRegisteredRootWithoutChangingGlobalTrust(t *testing.T) {
	repo, want, config := repository(t)
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_TEST_ASSUME_DIFFERENT_OWNER", "1")
	// This is Git's own ownership check, not a synthetic adapter error.
	refused := exec.Command("git", "rev-parse", "HEAD")
	refused.Dir = repo
	output, err := refused.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "detected dubious ownership") {
		t.Fatalf("expected real ownership refusal: %v", err)
	}
	got, err := Run(context.Background(), repo, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(got)) != want {
		t.Fatalf("registered HEAD read failed: %v", err)
	}
	after, err := os.ReadFile(config)
	if err != nil || string(before) != string(after) {
		t.Fatalf("persistent Git config changed: %v", err)
	}
	other := exec.Command("git", "rev-parse", "HEAD")
	other.Dir = repo
	if err := other.Run(); err == nil {
		t.Fatal("per-command retry unexpectedly granted persistent trust")
	}
}

func TestReadDoesNotExposeDiagnosticsOrTreatNonOwnershipFailureAsSuccess(t *testing.T) {
	repo, _, _ := repository(t)
	_, err := Run(context.Background(), repo, "rev-parse", "secret-token-that-is-not-a-ref")
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("failure must remain failure without raw diagnostics: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, repo, "rev-parse", "HEAD"); err == nil {
		t.Fatal("cancelled read succeeded")
	}
}

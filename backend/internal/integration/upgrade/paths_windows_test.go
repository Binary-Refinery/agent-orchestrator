//go:build upgrade && windows

package upgrade_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWindowsFixtureRootMatchesNativeLongPath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "long fixture directory name")
	must(t, os.Mkdir(base, 0o700))
	longBase, err := filepath.EvalSymlinks(base)
	must(t, err)
	input, err := syscall.UTF16PtrFromString(longBase)
	must(t, err)
	buffer := make([]uint16, len(longBase)+1)
	n, err := syscall.GetShortPathName(input, &buffer[0], uint32(len(buffer)))
	must(t, err)
	if n >= uint32(len(buffer)) {
		t.Fatal("short fixture path exceeds its long-path buffer")
	}
	shortBase := syscall.UTF16ToString(buffer[:n])
	if strings.EqualFold(shortBase, longBase) {
		t.Skip("fixture volume does not provide a distinct Windows 8.3 alias")
	}
	t.Setenv("TMP", shortBase)
	t.Setenv("TEMP", shortBase)
	// A child testing.T allocates its first TempDir after the environment change.
	t.Run("native workspace remains inside fixture", func(t *testing.T) {
		h := newHarness(t)
		executable, err := os.Executable()
		must(t, err)
		nativeRepo, err := filepath.EvalSymlinks(h.repo)
		must(t, err)
		cmd := exec.Command(executable, "-test.run=^TestWindowsFixtureNativeCWDHelper$")
		cmd.Dir = nativeRepo
		cmd.Env = append(h.env, "AO_FIXTURE_CWD_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native cwd helper: %v: %s", err, output)
		}
		nativeCWD := strings.TrimSpace(string(output))
		rel, err := filepath.Rel(h.root, nativeCWD)
		must(t, err)
		if rel != "repo" {
			t.Fatalf("short TEMP and long native cwd disagree: root=%q cwd=%q relative=%q", h.root, nativeCWD, rel)
		}
		// The normalization must not change which directory is owned by the test.
		rootInfo, err := os.Stat(h.root)
		must(t, err)
		nativeRootInfo, err := os.Stat(filepath.Dir(nativeCWD))
		must(t, err)
		if !os.SameFile(rootInfo, nativeRootInfo) {
			t.Fatal("normalized fixture root changed filesystem identity")
		}
	})
}

func TestWindowsFixtureNativeCWDHelper(t *testing.T) {
	if os.Getenv("AO_FIXTURE_CWD_HELPER") != "1" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(cwd)
	os.Exit(0)
}

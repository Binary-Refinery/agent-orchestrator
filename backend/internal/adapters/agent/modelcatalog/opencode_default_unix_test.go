//go:build linux || darwin

package modelcatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOpenCodeDefaultFingerprintDoesNotOpenNativeConfigFIFO(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.json")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = openCodeConfigFingerprint(context.Background(), root, probeEnvironment("", root))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		// Release a regressed blocking reader before failing the test.
		file, err := os.OpenFile(path, os.O_WRONLY, 0600)
		if err == nil {
			_ = file.Close()
		}
		t.Fatal("background native-config fingerprint blocked on a special file")
	}
}

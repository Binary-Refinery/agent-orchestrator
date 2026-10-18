package backbone

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This fixture was emitted by actual Copier and the installed Backbone guard,
// not by the Go receipt helper above. Its original Git objects preserve the
// producer revision and all canonical native blobs. No declared check is run.
func TestActualBackboneProducerReceipt(t *testing.T) {
	fixturePath := filepath.Join("testdata", "producer")
	bundle, err := os.ReadFile(filepath.Join(fixturePath, "workspace.bundle"))
	if err != nil {
		t.Fatal(err)
	}
	if hash(bundle) != "69cb616afcc14efc7c25a667a797d4e17a3b1ec6e4e26ea9df75ac896e7709ad" {
		t.Fatal("producer fixture bundle digest changed")
	}
	bundlePath, err := filepath.Abs(filepath.Join(fixturePath, "workspace.bundle"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "clone", "-q", "--no-checkout", bundlePath, "workspace")
	workspace := filepath.Join(root, "workspace")
	git(t, workspace, "-c", "core.autocrlf=false", "checkout", "-q", "--detach", "c77601c5fcd96188ddeca71b3cc9429a2d692a51")
	raw, err := os.ReadFile(filepath.Join(fixturePath, "context.json"))
	if err != nil {
		t.Fatal(err)
	}
	requestRaw, err := os.ReadFile(filepath.Join(fixturePath, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		TaskPacket string `json:"task_packet"`
	}
	if err := json.Unmarshal(requestRaw, &request); err != nil {
		t.Fatal(err)
	}
	companion, err := os.ReadFile(filepath.Join(workspace, ".engineering-backbone", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{SchemaVersion: "1", AdapterSHA256: hash(companion), Contexts: map[string]RoleBinding{"builder": {Role: "implementer", TaskPacket: request.TaskPacket, Context: raw}}}
	parsed, err := ValidateBinding(marshal(t, binding), []string{"builder"})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(context.Background(), workspace, parsed, "builder")
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.Context.Documents) != 33 || len(verified.NativeDocumentEvidence) != 13 || !strings.Contains(verified.Prompt, "Task Packet") {
		t.Fatal("producer context was not fully verified")
	}
	if verified.NativeDocumentEvidence[".opencode/skills/code-review/SKILL.md"] != "git-blob-at-approved-revision" {
		t.Fatal("nonselected producer skill not bound to actual tree")
	}
	if _, err := os.Stat(filepath.Join(workspace, "declared-check-executed")); !os.IsNotExist(err) {
		t.Fatal("data-only verification executed a declared check")
	}
}

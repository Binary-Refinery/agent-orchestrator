package backbone

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var canonicalSkills = []string{"task-intake", "implementation-plan", "bug-reproduction", "test-and-quality", "code-review", "finding-triage", "delta-review", "docs-traceability"}
var canonicalRoles = []string{"planner", "implementer", "reviewer", "quality-checker", "triage"}

// VerifyCanonicalSkills closes all native-discovered canonical skill and agent
// inputs. Selected documents have receipt hashes; nonselected documents are raw
// blobs at the already approved receipt commit. No checkout/filter/fetch runs.
func VerifyCanonicalSkills(ctx context.Context, workspace string, verified *VerifiedContext) error {
	if verified == nil || !revisionPattern.MatchString(verified.Context.Revision) {
		return fmt.Errorf("missing approved Backbone revision")
	}
	root, err := safeRoot(workspace)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(canonicalSkills)+len(canonicalRoles))
	for _, skill := range canonicalSkills {
		paths = append(paths, ".opencode/skills/"+skill+"/SKILL.md")
	}
	for _, role := range canonicalRoles {
		paths = append(paths, ".opencode/agents/backbone-"+role+".md")
	}
	if verified.NativeDocumentEvidence == nil {
		verified.NativeDocumentEvidence = map[string]string{}
	}
	for _, path := range paths {
		if _, ok := verified.Documents[path]; ok {
			verified.NativeDocumentEvidence[path] = "receipt-sha256"
			continue
		}
		data, err := readDocument(root, path)
		if err != nil {
			return err
		}
		blob, err := gitRead(ctx, root, MaxDocumentBytes, "cat-file", "blob", verified.Context.Revision+":"+path)
		if err != nil || !bytes.Equal(data, blob) {
			return fmt.Errorf("native Backbone document differs from approved Git blob: %s", path)
		}
		verified.NativeDocumentEvidence[path] = "git-blob-at-approved-revision"
	}
	head, err := gitRead(ctx, root, 128, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || strings.TrimSpace(string(head)) != verified.Context.Revision {
		return fmt.Errorf("backbone HEAD changed during context verification")
	}
	return nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.Len() {
		return 0, fmt.Errorf("git output exceeds Backbone limit")
	}
	return b.Buffer.Write(data)
}

// All inherited Git overrides are removed, including repository/object/config
// and alternate-index overrides. Local object reads cannot fetch, replace
// objects, apply smudge/textconv filters, invoke a pager or consult global config.
func gitRead(ctx context.Context, workspace string, limit int, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := make([]string, 0, 6+len(arguments))
	args = append(args, "--no-replace-objects", "--no-lazy-fetch", "--no-optional-locks", "--no-pager", "-C", workspace)
	args = append(args, arguments...)
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = workspace
	command.Env = gitEnvironment()
	output := &boundedBuffer{limit: limit}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("bounded Backbone Git read failed")
	}
	return output.Bytes(), nil
}

func gitEnvironment() []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			environment = append(environment, entry)
		}
	}
	return append(environment, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ATTR_NOSYSTEM=1")
}

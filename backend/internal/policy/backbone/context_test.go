package backbone

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func write(t *testing.T, root, name string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"--no-replace-objects", "--no-lazy-fetch", "-c", "core.hooksPath=" + os.DevNull, "-c", "init.templateDir=", "-C", root}, args...)...)
	command.Env = append(gitEnvironment(), "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func digestReceipt(t *testing.T, receipt ContextReceipt) []byte {
	t.Helper()
	raw := marshal(t, receipt)
	value, err := parseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	object := value.(map[string]any)
	delete(object, "context_sha256")
	receipt.ContextSHA256 = hash(canonicalJSON(object))
	return marshal(t, receipt)
}

type fixture struct {
	root    string
	binding *Binding
	receipt ContextReceipt
}

func makeFixture(t *testing.T) fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	companion := []byte(`{"schema_version":"1","adapter_version":"1.0.0","enabled":true,"opencode_version":"1.19.0","native_config":"opencode.json"}`)
	files := map[string][]byte{
		"AGENTS.md":                                 []byte("# Repository instructions\nFollow the Task Packet.\n"),
		".engineering-backbone/opencode.json":       companion,
		"opencode.json":                             []byte(`{"$schema":"https://opencode.ai/config.json","instructions":["AGENTS.md","docs/opencode/workflow.md"],"permission":{"*":"deny"}}`),
		"docs/opencode/workflow.md":                 []byte("# OpenCode workflow\nRead the complete context.\n"),
		"docs/opencode/opencode-runner-contract.md": []byte("# Runner contract\nA receipt is not proof of native loading.\n"),
		"docs/guide.md":                             []byte("# Guide\n## Valid heading\nFull file content.\n"),
		"plans/task.json":                           []byte(`{"schema_version":"1","id":"TEST-1","title":"Fixture","objective":"Verify context","role":"implementer","risk":"high","repository":{"base_branch":"main","base_revision":null,"head_revision":null},"approved_sources":["operator"],"scope":{"include":["src/**"],"exclude":[]},"acceptance":[{"id":"AC-1","description":"Verify","verification":"automatic","status":"pending","evidence":[]}],"validation":{"commands":[],"manual_evidence":[]},"execution_budget":{"max_iterations":2},"stop_conditions":["failed check"],"forbidden_actions":["publish"],"status":"active"}`),
	}
	for _, skill := range canonicalSkills {
		files[".opencode/skills/"+skill+"/SKILL.md"] = []byte("---\nname: " + skill + "\ndescription: Fixture\n---\n# Procedure\nApply approved procedure.\n")
	}
	for _, role := range canonicalRoles {
		files[".opencode/agents/backbone-"+role+".md"] = []byte("---\ndescription: Fixture\nmode: primary\npermission:\n  '*': deny\n---\n# Role\nFollow task.\n")
	}
	for name, data := range files {
		write(t, root, name, data)
	}
	git(t, root, "init", "-q")
	git(t, root, "add", ".")
	git(t, root, "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	revision := git(t, root, "rev-parse", "HEAD")
	receipt := ContextReceipt{SchemaVersion: "1", Claim: "structural-context-only", Revision: revision, TaskID: "TEST-1", Role: "implementer", RequestSHA256: strings.Repeat("a", 64), NavigationSHA256: strings.Repeat("b", 64), QualityProfileSHA256: strings.Repeat("c", 64), BackboneRef: "fixture", QualityCatalogVersion: "1", TechnologyCatalogVersion: "1", TechnologyCatalogSHA256: strings.Repeat("d", 64), Components: []string{"core"}, TechnologyProfiles: []VersionedID{}, TechnologyRules: []VersionedID{}, TechnologyCheckBindings: map[string][]CheckBinding{}, QualityProfiles: []VersionedID{}, QualityRules: []VersionedID{}, QualityCheckBindings: []CheckBinding{}, Checks: []Check{}, Documents: []Document{}, RuntimeLoading: "unverified"}
	selected := []string{"AGENTS.md", "opencode.json", ".engineering-backbone/opencode.json", "docs/opencode/workflow.md", "docs/opencode/opencode-runner-contract.md", "plans/task.json", ".opencode/agents/backbone-implementer.md", "docs/guide.md#valid-heading"}
	for _, skill := range roleSkills["implementer"] {
		selected = append(selected, ".opencode/skills/"+skill+"/SKILL.md")
	}
	for _, path := range selected {
		name, _, _ := relative(path, true)
		receipt.Documents = append(receipt.Documents, Document{Path: path, SHA256: hash(files[name])})
	}
	binding := &Binding{SchemaVersion: "1", AdapterSHA256: hash(companion), Contexts: map[string]RoleBinding{"builder": {Role: "implementer", TaskPacket: "plans/task.json", Context: digestReceipt(t, receipt)}}}
	return fixture{root: root, binding: binding, receipt: receipt}
}

func TestVerifyBoundContextAndNativeClosure(t *testing.T) {
	f := makeFixture(t)
	binding, err := ValidateBinding(marshal(t, f.binding), []string{"builder"})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(context.Background(), f.root, binding, "builder")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verified.Prompt, "Full file content.") || !strings.Contains(verified.Prompt, "Follow the Task Packet.") {
		t.Fatal("full verified document bytes absent from prompt")
	}
	if len(verified.NativeDocumentEvidence) != 13 || verified.NativeDocumentEvidence[".opencode/skills/code-review/SKILL.md"] != "git-blob-at-approved-revision" || verified.NativeDocumentEvidence[".opencode/agents/backbone-implementer.md"] != "receipt-sha256" {
		t.Fatalf("native closure evidence: %v", verified.NativeDocumentEvidence)
	}
	if _, ok := verified.Documents[".opencode/skills/code-review/SKILL.md"]; ok {
		t.Fatal("nonselected skill incorrectly claimed as receipt loaded")
	}
	if _, err := ValidateBinding(marshal(t, f.binding), []string{"other"}); err == nil {
		t.Fatal("unknown enclosing Instructor role accepted")
	}
	if _, err := ValidateBinding(marshal(t, f.binding), nil); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsChangedContextInputs(t *testing.T) {
	for _, test := range []struct{ name, path string }{
		{"selected role", ".opencode/agents/backbone-implementer.md"},
		{"other role", ".opencode/agents/backbone-reviewer.md"},
		{"selected skill", ".opencode/skills/task-intake/SKILL.md"},
		{"other skill", ".opencode/skills/code-review/SKILL.md"},
		{"anchored full file", "docs/guide.md"},
		{"explicit packet", "plans/task.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := makeFixture(t)
			path := filepath.Join(f.root, filepath.FromSlash(test.path))
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			write(t, f.root, test.path, append(original, []byte("\nchanged\n")...))
			if _, err := Verify(context.Background(), f.root, f.binding, "builder"); err == nil {
				t.Fatal("changed input accepted")
			}
		})
	}
}

func TestVerifyRejectsMissingMandatoryDocumentsAndStaleHead(t *testing.T) {
	for _, path := range []string{"plans/task.json", ".opencode/skills/docs-traceability/SKILL.md", ".opencode/agents/backbone-implementer.md", "AGENTS.md", "docs/opencode/opencode-runner-contract.md"} {
		t.Run(path, func(t *testing.T) {
			f := makeFixture(t)
			docs := []Document{}
			for _, doc := range f.receipt.Documents {
				if doc.Path != path {
					docs = append(docs, doc)
				}
			}
			f.receipt.Documents = docs
			bound := f.binding.Contexts["builder"]
			bound.Context = digestReceipt(t, f.receipt)
			f.binding.Contexts["builder"] = bound
			if _, err := Verify(context.Background(), f.root, f.binding, "builder"); err == nil {
				t.Fatal("missing mandatory document accepted")
			}
		})
	}
	t.Run("head", func(t *testing.T) {
		f := makeFixture(t)
		git(t, f.root, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "new head")
		if _, err := Verify(context.Background(), f.root, f.binding, "builder"); err == nil {
			t.Fatal("stale HEAD accepted")
		}
	})
}

func TestStrictJSONRejectsAmbiguityAndNulls(t *testing.T) {
	type nested struct {
		Name     string   `json:"name"`
		Flag     bool     `json:"flag"`
		List     []string `json:"list"`
		Optional string   `json:"optional,omitempty"`
	}
	for _, raw := range []string{
		`{"name":"x","name":"y","flag":true,"list":[]}`,
		`{"Name":"x","flag":true,"list":[]}`,
		`{"name":"x","flag":true,"list":[],"extra":1}`,
		`{"name":"x","flag":true}`,
		`{"name":null,"flag":true,"list":[]}`,
		`{"name":"x","flag":null,"list":[]}`,
		`{"name":"x","flag":true,"list":null}`,
		`{"name":"x","flag":true,"list":[null]}`,
		`{"name":"x","flag":true,"list":[],"optional":null}`,
		`{"name":"\ud800","flag":true,"list":[]}`,
		`{"name":"x","flag":true,"list":[]} {}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var value nested
			if err := DecodeStrict([]byte(raw), &value); err == nil {
				t.Fatal("ambiguous JSON accepted")
			}
		})
	}
	var value nested
	if err := DecodeStrict([]byte(`{"name":"x","flag":true,"list":[]}`), &value); err != nil {
		t.Fatal(err)
	}
	var raw json.RawMessage
	if err := DecodeStrict([]byte(strings.Repeat("[", 34)+"0"+strings.Repeat("]", 34)), &raw); err == nil {
		t.Fatal("excessive nesting accepted")
	}
	if err := DecodeStrict(bytes.Repeat([]byte(" "), MaxJSONBytes+1), &raw); err == nil {
		t.Fatal("oversized JSON accepted")
	}
}

func TestPacketNullableRevisionsAndFrozenGrammar(t *testing.T) {
	f := makeFixture(t)
	data, err := os.ReadFile(filepath.Join(f.root, "plans/task.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePacket(data, &f.receipt); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ old, new string }{
		{`"role":"implementer"`, `"role":"reviewer"`},
		{`"status":"active"`, `"status":"ready_for_review"`},
		{`"head_revision":null`, `"head_revision":"` + strings.Repeat("1", 40) + `"`},
		{`"max_iterations":2`, `"max_iterations":null`},
		{`"evidence":[]`, `"evidence":null`},
		{`"base_branch":"main"`, `"base_branch":"x/../y"`},
	} {
		t.Run(change.new, func(t *testing.T) {
			changed := strings.Replace(string(data), change.old, change.new, 1)
			receipt := f.receipt
			if strings.Contains(change.new, "reviewer") {
				receipt.Role = "reviewer"
			}
			if err := validatePacket([]byte(changed), &receipt); err == nil {
				t.Fatal("invalid packet accepted")
			}
		})
	}
}

func TestPortablePathsAndFences(t *testing.T) {
	for _, path := range []string{"../outside", "/absolute", "a//b", "a/./b", "a\\b", "C:/a", "con.txt", "a.", "a ", "a#", "a#Mixed", "a#x#y", "a\x00b"} {
		if _, _, err := relative(path, true); err == nil {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	for _, test := range []struct {
		content, anchor string
		want            bool
	}{
		{"# Name\n# Name\n", "name-1", true},
		{"````\n```\n# Hidden\n````\n# Visible\n", "hidden", false},
		{"```\n~~~\n# Hidden\n```\n# Visible\n", "hidden", false},
		{"~~~\n~~~ trailing\n# Hidden\n~~~\n# Visible\n", "visible", true},
		{"    # Indented\n", "indented", false},
		{"```lang\n# Hidden\n```\n", "hidden", false},
	} {
		if got := hasHeading([]byte(test.content), test.anchor); got != test.want {
			t.Fatalf("heading %q got %v want %v", test.anchor, got, test.want)
		}
	}
}

func TestVerifyRejectsSymlinksCaseAmbiguityAndOversize(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		f := makeFixture(t)
		path := filepath.Join(f.root, "AGENTS.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "AGENTS.md")
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := Verify(context.Background(), f.root, f.binding, "builder"); err == nil {
			t.Fatal("symlink accepted")
		}
	})
	t.Run("case", func(t *testing.T) {
		f := makeFixture(t)
		if err := os.Rename(filepath.Join(f.root, "AGENTS.md"), filepath.Join(f.root, "agents.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(context.Background(), f.root, f.binding, "builder"); err == nil {
			t.Fatal("case mismatch accepted")
		}
	})
	t.Run("size", func(t *testing.T) {
		f := makeFixture(t)
		write(t, f.root, "AGENTS.md", bytes.Repeat([]byte("a"), MaxDocumentBytes+1))
		if _, err := Verify(context.Background(), f.root, f.binding, "builder"); err == nil {
			t.Fatal("oversized document accepted")
		}
	})
}

func TestCanonicalGitReadIgnoresReplacementAndInheritedRepositoryOverrides(t *testing.T) {
	f := makeFixture(t)
	path := ".opencode/skills/code-review/SKILL.md"
	original := git(t, f.root, "rev-parse", f.receipt.Revision+":"+path)
	write(t, f.root, path, []byte("# Locally changed nonselected procedure\n"))
	replacement := git(t, f.root, "hash-object", "-w", path)
	git(t, f.root, "replace", original, replacement)
	if _, err := Verify(context.Background(), f.root, f.binding, "builder"); err == nil {
		t.Fatal("replace object hid changed native file")
	}
	// Restore original bytes; the replacement must also not cause a false stale
	// result when an operator has a benign local replacement ref.
	command := exec.Command("git", "--no-replace-objects", "-C", f.root, "cat-file", "blob", original)
	command.Env = gitEnvironment()
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	write(t, f.root, path, data)
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "wrong"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	t.Setenv("GIT_OBJECT_DIRECTORY", t.TempDir())
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", t.TempDir())
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.bare")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	if _, err := Verify(context.Background(), f.root, f.binding, "builder"); err != nil {
		t.Fatalf("inherited Git overrides affected verification: %v", err)
	}
}

func TestCanonicalDigestUnicodeMatchesProducerEncoding(t *testing.T) {
	value, err := parseJSON([]byte(`{"z":"<tag>&é\u2028\n","a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(canonicalJSON(value)); got != "{\"a\":1,\"z\":\"<tag>&é\u2028\\n\"}" {
		t.Fatalf("unexpected canonical JSON: %q", got)
	}
}

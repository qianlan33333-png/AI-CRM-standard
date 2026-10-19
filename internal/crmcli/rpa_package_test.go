package crmcli

import (
	"bytes"
	"encoding/json"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRPAPackageLocalMaterialDigestAndDirectoryEscape(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "1.jpg"), []byte("local-image"), 0600); e != nil {
		t.Fatal(e)
	}
	doc := p.TaskDocument{Schema: p.TaskPackageSchema, PlanID: 16, PlanRevision: 7, Groups: []string{"chat-a"}, Messages: []p.SequenceMessage{{ID: "text", Kind: "text", Text: "line one\nline two"}, {ID: "image", Kind: "image", RelativePath: "1.jpg"}}, Prose: "# task\n"}
	md, e := p.RenderTaskMarkdown(doc)
	if e != nil {
		t.Fatal(e)
	}
	body, saved, e := PrepareTaskPackage([]byte(md), root)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(body), root) || strings.Contains(string(body), "local-image") {
		t.Fatal("file bytes or absolute directory uploaded")
	}
	parsed, e := p.ParseTaskMarkdown(string(saved))
	if e != nil || len(parsed.Messages[1].SHA256) != 64 || parsed.Messages[1].ByteCount != 11 {
		t.Fatalf("digest was not persisted: %v", e)
	}
	if _, _, e = PrepareTaskPackage(saved, root); e != nil {
		t.Fatal("unchanged retry", e)
	}
	os.WriteFile(filepath.Join(root, "1.jpg"), []byte("changed"), 0600)
	if _, _, e = PrepareTaskPackage(saved, root); e == nil {
		t.Fatal("changed file accepted")
	}
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("private"), 0600)
	os.Symlink(outside, filepath.Join(root, "escape"))
	for _, name := range []string{"escape", "../outside", "/tmp/absolute"} {
		doc.Messages[1].RelativePath = name
		md, _ = p.RenderTaskMarkdown(doc)
		if _, _, e = PrepareTaskPackage([]byte(md), root); e == nil {
			t.Fatal("out of directory material accepted")
		}
	}
	protected := filepath.Join(t.TempDir(), "original.md")
	os.WriteFile(protected, []byte("preserve original"), 0600)
	linked := filepath.Join(root, "task-link.md")
	os.Symlink(protected, linked)
	if saveTaskMarkdown(linked, saved) == nil {
		t.Fatal("task save overwrote a symlink target")
	}
	original, _ := os.ReadFile(protected)
	if string(original) != "preserve original" {
		t.Fatal("task save changed an unrelated file")
	}
	if _, e = p.ParseTaskMarkdown("---\nschema: aicrm.group-sequence.v1\nschema: other\n---\n"); e == nil {
		t.Fatal("duplicate frontmatter key accepted")
	}
}

func TestRPAPackageFocusedHelpAndSchema(t *testing.T) {
	var output bytes.Buffer
	if code := Run([]string{"group-ops", "rpa-package", "save", "--help"}, strings.NewReader(""), &output, &output); code != 0 {
		t.Fatal(code, output.String())
	}
	help := output.String()
	if !strings.Contains(help, "--input") || !strings.Contains(help, "--dry-run") || strings.Contains(help, "--upload") || strings.Contains(help, "--wait-timeout") {
		t.Fatal("unfocused help", help)
	}
	output.Reset()
	if code := Run([]string{"schema", "get", "admin.saveRpaPackage"}, strings.NewReader(""), &output, &output); code != 0 {
		t.Fatal(code, output.String())
	}
	var view struct {
		Definitions map[string]any `json:"definitions"`
	}
	if err := json.Unmarshal(output.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Definitions) > 5 {
		t.Fatalf("RPA schema contains unrelated definitions: %d", len(view.Definitions))
	}
	refs := referencedDefinitions(map[string]any{"$ref": "#/components/schemas/A"}, map[string]any{"A": map[string]any{"$ref": "#/components/schemas/B"}, "B": map[string]any{"$ref": "#/components/schemas/A"}, "unused": map[string]any{}})
	if len(refs) != 2 {
		t.Fatal("nested/cyclic required schemas lost", refs)
	}
}

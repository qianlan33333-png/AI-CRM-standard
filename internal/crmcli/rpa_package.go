package crmcli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

// PrepareTaskPackage reads only explicitly referenced local materials. No
// attachment bytes, absolute paths or executable instructions go to the server.
func PrepareTaskPackage(markdown []byte, directory string) ([]byte, []byte, error) {
	doc, err := p.ParseTaskMarkdown(string(markdown))
	if err != nil {
		return nil, nil, fail("validation", "invalid task Markdown; consult docs/cli/rpa-packages.md", 2)
	}
	root, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, nil, fail("validation", "task directory unavailable", 2)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	for index, m := range doc.Messages {
		if m.Kind == "text" {
			continue
		}
		if (m.Kind != "image" && m.Kind != "file") || m.RelativePath == "" || filepath.IsAbs(m.RelativePath) || strings.Contains(m.RelativePath, "\\") {
			return nil, nil, fail("validation", "unsupported local material reference", 2)
		}
		file, err := filepath.EvalSymlinks(filepath.Join(root, m.RelativePath))
		if err != nil {
			return nil, nil, fail("validation", "referenced material unavailable", 2)
		}
		rel, err := filepath.Rel(root, file)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, nil, fail("validation", "material escapes the task directory", 2)
		}
		info, err := os.Stat(file)
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 {
			return nil, nil, fail("validation", "material must be a nonempty regular file", 2)
		}
		bytes, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fail("validation", "material could not be read", 2)
		}
		digest := sha256.Sum256(bytes)
		hash := hex.EncodeToString(digest[:])
		if (m.SHA256 != "" && m.SHA256 != hash) || (m.ByteCount != 0 && m.ByteCount != int64(len(bytes))) {
			return nil, nil, fail("validation", "material differs from its saved task digest", 2)
		}
		doc.Messages[index].SHA256 = hash
		doc.Messages[index].ByteCount = int64(len(bytes))
	}
	canonical, err := p.RenderTaskMarkdown(doc)
	if err != nil {
		return nil, nil, err
	}
	body, err := json.Marshal(map[string]string{"markdown": canonical})
	return body, []byte(canonical), err
}

func saveTaskMarkdown(file string, canonical []byte) error {
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() {
		return fail("validation", "task Markdown must be a regular file", 2)
	}
	temporary, err := os.CreateTemp(filepath.Dir(file), ".rpa-task-*")
	if err != nil {
		return fail("validation", "task Markdown could not be saved locally", 2)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(info.Mode().Perm()); err == nil {
		_, err = temporary.Write(canonical)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, file)
	}
	if err != nil {
		return fail("validation", "task Markdown could not be saved locally", 2)
	}
	return nil
}

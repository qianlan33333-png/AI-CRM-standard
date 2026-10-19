package port

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const TaskPackageSchema = "aicrm.group-sequence.v1"

// TaskDocument is declarative data. Markdown prose is never executable.
type TaskDocument struct {
	Schema       string            `yaml:"schema"`
	PlanID       int64             `yaml:"plan_id"`
	PlanRevision int64             `yaml:"plan_revision"`
	Groups       []string          `yaml:"groups"`
	Messages     []SequenceMessage `yaml:"messages"`
	Prose        string            `yaml:"-"`
}

func ParseTaskMarkdown(markdown string) (out TaskDocument, err error) {
	invalid := errors.New("invalid task Markdown frontmatter")
	if !strings.HasPrefix(markdown, "---\n") || strings.ContainsRune(markdown, 0) {
		return out, invalid
	}
	header, prose, ok := strings.Cut(markdown[4:], "\n---\n")
	if !ok {
		return out, invalid
	}
	d := yaml.NewDecoder(strings.NewReader(header))
	d.KnownFields(true)
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF || out.Schema != TaskPackageSchema || out.PlanID < 1 || out.PlanRevision < 1 || len(out.Groups) == 0 || len(out.Messages) == 0 {
		return TaskDocument{}, invalid
	}
	out.Prose = prose
	return out, nil
}

func RenderTaskMarkdown(document TaskDocument) (string, error) {
	var header strings.Builder
	e := yaml.NewEncoder(&header)
	e.SetIndent(2)
	if err := e.Encode(document); err != nil {
		return "", err
	}
	return "---\n" + header.String() + "---\n" + document.Prose, nil
}

type TaskPackage struct {
	ID        int64         `json:"package_id,string"`
	PlanID    int64         `json:"plan_id,string"`
	Markdown  string        `json:"markdown"`
	SHA256    string        `json:"sha256"`
	CreatedAt time.Time     `json:"created_at"`
	Run       NavigationRun `json:"run"`
}
type TaskPackageView struct {
	Package *TaskPackage `json:"package"`
}
type TaskPackages interface {
	SaveTaskPackage(context.Context, SidebarActor, string, string) (TaskPackage, error)
	ReadTaskPackage(context.Context, SidebarActor, int64) (TaskPackageView, error)
}

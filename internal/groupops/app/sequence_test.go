package app

import (
	"context"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	"strings"
	"testing"
)

type sequenceIdentityStore struct {
	NavigationStore
	readRun, readStaff int64
}

func (s *sequenceIdentityStore) ReadSequencePackageIdentity(_ context.Context, run, staff int64) (int64, string, error) {
	s.readRun, s.readStaff = run, staff
	return 42, strings.Repeat("c", 64), nil
}
func TestSequenceProjectionCarriesFrozenManifestAndExactPackage(t *testing.T) {
	store := &sequenceIdentityStore{}
	s := &NavigationService{Store: store}
	run := p.NavigationRun{ID: 10, StaffID: 19, PlanName: "计划", Targets: []p.NavigationTarget{{ChatID: "a"}, {ChatID: "b"}}, Sequence: &p.SequenceExecution{Messages: []p.SequenceMessage{{ID: "first", Kind: "text", Text: "文字"}, {ID: "last", Kind: "file", RelativePath: "sample.pdf", SHA256: strings.Repeat("a", 64), ByteCount: 20}}}}
	view, err := s.projectSequence(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	step := view.Step
	if step.PackageID != 42 || step.PackageSHA256 != strings.Repeat("c", 64) || step.GroupCount != 2 || step.MessageCount != 2 || len(step.Materials) != 1 || step.Materials[0].ID != "last" || step.Message.Kind != "text" || store.readRun != 10 || store.readStaff != 19 {
		t.Fatalf("incomplete immutable projection: %+v", step)
	}
	// Projection must retain the full manifest after cursor advancement/completion.
	run.Cursor = 2
	run.State = "completed"
	run.Sequence.MessageIndex = 0
	view, err = s.projectSequence(context.Background(), run)
	if err != nil || view.Step.State != "completed" || len(view.Step.Materials) != 1 {
		t.Fatalf("terminal projection: %+v %v", view, err)
	}
}

func TestRPASequenceMaterialScopeAndMessageIdentityValidation(t *testing.T) {
	valid := []p.SequenceMessage{{ID: "text", Kind: "text", Text: "原样文字\n第二行"}, {ID: "image", Kind: "image", RelativePath: "活动/1.jpg", SHA256: strings.Repeat("a", 64), ByteCount: 10}, {ID: "file", Kind: "file", RelativePath: "验收.docx", SHA256: strings.Repeat("b", 64), ByteCount: 20}}
	if e := validateMessages(valid); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"../secret", "/private/file", "a/../../secret", "a//file", "a/./file", "a\\file"} {
		bad := append([]p.SequenceMessage{}, valid...)
		bad[1].RelativePath = path
		if validateMessages(bad) == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
	for _, edit := range []func([]p.SequenceMessage){func(m []p.SequenceMessage) { m[1].ID = m[0].ID }, func(m []p.SequenceMessage) { m[1].SHA256 = "invalid" }, func(m []p.SequenceMessage) { m[2].ByteCount = 0 }, func(m []p.SequenceMessage) { m[0].Text = " " }, func(m []p.SequenceMessage) { m[0].RelativePath = "file" }, func(m []p.SequenceMessage) { m[0].Kind = "sdk_send" }} {
		bad := append([]p.SequenceMessage{}, valid...)
		edit(bad)
		if validateMessages(bad) == nil {
			t.Fatal("accepted invalid message")
		}
	}
}

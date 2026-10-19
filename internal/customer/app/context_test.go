package app

import (
	customerport "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/port"
	"testing"
	"time"
)

func TestContextHistoryDedupLateArrivalAndScope(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	v := customerport.CustomerContext{Scope: customerport.ContextScope{CustomerID: 1, StaffID: 2}, ThroughAt: at.Add(time.Hour), SourceCount: 3, Evidence: []customerport.ContextEvidence{{Ref: "chat:2", Kind: "chat", Role: "customer", Text: "现在暂停创业", OccurredAt: at.Add(time.Minute)}, {Ref: "chat:1", Kind: "chat", Role: "customer", Text: "想创业", OccurredAt: at}, {Ref: "chat:1", Kind: "chat", Role: "customer", Text: "想创业", OccurredAt: at}}}
	a, err := NormalizeContext(v)
	if err != nil || len(a.Evidence) != 2 || a.Evidence[0].Ref != "chat:1" {
		t.Fatalf("normalization=%+v err=%v", a, err)
	}
	v.Evidence = v.Evidence[:2]
	v.Evidence = append(v.Evidence, customerport.ContextEvidence{Ref: "chat:late", Kind: "chat", Role: "customer", Text: "补录的历史", OccurredAt: at.Add(-time.Hour)})
	b, _ := NormalizeContext(v)
	if a.SourceVersion == b.SourceVersion || b.Evidence[0].Ref != "chat:late" {
		t.Fatal("late arrival lost")
	}
	v.Scope.StaffID = 3
	c, _ := NormalizeContext(v)
	if c.SourceVersion == b.SourceVersion {
		t.Fatal("cross-staff cache collision")
	}
}
func TestContextAnalysisRejectsFabricatedOrStaffOnlyEvidence(t *testing.T) {
	es := []customerport.ContextEvidence{{Ref: "chat:customer", Role: "customer"}, {Ref: "chat:staff", Role: "staff"}}
	for _, ref := range []string{"missing", "chat:staff"} {
		if ValidateContextAnalysis(customerport.ContextAnalysis{Summary: "摘要", SummaryEvidenceRefs: []string{"chat:customer"}, Needs: []customerport.ContextStatement{{Text: "需求", EvidenceRefs: []string{ref}}}}, es) == nil {
			t.Fatalf("accepted %s", ref)
		}
	}
	if ValidateContextAnalysis(customerport.ContextAnalysis{Summary: "摘要", SummaryEvidenceRefs: []string{"chat:customer"}, Needs: []customerport.ContextStatement{{Text: "需求", EvidenceRefs: []string{"chat:customer"}}}}, es) != nil {
		t.Fatal("valid sourced analysis rejected")
	}
}

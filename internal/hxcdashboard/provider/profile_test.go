package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestProfileProjectionKeepsCompleteRepeatedBackgroundAndOnlyDisplayFields(t *testing.T) {
	text := "2026年4月已经营写作、个人IP和AI等课程、训练营及年度社群，训练营常复用录播课，由本人答疑、助教点评辅导。" + strings.Repeat("完整明细。", 1000)
	raw, _ := json.Marshal(map[string]any{"user_understanding": map[string]any{
		"result":     map[string]any{"backgrounds": []any{map[string]any{"text": text, "evidence_keys": []string{"source"}}, map[string]any{"text": "另一段经历", "evidence_keys": []string{"source"}}, map[string]any{"text": "没有依据的内部草稿", "evidence_keys": []string{"missing"}}}, "core_insights": []any{map[string]any{"text": "理解参考", "evidence_keys": []string{"source"}}}, "focus_candidates": []any{map[string]any{"text": "主线候选", "evidence_keys": []string{"source"}}}, "support_history": []any{map[string]any{"text": "过去建议与反馈", "evidence_keys": []string{"source"}}}},
		"references": map[string]any{"source": map[string]any{"message_id": "private-ref", "expressed_at": "2026-04-17T01:10:00+08:00"}},
	}})
	result, err := projectProfile(profileSource{Identity: "创业者"}, raw)
	if err != nil || len(result.Background) != 5 || result.Background[0].Value != text || result.Background[1].Label != "背景与经历" {
		t.Fatalf("projection lost full repeated fields: %v", err)
	}
	b, _ := json.Marshal(result.Background)
	if strings.Contains(string(b), "private-ref") || strings.Contains(string(b), "expressed_at") || strings.Contains(string(b), "内部草稿") {
		t.Fatal("display projection exported source metadata or unreferenced draft")
	}
	var display []map[string]any
	if json.Unmarshal(b, &display) != nil {
		t.Fatal("display fields malformed")
	}
	for _, row := range display {
		if len(row) != 3 {
			t.Fatal("display fields must be key/label/value only")
		}
	}
}

func TestProfileProjectionRejectsMalformedSourceAndDoesNotNarrateMetadata(t *testing.T) {
	if _, err := projectProfile(profileSource{}, []byte(`{"user_understanding":`)); err == nil {
		t.Fatal("malformed source accepted")
	}
	var value any
	_ = json.Unmarshal([]byte(`{"items":[{"title":"用户需要的支持","source_ref":"private"},{"summary":"完整的第二条需求","updated_at":"secret-meta"}],"state":"inferred","source_ref":"private"}`), &value)
	if got := readableProfileValue(value); got != "用户需要的支持；完整的第二条需求" {
		t.Fatalf("metadata leaked or details dropped: %q", got)
	}
	var unavailable *MySQL
	if _, err := unavailable.ReadCustomerProfile(context.Background(), ""); err == nil {
		t.Fatal("unconfigured source accepted")
	}
}

func TestProfileDiagnosisUsesStoredResultsOnlyAndExcludesEvidenceQuestions(t *testing.T) {
	raw := []byte(`{"recent_session_understanding":{"summary":"已有互动提炼完整摘要"},"user_understanding":{"result":{"understanding":"已有理解","current_guidance":{"priority":"已有当前主需求","support_mode":"已有沟通姿态","evidence_keys":["s"],"natural_question":"不得展示的下一问"}},"references":{"s":{"message_id":"private-ref","expressed_at":"2026-04-17"}}},"relationship_stage":"continuous_support","check_question":"不得展示的缺口"}`)
	result, err := projectProfile(profileSource{Stage: "已验证后端转化"}, raw)
	if err != nil || len(result.Diagnosis) != 5 {
		t.Fatalf("stored diagnosis unavailable: %v", err)
	}
	if result.Diagnosis[0].Value != "已有互动提炼完整摘要" || result.Diagnosis[1].Value != "已有当前主需求" || result.Diagnosis[2].Value != "已验证后端转化" || result.Diagnosis[3].Value != "待确认" || result.Diagnosis[4].Value != "已有沟通姿态" {
		t.Fatal("stored fields changed or relationship phase was fabricated into quality")
	}
	b, _ := json.Marshal(result.Diagnosis)
	if strings.Contains(string(b), "private-ref") || strings.Contains(string(b), "下一问") || strings.Contains(string(b), "关键证据") || strings.Contains(string(b), "缺口") {
		t.Fatal("excluded fields or metadata leaked")
	}
	missing, err := projectProfile(profileSource{}, []byte(`{"user_understanding":{"result":{"understanding":"未引用草稿","current_guidance":{"priority":"未引用需求"}}}}`))
	if err != nil || missing.Diagnosis[0].Value != "待确认" || missing.Diagnosis[1].Value != "待确认" {
		t.Fatal("unreferenced diagnosis treated as a fact")
	}
}

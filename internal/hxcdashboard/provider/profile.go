package provider

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qianlan33333-png/AI-CRM-standard/internal/hxcdashboard/port"
)

var ErrProfileUnavailable = errors.New("HXC profile unavailable")

type profileSource struct {
	Identity      string `json:"identity"`
	Industry      string `json:"industry"`
	Income        string `json:"income"`
	Business      string `json:"business"`
	Stage         string `json:"stage"`
	Focus         string `json:"focus"`
	Pain          string `json:"pain"`
	Career        string `json:"career"`
	Personal      string `json:"personal"`
	Communication string `json:"communication"`
	AI            string `json:"ai"`
}

// ReadCustomerProfile reuses the installed HXC connector. It reads one exact
// live account in a read-only transaction; no warehouse job or 55 API is used.
func (source *MySQL) ReadCustomerProfile(ctx context.Context, unionID string) (port.CustomerProfile, error) {
	if !source.Ready() || strings.TrimSpace(unionID) != unionID || unionID == "" {
		return port.CustomerProfile{}, fmt.Errorf("%w: configuration", ErrProfileUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	tx, err := source.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return port.CustomerProfile{}, fmt.Errorf("%w: begin", ErrProfileUnavailable)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,updated_at FROM new_version_users WHERE unionid=? AND is_deleted=0 ORDER BY id LIMIT 2`, unionID)
	if err != nil {
		return port.CustomerProfile{}, fmt.Errorf("%w: accounts_query", ErrProfileUnavailable)
	}
	type account struct {
		id string
		at time.Time
	}
	accounts := []account{}
	for rows.Next() {
		var row account
		if err = rows.Scan(&row.id, &row.at); err != nil {
			rows.Close()
			return port.CustomerProfile{}, fmt.Errorf("%w: account_scan", ErrProfileUnavailable)
		}
		accounts = append(accounts, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(accounts) > 1 {
		return port.CustomerProfile{}, fmt.Errorf("%w: account_ambiguity", ErrProfileUnavailable)
	}
	if len(accounts) == 0 {
		return port.CustomerProfile{Basic: []port.ProfileField{}, Background: []port.ProfileField{}}, nil
	}
	a := accounts[0]
	var raw []byte
	var backgroundAt sql.NullTime
	// Source tables retain different historical collations. Binding the exact
	// already-selected ID per table avoids a cross-collation join or any source
	// schema changes; all reads remain in this one source transaction.
	err = tx.QueryRowContext(ctx, `SELECT JSON_OBJECT('identity',identity_stage,'income',monthly_income_range,'business',business_mode,'stage',business_stage,'focus',business_focus,'pain',core_painful_scenario,'career',career_history,'personal',persona_sketch,'communication',communication_style,'ai',ai_usage_status),updated_at FROM new_version_user_backgrounds WHERE user_id=?`, a.id).Scan(&raw, &backgroundAt)
	if errors.Is(err, sql.ErrNoRows) {
		raw = []byte(`{}`)
	} else if err != nil {
		return port.CustomerProfile{}, fmt.Errorf("%w: background_query", ErrProfileUnavailable)
	}
	var fields profileSource
	if json.Unmarshal(raw, &fields) != nil {
		return port.CustomerProfile{}, fmt.Errorf("%w: background_shape", ErrProfileUnavailable)
	}
	err = tx.QueryRowContext(ctx, `SELECT coalesce(industry,'') FROM user_profiles WHERE user_id=? ORDER BY updated_at DESC,id DESC LIMIT 1`, a.id).Scan(&fields.Industry)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return port.CustomerProfile{}, fmt.Errorf("%w: industry_query", ErrProfileUnavailable)
	}
	var guidance []byte
	var state string
	var guidanceAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT guidance_json,status,updated_at FROM new_version_relationship_service_guidance WHERE user_id=? ORDER BY service_week_start DESC,version DESC LIMIT 1`, a.id).Scan(&guidance, &state, &guidanceAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return port.CustomerProfile{}, fmt.Errorf("%w: guidance_query", ErrProfileUnavailable)
	}
	if state != "ready" && state != "neutral" && state != "published" {
		guidance = nil
	}
	result, err := projectProfile(fields, guidance)
	if err != nil {
		return port.CustomerProfile{}, err
	}
	// Confirmed relation fields refine the corresponding background values.
	rows, err = tx.QueryContext(ctx, `SELECT field_key,value_json,updated_at FROM new_version_relationship_profiles WHERE user_id=? AND is_active=1 AND deleted_at IS NULL ORDER BY field_key`, a.id)
	if err != nil {
		return port.CustomerProfile{}, fmt.Errorf("%w: facts_query", ErrProfileUnavailable)
	}
	for rows.Next() {
		var key string
		var value []byte
		var at time.Time
		if err = rows.Scan(&key, &value, &at); err != nil {
			rows.Close()
			return port.CustomerProfile{}, fmt.Errorf("%w: facts_scan", ErrProfileUnavailable)
		}
		if label, ok := profileLabels[key]; ok {
			var v any
			if json.Unmarshal(value, &v) != nil {
				rows.Close()
				return port.CustomerProfile{}, fmt.Errorf("%w: facts_shape", ErrProfileUnavailable)
			}
			setProfileField(&result.Basic, port.ProfileField{Key: key, Label: label, Value: readableProfileValue(v)})
		}
		if at.After(result.UpdatedAt) {
			result.UpdatedAt = at
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return port.CustomerProfile{}, fmt.Errorf("%w: facts_iteration", ErrProfileUnavailable)
	}
	for _, at := range []time.Time{a.at, backgroundAt.Time, guidanceAt} {
		if at.After(result.UpdatedAt) {
			result.UpdatedAt = at
		}
	}
	var recentMessages int64
	var lastMessage sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT count(*),max(created_at) FROM new_version_messages WHERE user_id=? AND role='user' AND is_deleted=0 AND created_at>=DATE_SUB(NOW(),INTERVAL 7 DAY)`, a.id).Scan(&recentMessages, &lastMessage)
	if err != nil {
		return port.CustomerProfile{}, fmt.Errorf("%w: activity_query", ErrProfileUnavailable)
	}
	if lastMessage.Valid && lastMessage.Time.After(result.UpdatedAt) {
		result.UpdatedAt = lastMessage.Time
	}
	activity := fmt.Sprintf("近7天用户消息 %d 条", recentMessages)
	for _, field := range result.Diagnosis {
		if field.Key == "activity_posture" && field.Value != "待确认" {
			activity += "；" + field.Value
		}
	}
	setProfileField(&result.Diagnosis, port.ProfileField{Key: "activity_posture", Label: "活跃情况与沟通姿态", Value: activity})
	result.Mapped = true
	digest, _ := json.Marshal(struct {
		Basic, Background, Diagnosis []port.ProfileField
		At                           time.Time
	}{result.Basic, result.Background, result.Diagnosis, result.UpdatedAt})
	hash := sha256.Sum256(digest)
	result.Version = "hxc-profile.v1:" + hex.EncodeToString(hash[:])
	if err = tx.Commit(); err != nil {
		return port.CustomerProfile{}, fmt.Errorf("%w: commit", ErrProfileUnavailable)
	}
	return result, nil
}

var profileLabels = map[string]string{"identity": "身份／角色", "industry": "行业", "income": "收入／营收阶段", "business": "业务形态", "business_stage": "业务阶段", "communication_preference": "沟通偏好", "ai_stage": "AI 应用阶段", "self_view": "自我认知", "topic_interests": "关注话题", "needs": "用户明确表达的需求"}

func setProfileField(fields *[]port.ProfileField, field port.ProfileField) {
	field.Value = strings.TrimSpace(field.Value)
	if field.Value == "" {
		return
	}
	for i := range *fields {
		if (*fields)[i].Key == field.Key {
			(*fields)[i] = field
			return
		}
	}
	*fields = append(*fields, field)
}

func readableProfileValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprint(v)
	case []any:
		parts := []string{}
		for _, item := range v {
			if text := readableProfileValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "；")
	case map[string]any:
		parts := []string{}
		for _, key := range []string{"summary", "description", "title", "text", "self_description", "organization", "company", "role", "occupation", "location", "constraint", "advice", "user_response", "result", "strengths", "growth_edges", "items"} {
			if text := readableProfileValue(v[key]); text != "" {
				duplicate := false
				for _, p := range parts {
					duplicate = duplicate || p == text
				}
				if !duplicate {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "；")
	}
	return ""
}

func projectProfile(source profileSource, raw json.RawMessage) (port.CustomerProfile, error) {
	result := port.CustomerProfile{Basic: []port.ProfileField{}, Background: []port.ProfileField{}}
	for _, field := range []port.ProfileField{
		{"identity", "身份／角色", source.Identity}, {"industry", "行业", source.Industry}, {"income", "收入／营收阶段", source.Income}, {"business", "业务形态", source.Business}, {"business_stage", "业务阶段", source.Stage}, {"business_focus", "业务焦点", source.Focus}, {"pain", "主要卡点", source.Pain}, {"communication_preference", "沟通偏好", source.Communication}, {"ai_stage", "AI 应用阶段", source.AI},
	} {
		setProfileField(&result.Basic, field)
	}
	setProfileField(&result.Background, port.ProfileField{"career", "背景与经历", source.Career})
	setProfileField(&result.Background, port.ProfileField{"personal", "个人背景", source.Personal})
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	type understandingItem struct {
		Text         string   `json:"text"`
		EvidenceKeys []string `json:"evidence_keys"`
	}
	var guidance struct {
		RecentSession struct {
			Summary string `json:"summary"`
		} `json:"recent_session_understanding"`
		UserUnderstanding struct {
			Result struct {
				Understanding   string `json:"understanding"`
				CurrentGuidance struct {
					Priority     string   `json:"priority"`
					SupportMode  string   `json:"support_mode"`
					EvidenceKeys []string `json:"evidence_keys"`
				} `json:"current_guidance"`
				Backgrounds     []understandingItem `json:"backgrounds"`
				CoreInsights    []understandingItem `json:"core_insights"`
				FocusCandidates []understandingItem `json:"focus_candidates"`
				SupportHistory  []understandingItem `json:"support_history"`
			} `json:"result"`
			References map[string]struct {
				Message      string `json:"message_ref"`
				Conversation string `json:"conversation_ref"`
				Focus        string `json:"focus_ref"`
				MessageID    string `json:"message_id"`
				SessionID    string `json:"session_id"`
				SourceKey    string `json:"source_key"`
				At           string `json:"expressed_at"`
			} `json:"references"`
		} `json:"user_understanding"`
	}
	// Decode reference names explicitly: source metadata is used for eligibility,
	// never rendered or turned into a current confirmed customer fact.
	if json.Unmarshal(raw, &guidance) != nil {
		return result, ErrProfileUnavailable
	}
	understanding := guidance.UserUnderstanding.Result
	eligibleKeys := func(keys []string) bool {
		for _, key := range keys {
			ref := guidance.UserUnderstanding.References[key]
			if ref.At != "" && (ref.Message != "" || ref.Conversation != "" || ref.Focus != "" || ref.MessageID != "" || ref.SessionID != "" || ref.SourceKey != "") {
				return true
			}
		}
		return false
	}
	keys := make([]string, 0, len(guidance.UserUnderstanding.References))
	for key := range guidance.UserUnderstanding.References {
		keys = append(keys, key)
	}
	summary := ""
	if eligibleKeys(keys) {
		summary = guidance.RecentSession.Summary
		if strings.TrimSpace(summary) == "" {
			summary = understanding.Understanding
		}
	}
	priority, posture := "", ""
	if eligibleKeys(understanding.CurrentGuidance.EvidenceKeys) {
		priority, posture = understanding.CurrentGuidance.Priority, understanding.CurrentGuidance.SupportMode
	}
	for _, field := range []port.ProfileField{
		{"interaction_summary", "互动提炼摘要", summary}, {"current_need", "当前主需求", priority},
		{"business_intent_stage", "业务／意向阶段", source.Stage},
		{"interaction_relationship", "互动关系良好度", ""}, {"activity_posture", "活跃情况与沟通姿态", posture},
	} {
		if strings.TrimSpace(field.Value) == "" {
			field.Value = "待确认"
		}
		setProfileField(&result.Diagnosis, field)
	}
	for _, group := range []struct {
		key, label string
		items      []understandingItem
	}{{"backgrounds", "背景与经历", understanding.Backgrounds}, {"core_insights", "理解与诊断参考", understanding.CoreInsights}, {"focus_candidates", "主线问题候选", understanding.FocusCandidates}, {"support_history", "过去建议与反馈", understanding.SupportHistory}} {
		for i, item := range group.items {
			eligible := eligibleKeys(item.EvidenceKeys)
			if eligible {
				setProfileField(&result.Background, port.ProfileField{Key: fmt.Sprintf("%s_%d", group.key, i), Label: group.label, Value: item.Text})
			}
		}
	}
	return result, nil
}

var _ port.CustomerProfileReader = (*MySQL)(nil)

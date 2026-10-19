package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	accessapp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/app"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	cfg "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
)

func TestPostgreSQLSidebarGroupBindingCompositionJourney(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	databaseURL, cleanup := adminAccessCompositionDatabase(t, ctx)
	defer cleanup()
	owner := "sidebar-group-owner"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "access_token": "synthetic-token", "expires_in": 7200})
		case "/cgi-bin/externalcontact/groupchat/get":
			var body struct {
				ChatID string `json:"chat_id"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "group_chat": map[string]any{"chat_id": body.ChatID, "name": "群绑定验收", "owner": owner, "member_list": []any{}}})
		default:
			t.Errorf("unexpected Provider endpoint %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer provider.Close()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	application, err := compose(ctx, cfg.Runtime{Role: cfg.RoleAPI, DatabaseURL: databaseURL, PublicOrigin: "https://sidebar-group.fixture", ReleaseSHA: "sidebar-group-journey", WorkerOwner: "sidebar-group-journey", WorkerLimit: 1,
		GroupOps:  cfg.GroupOps{WebhookSecret: "sidebar-group-journey-webhook", ProviderReadEnabled: true},
		Survey:    cfg.Survey{DataKey: base64.RawStdEncoding.EncodeToString(key), IdentityPhoneDataKey: base64.RawStdEncoding.EncodeToString(key)},
		Bootstrap: cfg.Bootstrap{Enabled: true, Username: "sidebar-group-admin", Password: "sidebar-group-password", DisplayName: "群主验收"},
		WeCom:     cfg.WeCom{Enabled: true, CorpID: "fixture-corp", AgentID: "fixture-agent", Secret: "fixture-secret", ContactSecret: "fixture-contact", ContextSigningKey: strings.Repeat("k", 32), APIBase: provider.URL, HTTPClient: provider.Client()},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	if err = application.bootstrap(ctx, cfg.Bootstrap{Enabled: true, Username: "sidebar-group-admin", Password: "sidebar-group-password", DisplayName: "群主验收"}); err != nil {
		t.Fatal(err)
	}
	var actor, plan int64
	if err = application.pool.Native().QueryRow(ctx, `UPDATE admin_users SET wecom_userid=$1 WHERE username='sidebar-group-admin' RETURNING id`, owner).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if err = application.pool.Native().QueryRow(ctx, `INSERT INTO group_ops_plans(name,status,revision,created_by,updated_by,created_at,updated_at,plan_type) VALUES('侧边栏验收草稿','draft',1,$1,$1,clock_timestamp(),clock_timestamp(),'standard') RETURNING id`, actor).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if _, err = application.pool.Native().Exec(ctx, `INSERT INTO group_ops_plan_members(plan_id,staff_id) VALUES($1,$2)`, plan, actor); err != nil {
		t.Fatal(err)
	}
	session, err := application.authentication.LoginWithWeComUserID(ctx, accessapp.WeComLoginCommand{WeComUserID: owner, Remote: "sidebar-group-journey"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, body string, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "aicrm_sidebar_session", Value: session.SessionToken})
		r.AddCookie(&http.Cookie{Name: "aicrm_sidebar_csrf", Value: session.CSRFToken})
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		r.Header.Set("Idempotency-Key", "sidebar-group-bind-original")
		w := httptest.NewRecorder()
		application.handler.ServeHTTP(w, r)
		return w
	}
	read := func() p.SidebarContext {
		t.Helper()
		w := request("/api/sidebar/v2/group-ops/context", `{"chat_id":"fixture-owned"}`, false)
		if w.Code != 200 {
			t.Fatalf("context %d %s", w.Code, w.Body.String())
		}
		var out p.SidebarContext
		if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := read()
	if len(before.Candidates) != 1 || before.Candidates[0].ID != plan || len(before.Bindings) != 0 {
		t.Fatalf("before=%+v", before)
	}
	body := fmt.Sprintf(`{"chat_id":"fixture-owned","plan_id":"%d","expected_revision":1}`, plan)
	if w := request("/api/sidebar/v2/group-ops/bind", body, false); w.Code != 403 {
		t.Fatalf("missing CSRF %d", w.Code)
	}
	if w := request("/api/sidebar/v2/group-ops/bind", body, true); w.Code != 200 {
		t.Fatalf("bind %d %s", w.Code, w.Body.String())
	}
	after := read()
	if len(after.Bindings) != 1 || after.Bindings[0].Status != p.PlanDraft || after.Bindings[0].Revision != 2 || len(after.Candidates) != 0 {
		t.Fatalf("after=%+v", after)
	}
	if w := request("/api/sidebar/v2/group-ops/bind", body, true); w.Code != 200 {
		t.Fatalf("replay %d", w.Code)
	}
	if final := read(); final.Bindings[0].Revision != 2 {
		t.Fatal("replay mutated revision")
	}
	owner = "transferred-owner"
	if w := request("/api/sidebar/v2/group-ops/context", `{"chat_id":"fixture-owned"}`, false); w.Code != 403 {
		t.Fatalf("transferred group %d", w.Code)
	}
	for _, table := range []string{"group_ops_runs", "group_ops_executions", "external_effect_jobs"} {
		var count int
		if err = application.pool.Native().QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("unexpected %s rows=%d err=%v", table, count, err)
		}
	}
}

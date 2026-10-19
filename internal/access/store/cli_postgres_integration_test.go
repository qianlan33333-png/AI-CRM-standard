package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	accessport "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	accessapp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/app"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/credential"
	accesshttp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/http"
)

func TestPostgreSQLCLIAccountTokenHTTPPolicyResetAndPersistence(t *testing.T) {
	if environmentValue("AICRM_DATABASE_URL") == "" {
		t.Fatal("required isolated PostgreSQL URL missing")
	}
	f := newGovernanceBehaviorFixture(t)
	auth, e := accessapp.NewAuthentication(f.repository, f.unit, credential.PasswordHasher{}, accessapp.AuthenticationConfig{DummyPHCHash: f.owner.PasswordHash})
	if e != nil {
		t.Fatal(e)
	}
	h, e := accesshttp.NewHandler(accesshttp.Config{Renderer: governanceTestRenderer{}, Auth: auth, Management: f.management, CookieSecure: true})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewTLSServer(h.CLIBearer(h.Routes()))
	defer server.Close()
	type issued struct {
		Token   string     `json:"access_token"`
		CSRF    string     `json:"csrf_token"`
		Expires *time.Time `json:"expires_at"`
	}
	call := func(method, path string, body any, token issued, want int) []byte {
		t.Helper()
		b, _ := json.Marshal(body)
		r, _ := http.NewRequestWithContext(f.ctx, method, server.URL+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		if token.Token != "" {
			r.Header.Set("Authorization", "Bearer "+token.Token)
			r.Header.Set("X-CSRF-Token", token.CSRF)
		}
		res, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d", method, path, res.StatusCode, want)
		}
		var raw json.RawMessage
		json.NewDecoder(res.Body).Decode(&raw)
		return raw
	}
	login := func(want int) issued {
		raw := call("POST", "/api/admin/access/cli/login", map[string]string{"username": "owner", "password": "governance-password"}, issued{}, want)
		var result issued
		if want == 200 && json.Unmarshal(raw, &result) != nil {
			t.Fatal("invalid token response")
		}
		return result
	}
	first := login(200)
	if first.Expires == nil || time.Until(*first.Expires) < 23*time.Hour {
		t.Fatal("not 24 hours")
	}
	call("GET", "/api/admin/access/cli/session", nil, first, 200)
	// Browser Cookie replay of the CLI token is rejected by Access itself.
	r, _ := http.NewRequest("GET", server.URL+"/api/admin/access/cli/session", nil)
	r.AddCookie(&http.Cookie{Name: accesshttp.SessionCookieName, Value: first.Token})
	res, e := server.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("CLI token accepted as cookie")
	}
	actor, e := auth.Authenticate(cliContext(first.Token), first.Token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = auth.SetCLIPolicy(f.ctx, actor, f.owner.ID, []string{"127.0.0.1"}); e != nil {
		t.Fatal(e)
	}
	if policy, err := auth.ReadCLIPolicy(f.ctx, actor, f.owner.ID); err != nil || len(policy.AllowedCIDRs) != 1 || policy.AllowedCIDRs[0] != "127.0.0.1/32" {
		t.Fatalf("plain IP was not persisted canonically: %v %v", policy, err)
	}
	call("GET", "/api/admin/access/cli/session", nil, first, 401)
	long := login(200)
	if long.Expires != nil {
		t.Fatal("whitelist not long lived")
	}
	// Recreate Authentication to prove state is durable, not kept in a client/session cache.
	restarted, e := accessapp.NewAuthentication(f.repository, f.unit, credential.PasswordHasher{}, accessapp.AuthenticationConfig{DummyPHCHash: f.owner.PasswordHash})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = restarted.Authenticate(cliContext(long.Token), long.Token); e != nil {
		t.Fatal("persistent token failed", e)
	}
	if _, e = auth.SetCLIPolicy(f.ctx, actor, f.owner.ID, []string{"192.0.2.1/32"}); e != nil {
		t.Fatal(e)
	}
	login(403)
	call("GET", "/api/admin/access/cli/session", nil, long, 401)
	if _, e = auth.SetCLIPolicy(f.ctx, actor, f.owner.ID, []string{}); e != nil {
		t.Fatal(e)
	}
	current := login(200)
	if e = f.unit.Within(f.ctx, func(tx context.Context) error {
		return f.repository.SetPasswordHash(tx, f.owner.ID, f.owner.PasswordHash, time.Now())
	}); e != nil {
		t.Fatal(e)
	}
	call("GET", "/api/admin/access/cli/session", nil, current, 401)
	var count int
	if e = f.native.QueryRow(f.ctx, `SELECT count(*) FROM admin_access_audit WHERE action='cli_policy_changed'`).Scan(&count); e != nil || count != 3 {
		t.Fatal("policy audit not atomic", count, e)
	}
}

func cliContext(token string) context.Context {
	return accessport.WithCLIRequest(context.Background(), accessport.CLIRequest{Token: token, Source: netip.MustParseAddr("127.0.0.1")})
}

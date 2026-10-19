package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/credential"
	testconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config/tests"
)

func TestCLIAccountTokenInstalledContract(t *testing.T) {
	if testconfig.InstalledSurvey().Binary == "" {
		t.Fatal("installed binary inputs required")
	}
	f := newPublicSurveyPresentationChromiumFixture(t, publicSurveyBrowserOptions{accessGovernance: true})
	type token struct {
		Value  string     `json:"access_token"`
		CSRF   string     `json:"csrf_token"`
		Expiry *time.Time `json:"expires_at"`
	}
	call := func(method, path string, body any, auth token, source string, want int, idempotencyKey ...string) []byte {
		t.Helper()
		b, _ := json.Marshal(body)
		r, _ := http.NewRequestWithContext(f.ctx, method, f.server.URL+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json")
		// Synthetic source through the fixture's controlled, loopback trusted proxy.
		r.Header.Set("X-Forwarded-For", source)
		if len(idempotencyKey) > 0 {
			r.Header.Set("Idempotency-Key", idempotencyKey[0])
		}
		if auth.Value != "" {
			r.Header.Set("Authorization", "Bearer "+auth.Value)
			r.Header.Set("X-CSRF-Token", auth.CSRF)
		}
		res, e := f.server.Client().Do(r)
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
	loginPassword := func(user, password string, want int) token {
		raw := call("POST", "/api/admin/access/cli/login", map[string]string{"username": user, "password": password}, token{}, "203.0.113.20", want)
		var value token
		if want == 200 && json.Unmarshal(raw, &value) != nil {
			t.Fatal("token response")
		}
		return value
	}
	login := func(user string, want int) token { return loginPassword(user, "isolated-survey-admin-password", want) }
	owner := login("public-survey-browser", 200)
	if owner.Expiry == nil || time.Until(*owner.Expiry) < 23*time.Hour {
		t.Fatal("default CLI lifetime")
	}
	hash, e := (credential.PasswordHasher{}).Hash("isolated-survey-admin-password")
	if e != nil {
		t.Fatal(e)
	}
	var targetID int64
	if e = f.native.QueryRow(f.ctx, `WITH u AS (INSERT INTO admin_users(username,password_hash,display_name,is_active,login_enabled,access_granted_at) VALUES('cli-installed-viewer',$1,'CLI fixture',true,true,clock_timestamp()) RETURNING id), r AS (INSERT INTO admin_user_roles(admin_user_id,role_code) SELECT id,'viewer' FROM u) SELECT id FROM u`, hash).Scan(&targetID); e != nil {
		t.Fatal(e)
	}
	path := fmt.Sprintf("/api/admin/access/users/%d/cli-policy", targetID)
	short := login("cli-installed-viewer", 200)
	call("PUT", path, map[string]any{"allowed_cidrs": []string{"203.0.113.20/32"}}, short, "203.0.113.20", 403)
	call("PUT", path, map[string]any{"allowed_cidrs": []string{"203.0.113.20/32"}}, owner, "203.0.113.20", 200)
	call("GET", "/api/admin/access/cli/session", nil, short, "203.0.113.20", 401)
	long := login("cli-installed-viewer", 200)
	if long.Expiry != nil {
		t.Fatal("bound token must not expire")
	}
	call("GET", "/api/admin/access/cli/session", nil, long, "203.0.113.20", 200)
	call("GET", "/api/admin/access/cli/session", nil, long, "198.51.100.20", 403)
	call("PUT", path, map[string]any{"allowed_cidrs": []string{}}, owner, "203.0.113.20", 200)
	call("GET", "/api/admin/access/cli/session", nil, long, "203.0.113.20", 401)
	fresh := login("cli-installed-viewer", 200)
	passwordPath := fmt.Sprintf("/api/admin/access/users/%d/password", targetID)
	call("POST", passwordPath, map[string]string{"password": "Seven77"}, owner, "203.0.113.20", 400, "cli-installed-short-password")
	call("GET", "/api/admin/access/cli/session", nil, fresh, "203.0.113.20", 200)
	for range 2 {
		call("POST", passwordPath, map[string]string{"password": "EightPw8"}, owner, "203.0.113.20", 200, "cli-installed-password-reset")
	}
	call("GET", "/api/admin/access/cli/session", nil, fresh, "203.0.113.20", 401)
	login("cli-installed-viewer", 401)
	eight := loginPassword("cli-installed-viewer", "EightPw8", 200)
	call("GET", "/api/admin/access/cli/session", nil, eight, "203.0.113.20", 200)
	// Expiry is checked by the installed server, independent of client activity.
	digest := credential.Digest(owner.Value)
	if _, e = f.native.Exec(f.ctx, `UPDATE admin_sessions SET created_at=clock_timestamp()-interval '25 hours',expires_at=clock_timestamp()-interval '1 second' WHERE token_digest=$1`, digest[:]); e != nil {
		t.Fatal(e)
	}
	call("GET", "/api/admin/access/cli/session", nil, owner, "203.0.113.20", 401)
}

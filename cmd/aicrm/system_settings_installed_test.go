package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/credential"
	platformconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
	testconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config/tests"
)

// Every browser/API request goes to the supplied immutable application through
// the existing TLS proxy. Source stores only seed/read the isolated fixture.
func TestSystemSettingsInstalledContract(t *testing.T) {
	if testconfig.InstalledSurvey().Binary == "" {
		t.Fatal("installed binary and full package provenance required")
	}
	if _, err := platformconfig.DatabaseURL(); err != nil {
		t.Fatal("isolated PostgreSQL required")
	}
	f := newPublicSurveyPresentationChromiumFixture(t, publicSurveyBrowserOptions{accessGovernance: true})
	hash, err := (credential.PasswordHasher{}).Hash("isolated-settings-password")
	if err != nil {
		t.Fatal(err)
	}
	var targetID int64
	for _, role := range []string{"viewer", "admin"} {
		var id int64
		err = f.native.QueryRow(f.ctx, `WITH u AS (INSERT INTO admin_users(username,password_hash,display_name,is_active,login_enabled,access_granted_at) VALUES($1,$2,$1,true,true,clock_timestamp()) RETURNING id), r AS (INSERT INTO admin_user_roles(admin_user_id,role_code) SELECT id,$3 FROM u) SELECT id FROM u`, "settings-installed-"+role, hash, role).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		if role == "viewer" {
			targetID = id
		}
	}
	path := fmt.Sprintf("/api/admin/access/users/%d/cli-policy", targetID)
	// Anonymous requests cannot receive settings markup or sensitive policy data.
	for _, check := range []struct {
		path   string
		status int
	}{{"/admin/config/system", 303}, {"/api/admin/access/users", 401}, {path, 401}} {
		res, e := f.server.Client().Get(f.server.URL + check.path)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != check.status {
			t.Fatalf("anonymous %s status=%d", check.path, res.StatusCode)
		}
	}
	cookies := map[string]string{}
	for _, c := range f.adminCookies {
		cookies[c.Name] = c.Value
	}
	screenshots := newCapabilitiesScreenshotDirectory(t, "system-settings")
	input, err := json.Marshal(map[string]any{"base": f.browserURL, "cookies": cookies, "target": targetID, "screenshots": screenshots})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(f.ctx, "node", filepath.Join(filepath.Dir(f.script), "system_settings_chromium_journey.mjs"))
	command.Env = append(os.Environ(), "AICRM_SYSTEM_SETTINGS_INSTALLED_FIXTURE="+string(input))
	// Synthetic credentials stay in the environment and out of test output.
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("installed settings Chromium: %v\n%s", err, output)
	}
	var result struct {
		Contract string `json:"contract"`
		Status   string `json:"status"`
		Puts     int    `json:"policy_puts"`
	}
	for _, line := range bytes.Split(output, []byte("\n")) {
		var row struct {
			Contract string `json:"contract"`
			Status   string `json:"status"`
			Puts     int    `json:"policy_puts"`
		}
		if json.Unmarshal(line, &row) == nil && row.Contract == "system-settings-installed" {
			result = row
		}
	}
	if result.Contract != "system-settings-installed" || result.Status != "passed" || result.Puts != 2 {
		t.Fatal("missing structured installed settings browser verdict")
	}
	t.Log(strings.TrimSpace(string(output)))
	assertStored := func() {
		t.Helper()
		var version int64
		var cidrs []string
		var audits int
		if e := f.native.QueryRow(f.ctx, `SELECT version,allowed_cidrs FROM admin_cli_policies WHERE admin_user_id=$1`, targetID).Scan(&version, &cidrs); e != nil || version != 3 || len(cidrs) != 0 {
			t.Fatalf("persisted clear/version mismatch version=%d cidrs=%d err=%v", version, len(cidrs), e)
		}
		if e := f.native.QueryRow(f.ctx, `SELECT count(*) FROM admin_access_audit WHERE target_admin_user_id=$1 AND action='cli_policy_changed'`, targetID).Scan(&audits); e != nil || audits != 2 {
			t.Fatalf("policy audit mismatch count=%d err=%v", audits, e)
		}
	}
	assertStored()
	// A failing audit must roll back both the policy and its revocation version.
	// This trigger exists only in the disposable database, never in the package.
	_, err = f.native.Exec(f.ctx, `CREATE FUNCTION settings_reject_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='cli_policy_changed' THEN RAISE EXCEPTION 'synthetic audit rejection'; END IF; RETURN NEW; END $$; CREATE TRIGGER settings_reject_audit BEFORE INSERT ON admin_access_audit FOR EACH ROW EXECUTE FUNCTION settings_reject_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(f.ctx, "PUT", f.server.URL+path, strings.NewReader(`{"allowed_cidrs":["203.0.113.21"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", f.browserURL)
	request.Header.Set("X-CSRF-Token", cookies["aicrm_admin_csrf"])
	for _, c := range f.adminCookies {
		request.AddCookie(c)
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 500 {
		t.Fatalf("audit rejection status=%d", response.StatusCode)
	}
	assertStored()
	t.Log("installed settings: canonical IPv4/IPv6 readback, clear, CLI lifetime/revocation, role denial, atomic audit rollback verified")
}

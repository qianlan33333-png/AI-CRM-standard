package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	accessapp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/app"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	platformconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
	testconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config/tests"
)

// All business HTTP goes to the supplied package process. Source services
// only prepare disposable staff/session facts; no real Provider is reachable.
func TestSidebarGroupBindingInstalled(t *testing.T) {
	if !testconfig.InstalledProductContractsRequested() {
		t.Skip("explicit installed Product package required")
	}
	if testconfig.InstalledProduct().Binary == "" {
		t.Fatal("installed Product package inputs are incomplete")
	}
	f := newProductExternalPushChromiumFixtureWithOptions(t, 220*time.Second, productExternalPushChromiumFixtureOptions{enablePublicH5: true, enableAlipay: true, deferEffectsWorker: true})
	pool := f.application.pool.Native()
	const owner = "sidebar-installed-owner"
	var actor int64
	if err := pool.QueryRow(f.ctx, `UPDATE admin_users SET wecom_userid=$1 WHERE username='product-browser-owner' RETURNING id`, owner).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	var transferred, unavailable atomic.Bool
	provider, trust, reads, unexpected := installedSurveyProviderBoundary(t, installedProviderReadOverride{
		host:  "qyapi.weixin.qq.com",
		paths: map[string]bool{"/cgi-bin/gettoken": true, "/cgi-bin/get_jsapi_ticket": true, "/cgi-bin/ticket/get": true, "/cgi-bin/externalcontact/groupchat/get": true},
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if unavailable.Load() {
				http.Error(w, "synthetic provider unavailable", 503)
				return
			}
			switch r.URL.Path {
			case "/cgi-bin/gettoken":
				_, _ = io.WriteString(w, `{"errcode":0,"access_token":"synthetic-group-token","expires_in":7200}`)
			case "/cgi-bin/get_jsapi_ticket", "/cgi-bin/ticket/get":
				_, _ = io.WriteString(w, `{"errcode":0,"ticket":"synthetic-group-ticket","expires_in":7200}`)
			case "/cgi-bin/externalcontact/groupchat/get":
				var body struct {
					ChatID string `json:"chat_id"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					http.Error(w, "invalid synthetic read", 400)
					return
				}
				groupOwner := owner
				if body.ChatID == "foreign-group" || transferred.Load() {
					groupOwner = "other-owner"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "group_chat": map[string]any{"chat_id": body.ChatID, "name": "安装群绑定验收", "owner": groupOwner, "member_list": []any{}}})
			}
		}),
	})
	useInstalledPeriodicContactFixtureWithEnvironment(t, f, []string{
		"AICRM_WECOM_ENABLED=true", "AICRM_WECOM_CORP_ID=admin-layout-fixture-corp", "AICRM_WECOM_AGENT_ID=1",
		"AICRM_WECOM_SECRET=synthetic-group-secret", "AICRM_WECOM_CONTACT_SECRET=synthetic-group-contact", "AICRM_WECOM_CONTEXT_SIGNING_KEY=" + strings.Repeat("g", 32),
		"AICRM_GROUP_OPS_WEBHOOK_SECRET=synthetic-group-webhook-key-for-installed-tests", "AICRM_GROUP_OPS_PROVIDER_READ_ENABLED=true", "AICRM_GROUP_OPS_PROVIDER_ENABLED=false",
		"AICRM_OUTBOUND_PROVIDER_ENABLED=false", "AICRM_EFFECTS_PROVIDER_ENABLED=false", "AICRM_COMMERCE_PUSH_PROVIDER_ENABLED=false",
		"AICRM_OPEN_PLATFORM_TRUSTED_PROXY_CIDRS=127.0.0.1/32",
		"HTTPS_PROXY=" + provider, "HTTP_PROXY=" + provider, "NO_PROXY=127.0.0.1,localhost,::1", "SSL_CERT_FILE=" + trust,
	})
	// The installed process listens on HTTP behind this controlled TLS proxy.
	// Preserve the original transport and a synthetic untrusted client hop,
	// exactly as the existing Source resolver expects in production. The
	// application still rejects plain HTTP and untrusted forwarding headers.
	proxy, ok := f.server.Config.Handler.(*httputil.ReverseProxy)
	if !ok {
		t.Fatal("installed sidebar fixture must use the controlled reverse proxy")
	}
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		secure := r.TLS != nil
		director(r)
		if secure {
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("X-Forwarded-For", "203.0.113.50")
		}
	}
	session, err := f.application.authentication.LoginWithWeComUserID(f.ctx, accessapp.WeComLoginCommand{WeComUserID: owner, Remote: "installed-group-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	baseline := groupOpsBindingFixtureJobs(t, f.ctx, pool)
	newPlan := func(name, status string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(f.ctx, `INSERT INTO group_ops_plans(name,status,revision,created_by,updated_by,created_at,updated_at,plan_type) VALUES($1,$2,1,$3,$3,now(),now(),'standard') RETURNING id`, name, status, actor).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(f.ctx, `INSERT INTO group_ops_plan_members(plan_id,staff_id) VALUES($1,$2)`, id, actor); err != nil {
			t.Fatal(err)
		}
		return id
	}
	draft, paused, active := newPlan("安装草稿", "draft"), newPlan("安装暂停", "paused"), newPlan("启用只读", "active")
	if _, err := pool.Exec(f.ctx, `INSERT INTO group_ops_plan_group_assets(plan_id,asset_reference) VALUES($1,'retained-target'),($2,'owned-group')`, draft, active); err != nil {
		t.Fatal(err)
	}
	request := func(path string, body any, key string, authenticated, csrf bool, want int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		method := "POST"
		if body == nil {
			method = "GET"
			raw = nil
		}
		r, err := http.NewRequestWithContext(f.ctx, method, f.server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", f.server.URL)
		r.Header.Set("Idempotency-Key", key)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "aicrm_sidebar_session", Value: session.SessionToken})
			r.AddCookie(&http.Cookie{Name: "aicrm_sidebar_csrf", Value: session.CSRFToken})
		}
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		res, err := f.server.Client().Do(r)
		if err != nil {
			t.Fatal("installed sidebar HTTP transport")
		}
		defer res.Body.Close()
		out, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("installed sidebar path=%s status=%d want=%d body=%s err=%v", path, res.StatusCode, want, out, err)
		}
		return out
	}
	const contextPath, bindPath = "/api/sidebar/v2/group-ops/context", "/api/sidebar/v2/group-ops/bind"
	contextBody := map[string]any{"chat_id": "owned-group"}
	command := func(chat string, plan, revision int64) map[string]any {
		return map[string]any{"chat_id": chat, "plan_id": fmt.Sprint(plan), "expected_revision": revision}
	}
	request(contextPath, contextBody, "", false, false, 401)
	request(bindPath, command("owned-group", draft, 1), "installed-no-csrf", true, false, 403)
	request(contextPath, map[string]any{"chat_id": "foreign-group"}, "", true, false, 403)
	var before p.SidebarContext
	if err := json.Unmarshal(request(contextPath, contextBody, "", true, false, 200), &before); err != nil || len(before.Bindings) != 1 || before.Bindings[0].ID != active || before.Total != 2 {
		t.Fatalf("installed scope projection=%+v err=%v", before, err)
	}
	request(bindPath, command("unbound-active", active, 1), "installed-active-readonly", true, true, 409)
	request(bindPath, command("owned-group", draft, 1), "installed-sidebar-bind", true, true, 200)
	request(bindPath, command("owned-group", draft, 1), "installed-sidebar-bind", true, true, 200)
	var after p.SidebarContext
	if err := json.Unmarshal(request(contextPath, contextBody, "", true, false, 200), &after); err != nil || len(after.Bindings) != 2 || after.Total != 1 {
		t.Fatalf("installed persisted context=%+v err=%v", after, err)
	}
	var revision, targets int64
	var status string
	if err := pool.QueryRow(f.ctx, `SELECT revision,status,(SELECT count(*) FROM group_ops_plan_group_assets WHERE plan_id=$1) FROM group_ops_plans WHERE id=$1`, draft).Scan(&revision, &status, &targets); err != nil || revision != 2 || status != "draft" || targets != 2 {
		t.Fatalf("installed bind facts revision=%d status=%s targets=%d err=%v", revision, status, targets, err)
	}
	request(bindPath, command("stale-group", draft, 1), "installed-sidebar-stale", true, true, 409)
	// Failure after receipt reservation must roll back the target, revision,
	// receipt, audit and outbox together; the original key may then succeed.
	counts := func() string {
		t.Helper()
		var value string
		if err := pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM group_ops_operation_receipts)::text||':'||(SELECT count(*) FROM group_ops_audit_events)::text||':'||(SELECT count(*) FROM group_ops_outbox)::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	originalCounts := counts()
	if _, err := pool.Exec(f.ctx, `CREATE FUNCTION qa_sidebar_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic binding rollback'; END $$; CREATE TRIGGER qa_sidebar_fail BEFORE INSERT ON group_ops_plan_group_assets FOR EACH ROW EXECUTE FUNCTION qa_sidebar_fail()`); err != nil {
		t.Fatal(err)
	}
	request(bindPath, command("rollback-group", paused, 1), "installed-sidebar-rollback", true, true, 503)
	if counts() != originalCounts {
		t.Fatal("failed binding leaked receipt/audit/outbox")
	}
	if _, err := pool.Exec(f.ctx, `DROP TRIGGER qa_sidebar_fail ON group_ops_plan_group_assets; DROP FUNCTION qa_sidebar_fail()`); err != nil {
		t.Fatal(err)
	}
	request(bindPath, command("rollback-group", paused, 1), "installed-sidebar-rollback", true, true, 200)
	unavailable.Store(true)
	request(contextPath, contextBody, "", true, false, 503)
	unavailable.Store(false)
	// A third draft is reserved solely for the actual packaged mobile UI.
	browserPlan := newPlan("浏览器草稿", "draft")
	config, _ := json.Marshal(map[string]any{"mode": "sidebar", "base": f.server.URL, "plan": fmt.Sprint(browserPlan), "session": session.SessionToken, "csrf": session.CSRFToken, "screenshots": newCapabilitiesScreenshotDirectory(t, "sidebar")})
	runNewCapabilitiesChromium(t, f, string(config))
	if err := pool.QueryRow(f.ctx, `SELECT revision,status,(SELECT count(*) FROM group_ops_plan_group_assets WHERE plan_id=$1 AND asset_reference='browser-group') FROM group_ops_plans WHERE id=$1`, browserPlan).Scan(&revision, &status, &targets); err != nil || revision != 2 || status != "draft" || targets != 1 {
		t.Fatalf("browser persisted binding revision=%d status=%s targets=%d err=%v", revision, status, targets, err)
	}
	// Exercise the new composition routes against the actual installed binary.
	// The client navigation intent itself never creates a message/effect/job.
	const navigation = "/api/sidebar/v2/group-navigation"
	request(navigation, nil, "", false, false, 401)
	request(navigation+"/start", map[string]any{"plan_id": fmt.Sprint(draft), "plan_revision": 2}, "installed-navigation-create", true, false, 403)
	var nav p.NavigationRun
	startBody := map[string]any{"plan_id": fmt.Sprint(draft), "plan_revision": 2}
	if err := json.Unmarshal(request(navigation+"/start", startBody, "installed-navigation-create", true, true, 200), &nav); err != nil || nav.State != "paused" || len(nav.Targets) != 2 {
		t.Fatalf("navigation start=%+v %v", nav, err)
	}
	var replay p.NavigationRun
	if err := json.Unmarshal(request(navigation+"/start", startBody, "installed-navigation-create", true, true, 200), &replay); err != nil || replay.ID != nav.ID {
		t.Fatal("navigation replay", err)
	}
	navAction := func(action string, extra map[string]any, want int) {
		t.Helper()
		body := map[string]any{"run_id": fmt.Sprint(nav.ID), "revision": nav.Revision}
		for k, v := range extra {
			body[k] = v
		}
		raw := request(navigation+"/"+action, body, "", true, true, want)
		if want == 200 {
			if err := json.Unmarshal(raw, &nav); err != nil {
				t.Fatal(err)
			}
		}
	}
	navAction("open", nil, 409)
	navAction("resume", nil, 200)
	navAction("open", nil, 200)
	navAction("confirm", map[string]any{"step": 0, "chat_id": "wrong-group"}, 409)
	navAction("pause", nil, 200)
	navAction("confirm", map[string]any{"step": 0, "chat_id": nav.Targets[0].ChatID}, 409)
	navAction("resume", nil, 200)
	navAction("confirm", map[string]any{"step": 0, "chat_id": nav.Targets[0].ChatID}, 200)
	var restored p.NavigationView
	if err := json.Unmarshal(request(navigation, nil, "", true, false, 200), &restored); err != nil || restored.Run == nil || restored.Run.Cursor != 1 || len(restored.Run.Targets) != 2 {
		t.Fatalf("navigation reload=%+v %v", restored, err)
	}
	navAction("open", nil, 200)
	navAction("confirm", map[string]any{"step": 1, "chat_id": nav.Targets[1].ChatID}, 200)
	if nav.State != "completed" || nav.Cursor != 2 || len(nav.Observations) != 2 {
		t.Fatalf("navigation end=%+v", nav)
	}
	transferred.Store(true)
	request(contextPath, contextBody, "", true, false, 403)
	for _, table := range []string{"group_ops_runs", "group_ops_executions", "external_effect_jobs", "group_ops_group_message_tasks"} {
		var count int
		if err := pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("unexpected %s=%d err=%v", table, count, err)
		}
	}
	groupOpsBindingFixtureNoDispatchJobs(t, baseline, groupOpsBindingFixtureJobs(t, f.ctx, pool))
	if unexpected.Load() != 0 || reads.Load() == 0 {
		t.Fatalf("controlled provider reads=%d forbidden=%d", reads.Load(), unexpected.Load())
	}
	t.Logf("installed_sidebar app_sha=%s binary_sha256=%s controlled_provider_reads=%d unexpected_provider_calls=%d", testconfig.InstalledProduct().SHA, testconfig.InstalledProduct().BinarySHA256, reads.Load(), unexpected.Load())
	t.Run("installed CLI Markdown package save replay and reload", func(t *testing.T) {
		transferred.Store(false)
		cli := filepath.Join(testconfig.InstalledProduct().Root, "bin", "aicrm-cli")
		if info, err := os.Stat(cli); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			t.Fatal("this installed package must include executable bin/aicrm-cli")
		}
		directory := t.TempDir()
		caFile := filepath.Join(directory, "fixture-ca.pem")
		if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.server.Certificate().Raw}), 0600); err != nil {
			t.Fatal(err)
		}
		credentialFile := filepath.Join(directory, "terminal.json")
		environment := []string{}
		for _, value := range os.Environ() {
			if strings.HasPrefix(value, "AICRM_CLI_") || strings.HasPrefix(value, "SSL_CERT_FILE=") {
				continue
			}
			environment = append(environment, value)
		}
		environment = append(environment, "AICRM_CLI_CONFIG_DIR="+directory, "SSL_CERT_FILE="+caFile, "AICRM_CLI_CREDENTIAL_FILE="+credentialFile)
		cliRun := func(input string, args ...string) json.RawMessage {
			t.Helper()
			command := exec.CommandContext(f.ctx, cli, args...)
			command.Env = environment
			command.Stdin = strings.NewReader(input)
			var out, stderr bytes.Buffer
			command.Stdout = &out
			command.Stderr = &stderr
			if err := command.Run(); err != nil {
				t.Fatalf("installed CLI %s failed: %v; output=%s", args[0], err, out.String())
			}
			var envelope struct {
				Data json.RawMessage `json:"data"`
			}
			if json.Unmarshal(out.Bytes(), &envelope) != nil {
				t.Fatal("installed CLI JSON response missing")
			}
			if len(envelope.Data) > 0 {
				return envelope.Data
			}
			return out.Bytes()
		}
		cliRun("", "profile", "add", "fixture", f.server.URL)
		cliRun("product-browser-owner-password", "auth", "login", "--username", "product-browser-owner", "--secret-stdin", "--secret-output", credentialFile)
		packagePlan := newPlan("安装CLI任务包", "draft")
		if _, err := pool.Exec(f.ctx, `INSERT INTO group_ops_plan_group_assets(plan_id,asset_reference)VALUES($1,'package-a'),($1,'package-b')`, packagePlan); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "1.jpg"), []byte("synthetic-local-image"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "test.txt"), []byte("synthetic-local-file"), 0600); err != nil {
			t.Fatal(err)
		}
		doc := p.TaskDocument{Schema: p.TaskPackageSchema, PlanID: packagePlan, PlanRevision: 1, Groups: []string{"package-a", "package-b"}, Messages: []p.SequenceMessage{{ID: "intro", Kind: "text", Text: "installed CLI package"}, {ID: "image", Kind: "image", RelativePath: "1.jpg"}, {ID: "file", Kind: "file", RelativePath: "test.txt"}}, Prose: "# Installed Agent package\n"}
		markdown, err := p.RenderTaskMarkdown(doc)
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(directory, "task.md")
		if err := os.WriteFile(file, []byte(markdown), 0600); err != nil {
			t.Fatal(err)
		}
		var saved, replayed p.TaskPackage
		if err := json.Unmarshal(cliRun("", "group-ops", "rpa-package", "save", "--input", file, "--idempotency-key", "installed-package-original"), &saved); err != nil || saved.ID == 0 || saved.Run.State != "paused" || saved.Run.Sequence == nil || len(saved.Run.Sequence.Messages) != 3 || len(saved.Run.Targets) != 2 {
			t.Fatalf("installed package frozen task missing: %v", err)
		}
		canonical, err := os.ReadFile(file)
		if err != nil || bytes.Count(canonical, []byte("sha256:")) != 2 {
			t.Fatal("installed CLI local digest enrichment missing")
		}
		if err := json.Unmarshal(cliRun("", "group-ops", "rpa-package", "save", "--input", file, "--idempotency-key", "installed-package-original"), &replayed); err != nil || replayed.ID != saved.ID || replayed.Run.ID != saved.Run.ID {
			t.Fatal("installed CLI original key created another package")
		}
		readback := func() {
			t.Helper()
			var view p.TaskPackageView
			if err := json.Unmarshal(cliRun("", "group-ops", "rpa-package", "get", "--query", fmt.Sprintf("plan_id=%d", packagePlan)), &view); err != nil || view.Package == nil || view.Package.ID != saved.ID || view.Package.Run.ID != saved.Run.ID || view.Package.Markdown != string(canonical) {
				t.Fatal("installed CLI package readback differs")
			}
		}
		readback() // Each CLI invocation is a new process, with no in-memory task state.
		var persisted string
		var runID int64
		var count int
		if err := pool.QueryRow(f.ctx, `SELECT markdown,run_id,(SELECT count(*) FROM group_ops_rpa_packages WHERE plan_id=$1) FROM group_ops_rpa_packages WHERE id=$2`, packagePlan, saved.ID).Scan(&persisted, &runID, &count); err != nil || persisted != string(canonical) || runID != saved.Run.ID || count != 1 {
			t.Fatalf("installed package persistence differs: %v", err)
		}
		body, _ := json.Marshal(map[string]string{"markdown": string(canonical)})
		r, _ := http.NewRequestWithContext(f.ctx, "POST", f.server.URL+"/api/admin/automation-conversion/group-ops/rpa-package", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "installed-browser-refused")
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.AddCookie(&http.Cookie{Name: "aicrm_admin_session", Value: session.SessionToken})
		r.AddCookie(&http.Cookie{Name: "aicrm_admin_csrf", Value: session.CSRFToken})
		res, err := f.server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("installed browser package write status=%d", res.StatusCode)
		}
		readback()
		// The fixture has not granted or sent anything; close this paused task
		// before exercising the existing ordered-send contract under the same staff.
		request(navigation+"/cancel", map[string]any{"run_id": fmt.Sprint(saved.Run.ID), "revision": saved.Run.Revision}, "", true, true, 200)
		t.Logf("installed_cli_markdown app_sha=%s cli=%s package_id=%d persisted=true replay_same=true browser_write=403", testconfig.InstalledProduct().SHA, cli, saved.ID)
	})
	t.Run("local RPA pairing and ordered sequence", func(t *testing.T) {
		transferred.Store(false)
		localPlan := newPlan("安装包本地 RPA 验收", "draft")
		if _, e := pool.Exec(f.ctx, `INSERT INTO group_ops_plan_group_assets(plan_id,asset_reference)VALUES($1,'rpa-a'),($1,'rpa-b')`, localPlan); e != nil {
			t.Fatal(e)
		}
		call := func(method, path string, body any, key, mode, token string, csrf bool, want int) []byte {
			t.Helper()
			var raw []byte
			if body != nil {
				raw, _ = json.Marshal(body)
			}
			r, e := http.NewRequestWithContext(f.ctx, method, f.server.URL+path, bytes.NewReader(raw))
			if e != nil {
				t.Fatal(e)
			}
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", key)
			if mode == "admin" || mode == "sidebar" {
				prefix := "aicrm_admin_"
				if mode == "sidebar" {
					prefix = "aicrm_sidebar_"
				}
				r.AddCookie(&http.Cookie{Name: prefix + "session", Value: session.SessionToken})
				r.AddCookie(&http.Cookie{Name: prefix + "csrf", Value: session.CSRFToken})
				if csrf {
					r.Header.Set("X-CSRF-Token", session.CSRFToken)
				}
			}
			if mode == "native" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			response, e := f.server.Client().Do(r)
			if e != nil {
				t.Fatal(e)
			}
			defer response.Body.Close()
			out, e := io.ReadAll(response.Body)
			if e != nil || response.StatusCode != want {
				t.Fatalf("installed RPA %s %s status=%d want=%d body=%s err=%v", method, path, response.StatusCode, want, out, e)
			}
			return out
		}
		runRPASequenceHTTPContract(t, f.ctx, pool, localPlan, owner, call, func() {
			var e error
			session, e = f.application.authentication.LoginWithWeComUserID(f.ctx, accessapp.WeComLoginCommand{WeComUserID: owner, Remote: "fresh-installed-rpa-fixture"})
			if e != nil {
				t.Fatal(e)
			}
		}, f.application.effectsRuntime.Run)
		if unexpected.Load() != 0 {
			t.Fatal("RPA attempted an unexpected Provider endpoint")
		}
	})
}

func runNewCapabilitiesChromium(t *testing.T, f *productExternalPushChromiumFixture, config string) {
	t.Helper()
	// The registered contracts require the Linux Chromium environment. This
	// launches only the repository's disposable automated test browser.
	command := exec.CommandContext(f.ctx, "node", filepath.Join(filepath.Dir(f.script), "new_capabilities_chromium_journey.mjs"))
	command.Env = append(os.Environ(), "AICRM_NEW_CAPABILITIES_FIXTURE="+config)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("installed capability browser: %v\n%s", err, output)
	} else {
		t.Log(strings.TrimSpace(string(output)))
	}
}

func newCapabilitiesScreenshotDirectory(t *testing.T, mode string) string {
	t.Helper()
	if directory := platformconfig.PublicSurveyScreenshotDirectory(); directory != "" {
		if !filepath.IsAbs(directory) {
			t.Fatal("capability screenshot directory must be absolute")
		}
		return filepath.Join(directory, "new-capabilities-"+mode)
	}
	if preparation := os.Getenv("AICRM_TEST_PREP_DIR"); preparation != "" {
		if !filepath.IsAbs(preparation) {
			t.Fatal("capability artifact root must be absolute")
		}
		// The controller owns this persistent report; its disposable execution
		// checkout and t.TempDir are removed before evidence can be exported.
		return filepath.Join(filepath.Dir(preparation), "new-capabilities-browser", mode)
	}
	return t.TempDir()
}

// Keep the Provider read exception narrow even when the application requests
// a forbidden send endpoint or an incorrect transport method.
func TestInstalledGroupProviderBoundaryRejectsWrites(t *testing.T) {
	proxy, trust, reads, unexpected := installedSurveyProviderBoundary(t, installedProviderReadOverride{host: "qyapi.weixin.qq.com", paths: map[string]bool{"/cgi-bin/externalcontact/groupchat/get": true}, handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })})
	client := installedGroupBoundaryClient(t, proxy, trust)
	for _, tc := range []struct {
		method, path string
		want         int
	}{{"POST", "/cgi-bin/externalcontact/groupchat/get", 200}, {"GET", "/cgi-bin/externalcontact/groupchat/get", 403}, {"POST", "/cgi-bin/externalcontact/add_msg_template", 403}} {
		r, _ := http.NewRequest(tc.method, "https://qyapi.weixin.qq.com"+tc.path, strings.NewReader(`{"chat_id":"synthetic"}`))
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("method=%s path=%s status=%d", tc.method, tc.path, res.StatusCode)
		}
	}
	if reads.Load() != 1 || unexpected.Load() != 2 {
		t.Fatal("Provider read/write isolation counters")
	}
}

func installedGroupBoundaryClient(t *testing.T, proxy, trust string) *http.Client {
	t.Helper()
	cert, err := os.ReadFile(trust)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		t.Fatal("invalid synthetic provider CA")
	}
	endpoint, err := url.Parse(proxy)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(endpoint), TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
}

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	accessapp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/app"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/crmcli"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	cfg "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
	"github.com/zalando/go-keyring"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPostgreSQLRPAPackageCLIAtomicSaveReplayAndReload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	databaseURL, cleanup := adminAccessCompositionDatabase(t, ctx)
	defer cleanup()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "access_token": "fixture-token", "expires_in": 7200})
		case "/cgi-bin/externalcontact/groupchat/get":
			var input struct {
				ChatID string `json:"chat_id"`
			}
			json.NewDecoder(r.Body).Decode(&input)
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "group_chat": map[string]any{"chat_id": input.ChatID, "name": "same name", "owner": "package-owner", "member_list": []any{}}})
		default:
			t.Errorf("unexpected Provider endpoint %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer provider.Close()
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	origin := "https://" + server.Listener.Addr().String()
	key := make([]byte, 32)
	rand.Read(key)
	runtime := cfg.Runtime{Role: cfg.RoleAPI, DatabaseURL: databaseURL, PublicOrigin: origin, ReleaseSHA: "package-fixture", WorkerOwner: "package-fixture", WorkerLimit: 1, GroupOps: cfg.GroupOps{WebhookSecret: "fixture-webhook-secret", ProviderReadEnabled: true}, Survey: cfg.Survey{DataKey: base64.RawStdEncoding.EncodeToString(key), IdentityPhoneDataKey: base64.RawStdEncoding.EncodeToString(key)}, Bootstrap: cfg.Bootstrap{Enabled: true, Username: "package-admin", Password: "package-contract-password", DisplayName: "Package owner"}, WeCom: cfg.WeCom{Enabled: true, CorpID: "fixture-corp", AgentID: "fixture-agent", Secret: "fixture-secret", ContactSecret: "fixture-contact", ContextSigningKey: strings.Repeat("k", 32), APIBase: provider.URL, HTTPClient: provider.Client()}}
	application, e := compose(ctx, runtime)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { application.Close() }()
	if e = application.bootstrap(ctx, runtime.Bootstrap); e != nil {
		t.Fatal(e)
	}
	pool := application.pool.Native()
	var actor, plan int64
	if e = pool.QueryRow(ctx, `UPDATE admin_users SET wecom_userid='package-owner' WHERE username='package-admin' RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `INSERT INTO group_ops_plans(name,status,revision,created_by,updated_by,created_at,updated_at,plan_type)VALUES('CLI package','draft',1,$1,$1,now(),now(),'standard')RETURNING id`, actor).Scan(&plan); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO group_ops_plan_members(plan_id,staff_id)VALUES($1,$2)`, plan, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO group_ops_plan_group_assets(plan_id,asset_reference)VALUES($1,'package-a'),($1,'package-b')`, plan); e != nil {
		t.Fatal(e)
	}
	server.Config.Handler = application.handler
	server.StartTLS()
	original := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = original }()
	keyring.MockInit()
	t.Setenv("AICRM_CLI_CONFIG_DIR", t.TempDir())
	for _, name := range []string{"AICRM_CLI_TOKEN", "AICRM_CLI_PASSWORD", "AICRM_CLI_CLIENT_SECRET", "AICRM_CLI_CREDENTIAL_FILE"} {
		t.Setenv(name, "")
	}
	run := func(want int, input string, args ...string) map[string]json.RawMessage {
		t.Helper()
		var out, stderr bytes.Buffer
		code := crmcli.Run(args, strings.NewReader(input), &out, &stderr)
		if code != want {
			t.Fatalf("CLI code %d want %d: %s", code, want, out.String())
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(out.Bytes(), &result) != nil {
			t.Fatal("CLI did not return JSON")
		}
		if data, ok := result["data"]; ok {
			var value map[string]json.RawMessage
			if json.Unmarshal(data, &value) != nil {
				t.Fatal("CLI data is not an object")
			}
			return value
		}
		return result
	}
	run(0, "", "profile", "add", "fixture", origin)
	run(0, "package-contract-password", "auth", "login", "--username", "package-admin", "--secret-stdin")
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "1.jpg"), []byte("synthetic-image"), 0600)
	doc := p.TaskDocument{Schema: p.TaskPackageSchema, PlanID: plan, PlanRevision: 1, Groups: []string{"package-a", "package-b"}, Messages: []p.SequenceMessage{{ID: "text", Kind: "text", Text: "sequence one"}, {ID: "image", Kind: "image", RelativePath: "1.jpg"}}, Prose: "# local Agent package\n"}
	markdown, _ := p.RenderTaskMarkdown(doc)
	file := filepath.Join(root, "task.md")
	os.WriteFile(file, []byte(markdown), 0600)
	saved := run(0, "", "group-ops", "rpa-package", "save", "--input", file, "--idempotency-key", "package-save-original")
	var packageID string
	json.Unmarshal(saved["package_id"], &packageID)
	if packageID == "" {
		t.Fatal("package was not saved")
	}
	var frozen p.NavigationRun
	json.Unmarshal(saved["run"], &frozen)
	if frozen.State != "paused" || len(frozen.Targets) != 2 || len(frozen.Sequence.Messages) != 2 {
		t.Fatal("no frozen paused execution")
	}
	canonical, _ := os.ReadFile(file)
	if !strings.Contains(string(canonical), "sha256:") {
		t.Fatal("CLI did not preserve enriched Markdown")
	}
	replay := run(0, "", "group-ops", "rpa-package", "save", "--input", file, "--idempotency-key", "package-save-original")
	if !bytes.Equal(replay["package_id"], saved["package_id"]) {
		t.Fatal("save replay created another package")
	}
	view := run(0, "", "group-ops", "rpa-package", "get", "--query", fmt.Sprintf("plan_id=%d", plan))
	var read p.TaskPackage
	json.Unmarshal(view["package"], &read)
	if read.Markdown != string(canonical) || read.Run.ID != frozen.ID {
		t.Fatal("package readback differs")
	}
	// A browser may read but cannot configure the new package route.
	browser, e := application.authentication.LoginWithWeComUserID(ctx, accessapp.WeComLoginCommand{WeComUserID: "package-owner", Remote: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	// The native projection must expose the immutable package and full material
	// manifest before the first text, without borrowing the CLI credential.
	nativeCall := func(method, path string, body any, token string, admin bool, want int) []byte {
		t.Helper()
		payload, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, origin+path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if admin {
			req.AddCookie(&http.Cookie{Name: "aicrm_admin_session", Value: browser.SessionToken})
			req.AddCookie(&http.Cookie{Name: "aicrm_admin_csrf", Value: browser.CSRFToken})
			req.Header.Set("X-CSRF-Token", browser.CSRFToken)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s=%d want %d: %s", method, path, resp.StatusCode, want, data)
		}
		return data
	}
	pairNative := func(name string) string {
		var pair accessapp.RPAPairStart
		json.Unmarshal(nativeCall("POST", "/api/rpa/v1/pair/begin", map[string]string{"device_name": name}, "", false, 200), &pair)
		nativeCall("POST", "/api/admin/access/rpa-pair/approve", map[string]string{"user_code": pair.UserCode}, "", true, 200)
		return pair.DeviceCode
	}
	native, otherNative := pairNative("本机"), pairNative("其他Mac")
	var nativeView p.SequenceStepView
	json.Unmarshal(nativeCall("GET", "/api/rpa/v1/state", nil, native, false, 200), &nativeView)
	if nativeView.Step == nil || fmt.Sprint(nativeView.Step.PackageID) != packageID || nativeView.Step.PackageSHA256 != read.SHA256 || nativeView.Step.GroupCount != 2 || nativeView.Step.MessageCount != 2 || len(nativeView.Step.Materials) != 1 || nativeView.Step.Materials[0].SHA256 == "" {
		t.Fatalf("incomplete native manifest: %+v", nativeView.Step)
	}
	resume := map[string]any{"run_id": fmt.Sprint(frozen.ID), "revision": nativeView.Step.Revision, "group_index": 0, "message_index": 0}
	json.Unmarshal(nativeCall("POST", "/api/rpa/v1/resume", resume, native, false, 200), &nativeView)
	if nativeView.Step.PackageSHA256 != read.SHA256 {
		t.Fatal("mutation lost package binding")
	}
	nativeCall("POST", "/api/rpa/v1/resume", resume, native, false, 409)
	nativeCall("GET", "/api/rpa/v1/state", nil, otherNative, false, 403)
	request, _ := http.NewRequest("POST", origin+"/api/admin/automation-conversion/group-ops/rpa-package", strings.NewReader(`{"markdown":"unused"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", browser.CSRFToken)
	request.AddCookie(&http.Cookie{Name: "aicrm_admin_session", Value: browser.SessionToken})
	request.AddCookie(&http.Cookie{Name: "aicrm_admin_csrf", Value: browser.CSRFToken})
	response, e := server.Client().Do(request)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("browser package write accepted")
	}
	// Exact body, real terminal Bearer/CSRF and concurrent same-key replay.
	credential, e := crmcli.LoadCredential(origin)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]string{"markdown": string(canonical) + "\nnew note"})
	post := func(key string, payload []byte) int {
		req, _ := http.NewRequest("POST", origin+"/api/admin/automation-conversion/group-ops/rpa-package", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+credential.Token)
		req.Header.Set("X-CSRF-Token", credential.CSRF)
		req.Header.Set("Idempotency-Key", key)
		resp, err := server.Client().Do(req)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	invalid, _ := p.ParseTaskMarkdown(string(canonical))
	invalid.Groups[1] = "not-a-bound-group"
	invalidMarkdown, _ := p.RenderTaskMarkdown(invalid)
	invalidBody, _ := json.Marshal(map[string]string{"markdown": invalidMarkdown})
	if post("package-foreign-target", invalidBody) != 409 {
		t.Fatal("package accepted an arbitrary target")
	}
	if post("package-save-original", body) != 409 {
		t.Fatal("same key changed body accepted")
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- post("package-concurrent", body) }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("concurrent replay status=%d", code)
		}
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM group_ops_rpa_packages`).Scan(&count); e != nil || count != 2 {
		t.Fatal("duplicate packages", count, e)
	}
	nativeCall("POST", "/api/rpa/v1/prepare", map[string]any{"run_id": fmt.Sprint(frozen.ID), "revision": nativeView.Step.Revision, "group_index": 0, "message_index": 0}, native, false, 409)
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM outbound_rpa_intents`).Scan(&count); e != nil || count != 0 {
		t.Fatal("replaced activation created a send", count, e)
	}
	// Failure of the shared audit outbox must roll back package AND replacement.
	if _, e = pool.Exec(ctx, `CREATE FUNCTION reject_package_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture rollback'; END $$; CREATE TRIGGER reject_package_outbox BEFORE INSERT ON group_ops_outbox FOR EACH ROW EXECUTE FUNCTION reject_package_outbox()`); e != nil {
		t.Fatal(e)
	}
	if post("package-failed-atomic", body) != 503 {
		t.Fatal("outbox failure did not fail save")
	}
	if _, e = pool.Exec(ctx, `DROP TRIGGER reject_package_outbox ON group_ops_outbox; DROP FUNCTION reject_package_outbox()`); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM group_ops_rpa_packages`).Scan(&count); e != nil || count != 2 {
		t.Fatal("partial package commit")
	}
	var active int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM group_ops_navigation_runs WHERE state NOT IN ('completed','cancelled')`).Scan(&active); e != nil || active != 1 {
		t.Fatal("active execution was lost or duplicated")
	}
	application.Close()
	application, e = compose(ctx, runtime)
	if e != nil {
		t.Fatal(e)
	}
	server.Config.Handler = application.handler
	view = run(0, "", "group-ops", "rpa-package", "get", "--query", fmt.Sprintf("plan_id=%d", plan))
	if string(view["package"]) == "null" {
		t.Fatal("package lost after rebuilding application")
	}
}

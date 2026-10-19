package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/credential"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/crmcli"
	effectport "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
	testconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config/tests"
	platformpostgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
)

func TestCLIMaterialDependencyInstalled(t *testing.T) {
	if !testconfig.InstalledProductContractsRequested() {
		t.Skip("requires exact installed package inputs")
	}
	input := testconfig.InstalledProduct()
	validateOrdersOAuthInstalledProduct(t, input)
	exerciseCLIMaterialDependencyProcess(t, input, filepath.Join(input.Root, "bin", "aicrm-cli"))
}

func TestCLIMaterialDependencySourceProcess(t *testing.T) {
	binary := os.Getenv("AICRM_MATERIAL_SOURCE_BINARY")
	if binary == "" {
		t.Skip("explicit locally built source binary required")
	}
	raw, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	exerciseCLIMaterialDependencyProcess(t, testconfig.InstalledSurveyInputs{Binary: binary, Root: root, SHA: strings.Repeat("0", 39) + "1", BinarySHA256: fmt.Sprintf("%x", sha256.Sum256(raw))}, os.Getenv("AICRM_MATERIAL_SOURCE_CLI_BINARY"))
}

// Linux executes the packaged CLI process with a fixture-only CA. macOS uses
// the identical CLI entry point with the test transport: its system verifier
// intentionally ignores SSL_CERT_FILE. Neither path relaxes production TLS.
func exerciseCLIMaterialDependencyProcess(t *testing.T, input testconfig.InstalledSurveyInputs, cliBinary string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	dsn, cleanup := adminAccessCompositionDatabase(t, ctx)
	t.Cleanup(cleanup)
	native, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.Close)
	actor := surveyJourneyGovernedActor(t, ctx, native, "material-cli-owner", "Material CLI owner")
	passwordHash, err := (credential.PasswordHasher{}).Hash("isolated-survey-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = native.Exec(ctx, `UPDATE admin_users SET password_hash=$2 WHERE id=$1`, actor, passwordHash); err != nil {
		t.Fatal(err)
	}
	pool, err := platformpostgres.Wrap(native, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := platformpostgres.NewUnitOfWork(pool)
	if err != nil {
		t.Fatal(err)
	}
	materials := bindMaterialTransactionPorts(t, ctx, native, unit, actor, string(effectport.Hash("outbound.material.scope.v1", "wx-corp", "", "application_media_upload")))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	endpoint := "http://" + address
	backend, _ := url.Parse(endpoint)
	proxy := httputil.NewSingleHostReverseProxy(backend)
	direct := proxy.Director
	proxy.Director = func(r *http.Request) {
		direct(r)
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-For", "203.0.113.17")
	}
	bridge := httptest.NewUnstartedServer(proxy)
	bridge.StartTLS()
	t.Cleanup(bridge.Close)
	dataKey := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	process := startPeriodicContactInstalledProcess(t, input.Binary, input.Root, []string{
		"AICRM_DATABASE_URL=" + dsn, "AICRM_ROLE=api", "AICRM_LISTEN_ADDR=" + address, "AICRM_RELEASE_SHA=" + input.SHA,
		"AICRM_PUBLIC_ORIGIN=" + bridge.URL, "AICRM_WECOM_CORP_ID=wx-corp", "AICRM_WECOM_AGENT_ID=",
		"AICRM_SURVEY_DATA_KEY=" + dataKey, "AICRM_IDENTITY_PHONE_DATA_KEY=" + dataKey, "AICRM_COMMERCE_PUSH_PAYLOAD_DATA_KEY=" + dataKey,
		"AICRM_GROUP_OPS_WEBHOOK_SECRET=synthetic-material-cli-secret-32-bytes",
		"AICRM_WECOM_CONTEXT_SIGNING_KEY=synthetic-material-cli-key-32-bytes",
		"AICRM_OPEN_PLATFORM_TRUSTED_PROXY_CIDRS=127.0.0.1/32",
		"AICRM_OUTBOUND_PROVIDER_ENABLED=false", "AICRM_EFFECTS_PROVIDER_ENABLED=false",
		"HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY=127.0.0.1,localhost,::1",
	})
	waitInstalledSurveyReady(t, process, endpoint, input.SHA)
	dir := t.TempDir()
	credentialFile := filepath.Join(dir, "credential.json")
	caFile := filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AICRM_CLI_CONFIG_DIR", dir)
	t.Setenv("AICRM_CLI_CREDENTIAL_FILE", "")
	t.Setenv("AICRM_CLI_TOKEN", "")
	t.Setenv("AICRM_CLI_PASSWORD", "")
	oldTransport := http.DefaultTransport
	http.DefaultTransport = bridge.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	run := func(want int, body string, args ...string) map[string]any {
		t.Helper()
		var out, stderr bytes.Buffer
		var code int
		if runtime.GOOS == "linux" {
			if cliBinary == "" {
				t.Fatal("packaged CLI binary required on Linux")
			}
			cmd := exec.CommandContext(ctx, cliBinary, args...)
			cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+caFile, "SSL_CERT_DIR="+dir)
			cmd.Stdin = strings.NewReader(body)
			cmd.Stdout = &out
			cmd.Stderr = &stderr
			if e := cmd.Run(); e != nil {
				if exit, ok := e.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatal(e)
				}
			}
		} else {
			code = crmcli.Run(args, strings.NewReader(body), &out, &stderr)
		}
		if code != want {
			t.Fatalf("CLI %v code=%d want=%d output=%s stderr=%s", args, code, want, out.String(), stderr.String())
		}
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("CLI did not return a JSON result: %v", err)
		}
		return result
	}
	run(0, "", "profile", "add", "material-fixture", bridge.URL)
	run(0, "isolated-survey-admin-password", "auth", "login", "--username", "material-cli-owner", "--secret-stdin", "--secret-output", credentialFile)
	t.Setenv("AICRM_CLI_CREDENTIAL_FILE", credentialFile)
	run(0, "", "doctor")
	invoke := func(id string, body any, key string, args ...string) map[string]any {
		t.Helper()
		a := []string{"invoke", id}
		text := ""
		if body != nil {
			raw, e := json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
			text = string(raw)
			a = append(a, "--input", "-")
		}
		if key != "" {
			a = append(a, "--idempotency-key", key)
		}
		a = append(a, args...)
		r := run(0, text, a...)
		return r["data"].(map[string]any)
	}
	imageResult := invoke("admin.createMediaImage", map[string]any{"file_name": "cli-cover.png", "name": "CLI cover", "data_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(materialTransactionPNG(t))}, "cli-material-image")
	imageItem := imageResult["item"].(map[string]any)
	imageID := int64(imageItem["id"].(float64))
	miniResult := invoke("admin.createMediaMiniProgram", map[string]any{"name": "CLI card", "app_id": "wx-synthetic", "page_path": "pages/card", "title": "CLI card", "thumb_image_id": imageID}, "cli-material-mini")
	miniID := int64(miniResult["item"].(map[string]any)["id"].(float64))
	miniRead := invoke("admin.getMediaMiniProgram", nil, "", "--param", fmt.Sprintf("id=%d", miniID))
	if miniRead["item"].(map[string]any)["thumb_image_id"] != float64(imageID) {
		t.Fatal("CLI lost small-program cover reference")
	}
	local := invoke("admin.resolveLocalMiniProgramThumbnail", nil, "cli-material-local", "--param", fmt.Sprintf("id=%d", miniID))
	if local["real_external_call_executed"] != false || local["resolution"].(map[string]any)["status"] != "not_available" {
		t.Fatal("local compatibility resolution falsely claimed Provider readiness")
	}
	prepared := invoke("admin.prepareMediaPreparation", map[string]any{"force": true}, "cli-material-prepare", "--param", fmt.Sprintf("source_ref=image:%d", imageID))
	if prepared["material"].(map[string]any)["state"] != "queued" {
		t.Fatal("explicit CLI preparation did not accept its upload task")
	}
	// Execute only the synthetic uploader, never a real Provider or send Worker.
	materials.prepare(t, "image:"+strconv.FormatInt(imageID, 10), time.Now().UTC())
	status := invoke("admin.listMediaPreparations", nil, "")
	items := status["items"].([]any)
	ready := false
	for _, v := range items {
		m := v.(map[string]any)
		if m["source_ref"] == fmt.Sprintf("image:%d", imageID) {
			ready = m["credential_usable"] == true && m["credential_state"] == "ready"
		}
	}
	if !ready {
		t.Fatal("CLI preparation readback omitted the ready uploaded cover")
	}
	invite := invoke("admin.createMediaGroupInvite", map[string]any{"name": "CLI invite", "title": "Join", "join_url": "https://work.weixin.qq.com/gm/0123456789abcdef0123456789abcdef"}, "cli-material-invite")
	inviteID := int64(invite["item"].(map[string]any)["id"].(float64))
	content := invoke("admin.createGroupOpsContentPackage", map[string]any{"name": "CLI content", "content_text": "Synthetic content", "enabled": true, "refs": []map[string]any{{"kind": "miniprogram", "id": miniID}, {"kind": "image", "id": imageID}, {"kind": "group_invite", "id": inviteID}}}, "cli-material-content")
	packageID := int64(content["id"].(float64))
	binding := map[string]any{"campaign_code": "cli-material", "plan_id": "cli-plan", "package_id": packageID, "group_invite_id": inviteID}
	invoke("admin.bindGroupOpsContentPackage", binding, "cli-material-binding")
	read := invoke("admin.getGroupOpsContentPackageBinding", nil, "", "--param", "campaign_code=cli-material", "--param", "content_plan_id=cli-plan")
	if read["package_id"] != float64(packageID) || read["group_invite_id"] != float64(inviteID) {
		t.Fatalf("CLI saved binding cannot be reread: %+v", read)
	}
	var refs, uploads, sends int
	if err = native.QueryRow(ctx, `SELECT (SELECT count(*) FROM media_content_package_version_refs WHERE package_id=$1),(SELECT count(*) FROM outbound_material_preparations WHERE source_ref=$2),(SELECT count(*) FROM external_effects WHERE kind IN ('group_message','channel_welcome_message','outbound_message'))`, packageID, fmt.Sprintf("image:%d", imageID)).Scan(&refs, &uploads, &sends); err != nil || refs != 3 || uploads != 1 || sends != 0 {
		t.Fatalf("CLI persistence refs=%d uploads=%d sends=%d err=%v", refs, uploads, sends, err)
	}
	t.Logf("CLI material dependency app_sha=%s binary_sha256=%s cli_process=%t cover_uploaded=true mini_readback=true task_receipt=true content_binding_readback=true zero_real_provider_writes=true", input.SHA, input.BinarySHA256, runtime.GOOS == "linux")
}

package main

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	channel "github.com/qianlan33333-png/AI-CRM-standard/internal/channel"
	effectport "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
	mediaport "github.com/qianlan33333-png/AI-CRM-standard/internal/media/port"
	testconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config/tests"
	platformpostgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
	wecom "github.com/qianlan33333-png/AI-CRM-standard/internal/wecom"
)

// Exercise the real installed callback composition using synthetic signed XML
// and an isolated DB. No worker or real Provider network can execute.
func TestChannelConsentWelcomeInstalled(t *testing.T) {
	if !testconfig.InstalledProductContractsRequested() {
		t.Skip("requires exact installed package inputs")
	}
	input := testconfig.InstalledProduct()
	if input.Binary == "" {
		t.Fatal("missing installed binary")
	}
	validateOrdersOAuthInstalledProduct(t, input)
	exerciseChannelConsentWelcomeProcess(t, input)
}

// Source-process validation exercises the identical fixture locally while
// keeping its locally built bytes separate from installed-package evidence.
func TestChannelConsentWelcomeSourceProcess(t *testing.T) {
	binary := os.Getenv("AICRM_CONSENT_SOURCE_BINARY")
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
	digest := sha256.Sum256(raw)
	exerciseChannelConsentWelcomeProcess(t, testconfig.InstalledSurveyInputs{Binary: binary, Root: root, SHA: "0123456789abcdef0123456789abcdef01234567", BinarySHA256: fmt.Sprintf("%x", digest)})
}

func exerciseChannelConsentWelcomeProcess(t *testing.T, input testconfig.InstalledSurveyInputs) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	dsn, cleanup := adminAccessCompositionDatabase(t, ctx)
	t.Cleanup(cleanup)
	native, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.Close)
	pool, err := platformpostgres.Wrap(native, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := platformpostgres.NewUnitOfWork(pool)
	if err != nil {
		t.Fatal(err)
	}
	hmacKey := "12345678901234567890123456789012"
	digester, err := wecom.NewHMACStateDigester([]byte(hmacKey))
	if err != nil {
		t.Fatal(err)
	}
	adminID := surveyJourneyGovernedActor(t, ctx, native, "installed-consent-admin", "Installed consent administrator")
	materials := bindMaterialTransactionPorts(t, ctx, native, unit, adminID, string(effectport.Hash("outbound.material.scope.v1", "wx-corp", "", "application_media_upload")))
	if id := materials.image(t, "installed-welcome"); id != 1 {
		t.Fatalf("isolated material fixture image=%d want=1", id)
	}
	materials.prepare(t, "image:1", time.Now().UTC())
	ready := seedChannelWelcomeFixture(t, ctx, unit, channel.NewPostgreSQLStore(), digester, adminID, "installed-consent", "installed-consent-state", true, 1, 0)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	endpoint := "http://" + address
	aesKey := base64.RawStdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyz123456"))
	token := "synthetic-consent-token"
	dataKey := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	process := startPeriodicContactInstalledProcess(t, input.Binary, input.Root, []string{
		"AICRM_DATABASE_URL=" + dsn, "AICRM_ROLE=api", "AICRM_LISTEN_ADDR=" + address, "AICRM_RELEASE_SHA=" + input.SHA,
		"AICRM_SURVEY_DATA_KEY=" + dataKey, "AICRM_IDENTITY_PHONE_DATA_KEY=" + dataKey, "AICRM_COMMERCE_PUSH_PAYLOAD_DATA_KEY=" + dataKey,
		"AICRM_GROUP_OPS_WEBHOOK_SECRET=synthetic-consent-webhook-secret-32-bytes",
		"AICRM_WECOM_CONTEXT_SIGNING_KEY=synthetic-consent-context-key-32-bytes",
		"AICRM_PUBLIC_ORIGIN=https://installed.example.test", "AICRM_WECOM_CORP_ID=wx-corp", "AICRM_WECOM_CALLBACK_ENABLED=true",
		"AICRM_WECOM_CALLBACK_TOKEN=" + token, "AICRM_WECOM_CALLBACK_AES_KEY=" + aesKey, "AICRM_CHANNEL_STATE_HMAC_KEY=" + hmacKey,
		"AICRM_OUTBOUND_PROVIDER_ENABLED=false", "AICRM_EFFECTS_PROVIDER_ENABLED=false",
		"HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY=127.0.0.1,localhost,::1",
	})
	waitInstalledSurveyReady(t, process, endpoint, input.SHA)
	crypto, err := wecom.NewCallbackCrypto(token, aesKey, "wx-corp")
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	post := func(plain []byte) {
		t.Helper()
		encrypted, e := crypto.Encrypt(plain)
		if e != nil {
			t.Fatal(e)
		}
		timestamp, nonce := strconv.FormatInt(time.Now().Unix(), 10), "synthetic-consent-nonce"
		parts := []string{token, timestamp, nonce, encrypted}
		sort.Strings(parts)
		signature := fmt.Sprintf("%x", sha1.Sum([]byte(strings.Join(parts, ""))))
		query := url.Values{"msg_signature": {signature}, "timestamp": {timestamp}, "nonce": {nonce}}
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/wecom/events?"+query.Encode(), strings.NewReader("<xml><ToUserName>wx-corp</ToUserName><Encrypt><![CDATA["+encrypted+"]]></Encrypt></xml>"))
		if e != nil {
			t.Fatal(e)
		}
		response, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(io.LimitReader(response.Body, 8192))
		response.Body.Close()
		if e != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "Encrypt") {
			t.Fatalf("installed callback status=%d err=%v", response.StatusCode, e)
		}
	}
	post(channelWelcomePlaintext(ready.rawState))
	var original string
	if err = native.QueryRow(ctx, `SELECT callback_id FROM channel_welcome_intents WHERE channel_id=$1`, ready.resolution.Asset.ChannelID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	var initialState string
	var materialRaw []byte
	if err = native.QueryRow(ctx, `SELECT state,welcome_material_snapshot FROM channel_welcome_intents WHERE callback_id=$1`, original).Scan(&initialState, &materialRaw); err != nil {
		t.Fatal(err)
	}
	var materialSnapshot mediaport.GroupOpsMaterialSnapshot
	if initialState != "queued" || json.Unmarshal(materialRaw, &materialSnapshot) != nil || len(materialSnapshot.Attachments) != 1 || materialSnapshot.Attachments[0].MsgType != "image" || materialSnapshot.Attachments[0].MediaID != "synthetic-media-image:1" {
		t.Fatalf("installed callback failed real material capture: state=%s snapshot=%s", initialState, materialRaw)
	}
	// Synthetic setup of the known pre-consent rejection. The production
	// callback, correlation, intent/effect acceptance and replay are all binary.
	if _, err = native.Exec(ctx, `UPDATE channel_welcome_intents SET state='final_failed',result_reason='provider_rejected',provider_error_code=41051,attempt_count=1 WHERE callback_id=$1`, original); err != nil {
		t.Fatal(err)
	}
	approval := []byte(`<xml><ToUserName>wx-corp</ToUserName><CreateTime>1788336003</CreateTime><MsgType>event</MsgType><Event>change_external_contact</Event><ChangeType>msg_audit_approved</ChangeType><ExternalUserID>external-1</ExternalUserID><UserID>employee-1</UserID><WelcomeCode>installed-consent-fresh-code</WelcomeCode></xml>`)
	post(approval)
	post(approval)
	var count, effects int
	if err = native.QueryRow(ctx, `SELECT (SELECT count(*) FROM channel_welcome_intents WHERE channel_id=$1),(SELECT count(*) FROM external_effects WHERE kind='channel_welcome_message')`, ready.resolution.Asset.ChannelID).Scan(&count, &effects); err != nil || count != 2 || effects != 2 {
		t.Fatalf("installed consent intents=%d effects=%d err=%v", count, effects, err)
	}
	var state string
	if err = native.QueryRow(ctx, `SELECT state FROM channel_welcome_intents WHERE channel_id=$1 AND callback_id<>$2`, ready.resolution.Asset.ChannelID, original).Scan(&state); err != nil || state != "queued" {
		t.Fatalf("installed consent state=%s err=%v", state, err)
	}
	t.Logf("channel-consent-welcome-process app_sha=%s binary_sha256=%s signed_callback=true real_image_snapshot=true no_state_resolved=true replay_deduplicated=true zero_provider_writes=true", input.SHA, input.BinarySHA256)
}

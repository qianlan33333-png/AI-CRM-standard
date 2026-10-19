package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	accesshttp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/http"
	platformconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	productstore "github.com/qianlan33333-png/AI-CRM-standard/internal/product/store"
)

func TestPostgreSQLReferralCompatibleProductsChromiumJourney(t *testing.T) {
	if *domesticReleaseSmokeInputFile != "" {
		t.Skip("the exact installed candidate has a separate compatible-products Chromium journey")
	}
	if !platformconfig.ChromiumJourneyRequired() {
		t.Skip("set AICRM_REQUIRE_CHROMIUM_JOURNEY=1")
	}
	referralCompatibleProductsChromiumJourney(t, false)
}

func TestPostgreSQLReferralCompatibleProductsInstalledChromiumJourney(t *testing.T) {
	if *domesticReleaseSmokeInputFile == "" {
		t.Skip("requires the fixed exact-binary installed smoke input")
	}
	referralCompatibleProductsChromiumJourney(t, true)
}

func referralCompatibleProductsChromiumJourney(t *testing.T, installed bool) {
	t.Helper()
	f := newPromotionCheckoutFixture(t)
	defer f.close()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	t.Chdir(repository)
	screenshotDirectory := os.Getenv("AICRM_COMPATIBLE_BROWSER_SCREENSHOTS")
	if !installed {
		prepareProductExternalPushChromiumArtifacts(t, repository)
	}
	for _, raw := range os.Environ() {
		key, _, _ := strings.Cut(raw, "=")
		if strings.HasPrefix(key, "AICRM_") {
			t.Setenv(key, "")
		}
	}

	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	defer server.Close()
	origin := "https://" + server.Listener.Addr().String()
	dataKey := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	config := platformconfig.Runtime{
		Role: platformconfig.RoleAPI, DatabaseURL: f.pool.Config().ConnConfig.ConnString(),
		PublicOrigin: origin, H5PublicOrigin: origin,
		ReleaseSHA: "referral-compatible-products-chromium", WorkerOwner: "referral-compatible-products-chromium", WorkerLimit: 1,
		Referral:  platformconfig.Referral{TokenDataKey: dataKey},
		GroupOps:  platformconfig.GroupOps{WebhookSecret: "synthetic-compatible-products-webhook"},
		Survey:    platformconfig.Survey{DataKey: dataKey, IdentityPhoneDataKey: dataKey},
		WeChatPay: platformconfig.WeChatPay{AppID: "wx-promotion-checkout", AppScope: "wechat-app:promotion-checkout"},
		Bootstrap: platformconfig.Bootstrap{Enabled: true, Username: "compatible-products-owner", Password: "compatible-products-owner-password", DisplayName: "兼容商品管理员"},
	}
	application, err := compose(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	if err = application.bootstrap(ctx, config.Bootstrap); err != nil {
		t.Fatal(err)
	}

	productRepository, err := productstore.NewPostgreSQL(f.pool, f.uow)
	if err != nil {
		t.Fatal(err)
	}
	shopProducts := make([]int64, 0, 2)
	for index, name := range []string{"小店兼容课一", "小店兼容课二"} {
		var id productport.ID
		err = f.uow.Within(ctx, func(tx context.Context) error {
			var importErr error
			id, _, importErr = productRepository.ImportShopSKUWithin(tx, productport.ShopSKU{
				AppID: "wx-compatible-admin", ProductID: "compatible-parent", SKUID: []string{"compatible-sku-one", "compatible-sku-two"}[index],
				Name: name, PriceMinor: 680,
			}, "referral-compatible-products-browser", time.Now().UTC())
			return importErr
		})
		if err != nil {
			t.Fatalf("seed WeChat Shop compatible product %q: %v", name, err)
		}
		shopProducts = append(shopProducts, int64(id))
	}

	if installed {
		preparePaidReferralInstalledDirectory(t, f, []int64{f.buyer, f.promoter.CustomerID, f.otherPromoter.CustomerID})
		inputs, binary, root := referralInstalledArtifact(t)
		listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			t.Fatal(listenErr)
		}
		address := listener.Addr().String()
		_ = listener.Close()
		environment := paidReferralInstalledEnvironment(f.pool.Config().ConnConfig.ConnString(), inputs.InstalledSHA, address)
		for i, raw := range environment {
			key, _, _ := strings.Cut(raw, "=")
			if key == "AICRM_PUBLIC_ORIGIN" || key == "AICRM_H5_PUBLIC_ORIGIN" {
				environment[i] = key + "=" + origin
			}
		}
		process := startDomesticSmokeProcess(t, binary, root, environment)
		// Stop the installed API before deferred fixture pools/clone cleanup.
		defer func() {
			if process.finished {
				return
			}
			_ = process.command.Process.Signal(os.Interrupt)
			select {
			case processErr := <-process.done:
				process.finished = true
				if processErr != nil {
					t.Errorf("stop installed API: %v", processErr)
				}
			case <-time.After(8 * time.Second):
				_ = process.command.Process.Kill()
				<-process.done
				process.finished = true
				t.Error("installed API stop timeout")
			}
		}()
		waitDomesticReleaseReady(t, process, "http://"+address, inputs.InstalledSHA)
		target, parseErr := url.Parse("http://" + address)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		server.Config.Handler = httputil.NewSingleHostReverseProxy(target)
		t.Logf("referral_compatible_products_installed: source=%s installed=%s", inputs.SourceSHA, inputs.InstalledSHA)
	} else {
		server.Config.Handler = application.handler
	}
	server.StartTLS()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	session, csrf := compatibleProductsAdminLogin(t, ctx, client, origin, config.Bootstrap.Username, config.Bootstrap.Password)
	do := func(method, path string, body any, key string) (int, map[string]any, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			encoded, encodeErr := json.Marshal(body)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			reader = strings.NewReader(string(encoded))
		}
		request, requestErr := http.NewRequestWithContext(ctx, method, origin+path, reader)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Origin", origin)
		request.AddCookie(&http.Cookie{Name: accesshttp.SessionCookieName, Value: session})
		request.AddCookie(&http.Cookie{Name: accesshttp.CSRFCookieName, Value: csrf})
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
			request.Header.Set("X-CSRF-Token", csrf)
		}
		response, doErr := client.Do(request)
		if doErr != nil {
			t.Fatal(doErr)
		}
		defer response.Body.Close()
		data, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		value := map[string]any{}
		if len(data) > 0 {
			if decodeErr := json.Unmarshal(data, &value); decodeErr != nil {
				t.Fatalf("decode %s %s response: %v body=%s", method, path, decodeErr, data)
			}
		}
		return response.StatusCode, value, data
	}

	status, page, pageBody := do(http.MethodGet, "/api/admin/referral/product-options?purpose=compatible_purchase&offset=0&limit=50", nil, "")
	if status != http.StatusOK {
		t.Fatalf("compatible product page status=%d body=%s", status, pageBody)
	}
	if _, exists := page["source_kind"]; exists {
		t.Fatalf("product page must use the ProductOptionPage shape without a top-level source_kind: %s", pageBody)
	}
	items, ok := page["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("compatible page should contain exactly the two Shop products, got %s", pageBody)
	}
	for _, value := range items {
		item, _ := value.(map[string]any)
		if item["source_kind"] != "wechat_shop" || item["product_type"] != "standard" {
			t.Fatalf("unexpected compatible product projection: %v", item)
		}
	}
	status, single, singleBody := do(http.MethodGet, "/api/admin/referral/product-options?product_id="+strconv.FormatInt(shopProducts[0], 10)+"&product_type=standard&purpose=compatible_purchase", nil, "")
	if status != http.StatusOK || single["source_kind"] != "wechat_shop" || single["product_type"] != "standard" || int64(single["id"].(float64)) != shopProducts[0] {
		t.Fatalf("compatible single product must be a direct ProductOption: status=%d body=%s", status, singleBody)
	}
	if _, exists := single["items"]; exists {
		t.Fatalf("compatible single product was incorrectly wrapped as a page: %s", singleBody)
	}

	now := time.Now().UTC()
	startsAt := now.Add(-time.Hour).Format(time.RFC3339)
	endsAt := now.Add(24 * time.Hour).Format(time.RFC3339)
	create := map[string]any{
		"name": "小店购买资格兼容管理旅程", "starts_at": startsAt, "ends_at": endsAt,
		"team_mode": "individual", "qualification_mode": "product_purchase", "product_id": f.productID,
		"product_type": "standard_product", "leaderboard_metric": "invites", "compatible_product_ids": []int64{shopProducts[0]},
	}
	status, campaign, responseBody := do(http.MethodPost, "/api/admin/referral/campaigns", create, "compatible-products-campaign-create")
	if status != http.StatusCreated {
		t.Fatalf("create campaign=%d body=%s", status, responseBody)
	}
	campaignID := int64(campaign["id"].(float64))
	version := int64(campaign["version"].(float64))
	if !compatibleProductIDsEqual(campaign["compatible_product_ids"], []int64{shopProducts[0]}) {
		t.Fatalf("create did not persist initial Shop compatibility: %s", responseBody)
	}

	// An older client omitting the newly optional field preserves the current list.
	legacyUpdate := compatibleProductCampaignUpdate(campaign, version, nil)
	status, campaign, responseBody = do(http.MethodPut, "/api/admin/referral/campaigns/"+strconv.FormatInt(campaignID, 10), legacyUpdate, "compatible-products-legacy-omission")
	if status != http.StatusOK || !compatibleProductIDsEqual(campaign["compatible_product_ids"], []int64{shopProducts[0]}) {
		t.Fatalf("omitted compatible_product_ids should retain existing IDs: status=%d body=%s", status, responseBody)
	}

	// Empty is an explicit clear, while a normal active-campaign state transition
	// proves the campaign goes through its real admin lifecycle before UI edits.
	status, campaign, responseBody = do(http.MethodPut, "/api/admin/referral/campaigns/"+strconv.FormatInt(campaignID, 10), compatibleProductCampaignUpdate(campaign, int64(campaign["version"].(float64)), []int64{}), "compatible-products-clear")
	if status != http.StatusOK || !compatibleProductIDsEqual(campaign["compatible_product_ids"], []int64{}) {
		t.Fatalf("empty compatible_product_ids should clear the list: status=%d body=%s", status, responseBody)
	}
	status, campaign, responseBody = do(http.MethodPost, "/api/admin/referral/campaigns/"+strconv.FormatInt(campaignID, 10)+"/state", map[string]any{"expected_version": campaign["version"], "target": "active"}, "compatible-products-activate")
	if status != http.StatusOK || campaign["state"] != "active" {
		t.Fatalf("activate campaign=%d body=%s", status, responseBody)
	}
	version = int64(campaign["version"].(float64))
	status, _, responseBody = do(http.MethodPut, "/api/admin/referral/campaigns/"+strconv.FormatInt(campaignID, 10), compatibleProductCampaignUpdate(campaign, version, []int64{f.productID}), "compatible-products-native-rejected")
	if status != http.StatusBadRequest {
		t.Fatalf("unsynced non-Shop product should be rejected, status=%d body=%s", status, responseBody)
	}
	status, campaign, responseBody = do(http.MethodPut, "/api/admin/referral/campaigns/"+strconv.FormatInt(campaignID, 10), compatibleProductCampaignUpdate(campaign, version, []int64{shopProducts[0]}), "compatible-products-active-edit")
	if status != http.StatusOK || !compatibleProductIDsEqual(campaign["compatible_product_ids"], []int64{shopProducts[0]}) {
		t.Fatalf("active campaign must permit compatibility-list edits: status=%d body=%s", status, responseBody)
	}

	journey := filepath.Join(filepath.Dir(sourceFile), "referral_compatible_products_chromium_journey.mjs")
	command := exec.CommandContext(ctx, "node", journey)
	command.Env = append(os.Environ(),
		"AICRM_REFERRAL_ADMIN_BROWSER_URL="+origin,
		"AICRM_REFERRAL_ADMIN_BROWSER_CAMPAIGN="+strconv.FormatInt(campaignID, 10),
		"AICRM_REFERRAL_ADMIN_BROWSER_SAVED_PRODUCT="+strconv.FormatInt(shopProducts[0], 10),
		"AICRM_REFERRAL_ADMIN_BROWSER_ADD_PRODUCT="+strconv.FormatInt(shopProducts[1], 10),
		"AICRM_REFERRAL_ADMIN_BROWSER_SESSION="+session,
		"AICRM_REFERRAL_ADMIN_BROWSER_CSRF="+csrf,
	)
	if screenshotDirectory != "" {
		command.Env = append(command.Env, "AICRM_COMPATIBLE_BROWSER_SCREENSHOTS="+screenshotDirectory)
	}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "referral_compatible_products_chromium: PASS") {
		t.Fatalf("compatible-products Chromium journey err=%v output=%s", err, strings.TrimSpace(string(output)))
	}
	t.Logf("%s", strings.TrimSpace(string(output)))
	status, campaign, responseBody = do(http.MethodGet, "/api/admin/referral/campaigns/"+strconv.FormatInt(campaignID, 10), nil, "")
	if status != http.StatusOK || !compatibleProductIDsEqual(campaign["compatible_product_ids"], []int64{shopProducts[1]}) {
		t.Fatalf("browser save did not persist and reload the second Shop product: status=%d body=%s", status, responseBody)
	}
	t.Log("referral_compatible_products_admin_browser: HTTP omission/clear/Shop-only/CAS contracts and Chromium save-refresh passed")
}

func compatibleProductCampaignUpdate(current map[string]any, version int64, compatibleIDs []int64) map[string]any {
	body := map[string]any{
		"expected_version": version,
		"name":             current["name"], "cover_url": current["cover_url"], "description": current["description"], "reward_rules": current["reward_rules"],
		"starts_at": current["starts_at"], "ends_at": current["ends_at"], "team_mode": current["team_mode"],
		"qualification_mode": current["qualification_mode"], "product_id": current["product_id"], "product_type": current["product_type"], "leaderboard_metric": current["leaderboard_metric"],
	}
	if compatibleIDs != nil {
		body["compatible_product_ids"] = compatibleIDs
	}
	return body
}

func compatibleProductIDsEqual(raw any, want []int64) bool {
	items, ok := raw.([]any)
	if !ok || len(items) != len(want) {
		return false
	}
	for index, value := range items {
		numeric, ok := value.(float64)
		if !ok || int64(numeric) != want[index] {
			return false
		}
	}
	return true
}

func compatibleProductsAdminLogin(t *testing.T, ctx context.Context, client *http.Client, origin, username, password string) (string, string) {
	t.Helper()
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	pageBytes, err := io.ReadAll(page.Body)
	page.Body.Close()
	if err != nil || page.StatusCode != http.StatusOK {
		t.Fatalf("admin login page status=%d err=%v body=%s", page.StatusCode, err, pageBytes)
	}
	var loginCSRF *http.Cookie
	for _, cookie := range page.Cookies() {
		if cookie.Name == accesshttp.LoginCSRFCookieName {
			loginCSRF = cookie
		}
	}
	match := regexp.MustCompile(`name="login_csrf_token" value="([^"]+)"`).FindSubmatch(pageBytes)
	if loginCSRF == nil || len(match) != 2 {
		t.Fatalf("login page missed CSRF cookie/token: %s", pageBytes)
	}
	form := url.Values{"username": {username}, "password": {password}, "login_csrf_token": {string(match[1])}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(loginCSRF)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin login status=%d", response.StatusCode)
	}
	var session, csrf string
	for _, cookie := range response.Cookies() {
		switch cookie.Name {
		case accesshttp.SessionCookieName:
			session = cookie.Value
		case accesshttp.CSRFCookieName:
			csrf = cookie.Value
		}
	}
	if session == "" || csrf == "" {
		t.Fatalf("successful admin login missed session/CSRF cookie")
	}
	return session, csrf
}

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	distributionapp "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/app"
	platformconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
	referralport "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/port"
)

// Reproduces the reported order shape in an isolated database: native paid
// self-purchase of the bound product, no activity context, no participation.
// Runs the same HTTP contract against the exact installed binary when supplied.
func TestPostgreSQLReferralPurchasedEntryHTTPJourney(t *testing.T) {
	for _, channel := range []string{"mini_program", "h5_official_account"} {
		t.Run(channel, func(t *testing.T) { referralPurchasedEntryJourney(t, false, channel) })
	}
}
func TestPostgreSQLReferralPurchasedEntryChromiumJourney(t *testing.T) {
	referralPurchasedEntryJourney(t, true, "h5_official_account")
}
func referralPurchasedEntryJourney(t *testing.T, browser bool, channel string) {
	screenshotDir := os.Getenv("AICRM_PURCHASED_BROWSER_SCREENSHOTS")
	identityKind := "mp_openid"
	if channel == "h5_official_account" {
		identityKind = "oa_openid"
	}
	f := newPromotionCheckoutFixture(t)
	defer f.close()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	t.Chdir(repository)
	if browser && *domesticReleaseSmokeInputFile == "" {
		prepareProductExternalPushChromiumArtifacts(t, repository)
	}
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	defer server.Close()
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if strings.HasPrefix(k, "AICRM_") {
			t.Setenv(k, "")
		}
	}
	environment := paidReferralInstalledEnvironment(f.pool.Config().ConnConfig.ConnString(), strings.Repeat("a", 40), "127.0.0.1:18081")
	for _, v := range environment {
		k, v, _ := strings.Cut(v, "=")
		t.Setenv(k, v)
	}
	cfg, err := platformconfig.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.PublicOrigin = "https://" + server.Listener.Addr().String()
	cfg.H5PublicOrigin = cfg.PublicOrigin
	application, err := compose(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	var identityID int64
	now := time.Now().UTC()
	if err = f.pool.QueryRow(ctx, `INSERT INTO customer_identities(customer_id,kind,scope_key,normalized_value,assurance,source,normalizer_version,verified_at) VALUES($1,$3,'wechat-app:promotion-checkout','purchased-entry-owner','verified','synthetic-test',1,$2) RETURNING id`, f.buyer, now, identityKind).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	var orderID int64
	merchant := "v3pay_4KT4NC4OT2SIVU5YYRBRBOBYJU"
	paidAt := now.Add(-time.Hour)
	if err = f.pool.QueryRow(ctx, `INSERT INTO orders(provider,source_system,source_key,merchant_order_no,payer_customer_id,beneficiary_customer_id,amount_minor,currency,status,record_origin,effect_eligible,version,created_at,updated_at) VALUES('wechat_pay','v3-checkout',$1,$1,$2,$2,680,'CNY','paid','native',true,2,$3,$3) RETURNING id`, merchant, f.buyer, paidAt).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO order_items(order_id,line_no,product_id,product_version,product_code,product_name,unit_amount_minor,quantity,line_amount_minor) VALUES($1,1,$2,1,'promotion-checkout-product','分销归因商品',680,1,680)`, orderID, f.productID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO order_checkout_snapshots(order_id,product_type,product_id,product_code,product_name,product_version,service_period_duration_days,gross_amount_minor,discount_amount_minor,payable_amount_minor,currency,coupon_applied,coupon_reservation_ref,reserved_at,created_at) VALUES($1,'standard_product',$2,'promotion-checkout-product','分销归因商品',1,0,680,0,680,'CNY',false,'',$3,$3)`, orderID, f.productID, paidAt); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("synthetic-purchased-entry-paid"))
	if _, err = f.pool.Exec(ctx, `INSERT INTO order_paid_events(order_id,order_version,source_digest,occurred_at) VALUES($1,2,$2,$3)`, orderID, digest[:], paidAt); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO payments(order_id,provider,payment_channel,merchant_order_no,payer_identity_id,payer_customer_id,beneficiary_customer_id,amount_minor,currency,status,profit_sharing_marked,version,paid_confirmed_at,created_at,updated_at) VALUES($1,'wechat_pay',$6,$2,$3,$4,$4,680,'CNY','paid',false,1,$5,$5,$5)`, orderID, merchant, identityID, f.buyer, paidAt, channel); err != nil {
		t.Fatal(err)
	}
	var campaignID int64
	if err = f.pool.QueryRow(ctx, `INSERT INTO referral_campaigns(name,state,starts_at,ends_at,version,created_by,created_at,updated_at,team_mode,qualification_mode,product_id,product_type) VALUES('6.8 已购回归','active',$1,$2,1,0,$3,$3,'individual','product_purchase',$4,'standard_product') RETURNING id`, now.Add(-time.Hour), now.Add(time.Hour), now, f.productID).Scan(&campaignID); err != nil {
		t.Fatal(err)
	}
	// A pre-existing referrer must survive the organic paid-entry recovery.
	var oldInvitation int64
	oldDigest := sha256.Sum256([]byte("synthetic-existing-invitation"))
	if err = f.pool.QueryRow(ctx, `INSERT INTO referral_invitations(campaign_id,inviter_customer_id,token_digest,state,created_at,expires_at) VALUES($1,$2,$3,'active',$4,$5) RETURNING id`, campaignID, f.promoter.CustomerID, oldDigest[:], now, now.Add(time.Hour)).Scan(&oldInvitation); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO referral_current_relationships(customer_id,referrer_customer_id,source_campaign_id,invitation_id,version,effective_at) VALUES($1,$2,$3,$4,1,$5)`, f.buyer, f.promoter.CustomerID, campaignID, oldInvitation, now); err != nil {
		t.Fatal(err)
	}
	sessions, err := distributionapp.NewBrowserSessionService(f.uow, f.distributions)
	if err != nil {
		t.Fatal(err)
	}
	issue := func(customer, identity int64) string {
		t.Helper()
		v, _, e := sessions.Issue(ctx, referralport.TrustedSessionActor{CustomerID: customer, IdentityID: identity, Channel: channel, AppID: "wx-promotion-checkout", AppScope: "wechat-app:promotion-checkout", OccurredAt: now})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	ownerSession := issue(f.buyer, identityID)
	var otherCustomer, otherIdentity int64
	if err = f.pool.QueryRow(ctx, `INSERT INTO customers DEFAULT VALUES RETURNING id`).Scan(&otherCustomer); err != nil {
		t.Fatal(err)
	}
	if err = f.pool.QueryRow(ctx, `INSERT INTO customer_identities(customer_id,kind,scope_key,normalized_value,assurance,source,normalizer_version,verified_at) VALUES($1,$3,'wechat-app:promotion-checkout','purchased-entry-unpaid','verified','synthetic-test',1,$2) RETURNING id`, otherCustomer, now, identityKind).Scan(&otherIdentity); err != nil {
		t.Fatal(err)
	}
	otherSession := issue(otherCustomer, otherIdentity)
	server.Config.Handler = application.handler
	if *domesticReleaseSmokeInputFile != "" {
		preparePaidReferralInstalledDirectory(t, f, []int64{f.buyer, f.promoter.CustomerID, f.otherPromoter.CustomerID, otherCustomer})
		inputs, binary, root := referralInstalledArtifact(t)
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		address := listener.Addr().String()
		listener.Close()
		installedEnvironment := paidReferralInstalledEnvironment(f.pool.Config().ConnConfig.ConnString(), inputs.InstalledSHA, address)
		for i, v := range installedEnvironment {
			k, _, _ := strings.Cut(v, "=")
			if k == "AICRM_PUBLIC_ORIGIN" || k == "AICRM_H5_PUBLIC_ORIGIN" {
				installedEnvironment[i] = k + "=" + cfg.PublicOrigin
			}
		}
		process := startDomesticSmokeProcess(t, binary, root, installedEnvironment)
		defer func() {
			if !process.finished {
				_ = process.command.Process.Signal(os.Interrupt)
				select {
				case e := <-process.done:
					process.finished = true
					if e != nil {
						t.Error(e)
					}
				case <-time.After(8 * time.Second):
					_ = process.command.Process.Kill()
					<-process.done
					process.finished = true
					t.Error("installed API stop timeout")
				}
			}
		}()
		waitDomesticReleaseReady(t, process, "http://"+address, inputs.InstalledSHA)
		target, _ := url.Parse("http://" + address)
		server.Config.Handler = httputil.NewSingleHostReverseProxy(target)
		t.Logf("purchased_entry_installed: source=%s installed=%s", inputs.SourceSHA, inputs.InstalledSHA)
	}
	server.StartTLS()
	base := server.URL
	client := server.Client()
	client.Timeout = 10 * time.Second
	path := fmt.Sprintf("/api/v1/referral/campaigns/%d", campaignID)
	request := func(method, suffix, session, key string, csrf bool) (int, map[string]any, error) {
		var body io.Reader
		if method == http.MethodPost {
			body = strings.NewReader(`{}`)
		}
		req, e := http.NewRequest(method, base+path+suffix, body)
		if e != nil {
			return 0, nil, e
		}
		req.AddCookie(&http.Cookie{Name: "aicrm_distribution_session", Value: session})
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", cfg.PublicOrigin)
			req.Header.Set("Idempotency-Key", key)
			if csrf {
				req.AddCookie(&http.Cookie{Name: "aicrm_distribution_csrf", Value: "purchased-entry-csrf"})
				req.Header.Set("X-Distribution-CSRF", "purchased-entry-csrf")
			}
		}
		res, e := client.Do(req)
		if e != nil {
			return 0, nil, e
		}
		defer res.Body.Close()
		var v map[string]any
		e = json.NewDecoder(res.Body).Decode(&v)
		return res.StatusCode, v, e
	}
	must := func(method, suffix, session, key string, status int) map[string]any {
		t.Helper()
		code, v, e := request(method, suffix, session, key, true)
		if e != nil || code != status {
			t.Fatalf("%s %s code=%d want=%d body=%v err=%v", method, suffix, code, status, v, e)
		}
		return v
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var v int
		if e := f.pool.QueryRow(ctx, query, args...).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	for range 2 {
		mine := must("GET", "/me", ownerSession, "", 200)
		if mine["purchase_eligible"] != true || mine["participation"] != nil {
			t.Fatalf("paid entry not recognized: %v", mine)
		}
	}
	if count(`SELECT count(*) FROM referral_participations WHERE campaign_id=$1`, campaignID) != 0 {
		t.Fatal("GET created participation")
	}
	if mine := must("GET", "/me", otherSession, "", 200); mine["purchase_eligible"] != false {
		t.Fatal("another customer's order grants access", mine)
	}
	must("POST", "/participations", otherSession, "purchased-entry-unpaid", 403)
	var wrongCampaign int64
	if err = f.pool.QueryRow(ctx, `INSERT INTO referral_campaigns(name,state,starts_at,ends_at,version,created_by,created_at,updated_at,team_mode,qualification_mode,product_id,product_type) VALUES('其他商品','active',$1,$2,1,0,$3,$3,'individual','product_purchase',$4,'standard_product') RETURNING id`, now.Add(-time.Hour), now.Add(time.Hour), now, f.productID+1000000).Scan(&wrongCampaign); err != nil {
		t.Fatal(err)
	}
	originalPath := path
	path = fmt.Sprintf("/api/v1/referral/campaigns/%d", wrongCampaign)
	if mine := must("GET", "/me", ownerSession, "", 200); mine["purchase_eligible"] != false {
		t.Fatal("wrong product grants access", mine)
	}
	must("POST", "/participations", ownerSession, "purchased-entry-wrong-product", 403)
	path = originalPath
	// No authentication and no CSRF must not be enough to join.
	must("GET", "/me", "", "", 401)
	if code, _, _ := request("POST", "/participations", ownerSession, "purchased-no-csrf", false); code != 403 {
		t.Fatalf("missing CSRF accepted: %d", code)
	}
	// Eligibility changed after page read: the existing POST check rejects it.
	if _, err = f.pool.Exec(ctx, `INSERT INTO payment_refunds(payment_id,provider,refund_no,amount_minor,reason,status,version,created_at,updated_at) SELECT id,'wechat_pay','purchased-entry-refund',680,'synthetic','requested',1,$2,$2 FROM payments WHERE order_id=$1`, orderID, now); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"requested", "completed"} {
		if _, err = f.pool.Exec(ctx, `UPDATE payment_refunds SET status=$1 WHERE refund_no='purchased-entry-refund'`, state); err != nil {
			t.Fatal(err)
		}
		if mine := must("GET", "/me", ownerSession, "", 200); mine["purchase_eligible"] != false {
			t.Fatal("refund grants eligibility", state, mine)
		}
		must("POST", "/participations", ownerSession, "purchased-entry-refunded-"+state, 403)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE payment_refunds SET status='final_failed' WHERE refund_no='purchased-entry-refund'`); err != nil {
		t.Fatal(err)
	}
	if browser {
		poster := referralBrowserPosterFixture(t)
		posterDigest := sha256.Sum256(poster)
		if _, err = f.pool.Exec(ctx, `INSERT INTO referral_campaign_posters(campaign_id,slot,source_image_id,description,media_type,image_bytes,image_sha256,published_at,image_width,image_height) VALUES($1,1,1,'个人海报','image/png',$2,$3,$4,1250,2426)`, campaignID, poster, posterDigest[:], now); err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(ctx, "node", filepath.Join(repository, "cmd/aicrm/referral_purchased_entry_chromium_journey.mjs"))
		command.Env = append(os.Environ(), "AICRM_PURCHASED_BROWSER_URL="+base, "AICRM_PURCHASED_BROWSER_SESSION="+ownerSession, fmt.Sprintf("AICRM_PURCHASED_BROWSER_CAMPAIGN=%d", campaignID), "AICRM_PURCHASED_BROWSER_SCREENSHOTS="+screenshotDir)
		output, e := command.CombinedOutput()
		if e != nil {
			t.Fatalf("purchased browser: %v %s", e, output)
		}
		t.Log(string(output))
	}
	// Both sessions have independent trusted ownership; no customer IDs in bodies.
	codes := make(chan int, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, _, e := request("POST", "/participations", ownerSession, fmt.Sprintf("purchased-entry-repeat-%d", i), true)
			if e != nil {
				code = 0
			}
			codes <- code
		}(i)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("concurrent join status=%d", code)
		}
	}
	if count(`SELECT count(*) FROM referral_participations WHERE campaign_id=$1 AND customer_id=$2`, campaignID, f.buyer) != 1 {
		t.Fatal("duplicate participation")
	}
	mine := must("GET", "/me", ownerSession, "", 200)
	if mine["participation"] == nil || mine["invitation_available"] != true {
		t.Fatal("readback missing participant", mine)
	}
	link := must("POST", "/invite", ownerSession, "purchased-entry-invite", 200)
	if !strings.Contains(fmt.Sprint(link["url"]), "/referral/invite/rfi_") {
		t.Fatal("no personal invitation", link)
	}
	for _, query := range []string{`SELECT count(*) FROM referral_score_events WHERE campaign_id=$1`, `SELECT count(*) FROM referral_product_sale_events WHERE campaign_id=$1`, `SELECT count(*) FROM referral_relationship_history WHERE source_campaign_id=$1`} {
		if count(query, campaignID) != 0 {
			t.Fatal("recovery changed historical attribution", query)
		}
	}
	if count(`SELECT count(*) FROM referral_current_relationships WHERE customer_id=$1 AND referrer_customer_id=$2 AND version=1`, f.buyer, f.promoter.CustomerID) != 1 {
		t.Fatal("existing referrer changed")
	}
	if count(`SELECT count(*) FROM order_paid_events WHERE order_id=$1`, orderID) != 1 || count(`SELECT count(*) FROM referral_product_sale_checkouts WHERE order_id=$1`, orderID) != 0 {
		t.Fatal("old payment/context was replayed")
	}
	if count(`SELECT count(*) FROM payment_profit_sharing_provider_intents`) != 0 {
		t.Fatal("recovery created Provider writes")
	}
	t.Log("reported-order shape: paid/no context -> eligible read -> CSRF/idempotent join -> personal invitation; refunds/ownership/history retained")
}

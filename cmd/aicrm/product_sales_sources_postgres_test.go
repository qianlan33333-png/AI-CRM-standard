package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	accesshttp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/http"
	effectport "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
	paymentapp "github.com/qianlan33333-png/AI-CRM-standard/internal/payment/app"
	paymentport "github.com/qianlan33333-png/AI-CRM-standard/internal/payment/port"
	paymentprovider "github.com/qianlan33333-png/AI-CRM-standard/internal/payment/provider"
	testconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config/tests"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"github.com/xuri/excelize/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPostgreSQLProductSalesSourcesTransactions(t *testing.T) {
	testProductSalesSourcesTransactions(t, false)
}

func TestProductSalesSourcesInstalled(t *testing.T) {
	if !testconfig.InstalledProductContractsRequested() {
		t.Skip("explicit installed Product package required")
	}
	if testconfig.InstalledProduct().Binary == "" {
		t.Fatal("installed Product package inputs are incomplete")
	}
	testProductSalesSourcesTransactions(t, true)
}

func testProductSalesSourcesTransactions(t *testing.T, installed bool) {
	f := newProductExternalPushChromiumFixtureWithOptions(t, 220*time.Second, productExternalPushChromiumFixtureOptions{enablePublicH5: true, enableAlipay: true, deferEffectsWorker: true})
	if installed {
		useInstalledPeriodicContactFixture(t, f)
	}
	// The source application only seeds synthetic identities/sessions and paid
	// callback facts. Every tested business HTTP request uses the TLS server;
	// installed mode proxies that server to the immutable package process.
	handler := salesSourceFixtureHTTP(t, f)
	t.Logf("sales_sources_business_http installed_binary=%t", installed)
	pool := f.application.pool.Native()
	owner, csrf := adminAccessLogin(t, f.application.handler, "product-browser-owner", "product-browser-owner-password")
	admin := func(method, path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", key)
		r.AddCookie(&http.Cookie{Name: accesshttp.SessionCookieName, Value: owner})
		r.AddCookie(&http.Cookie{Name: accesshttp.CSRFCookieName, Value: csrf})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	sourcesPath := fmt.Sprintf("/api/admin/wechat-pay/products/%d/sales-sources", f.productID)
	if denied := automationFixedContentRequest(t, handler, "GET", sourcesPath, nil, "", "", ""); denied.Code != http.StatusUnauthorized {
		t.Fatalf("source list without session: %d", denied.Code)
	}
	if denied := automationFixedContentRequest(t, handler, "POST", sourcesPath, []byte(`{"name":"forbidden"}`), owner, "", "source-no-csrf-0001"); denied.Code != http.StatusForbidden {
		t.Fatalf("source mutation without CSRF: %d", denied.Code)
	}
	create := func(id int64, period bool, name string) productport.SalesSource {
		kind := "wechat-pay/products"
		if period {
			kind = "service-period-products"
		}
		path := fmt.Sprintf("/api/admin/%s/%d/sales-sources", kind, id)
		body, _ := json.Marshal(map[string]string{"name": name})
		key := fmt.Sprintf("source-create-%d-%s", id, name)
		w := admin("POST", path, string(body), key)
		var v productport.SalesSource
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.ID < 1 {
			t.Fatalf("create source %d %s", w.Code, w.Body)
		}
		if replay := admin("POST", path, string(body), key); !bytes.Equal(replay.Body.Bytes(), w.Body.Bytes()) {
			t.Fatal("source create replay")
		}
		return v
	}
	a := create(f.productID, false, "=渠道 A")
	if page := admin("GET", fmt.Sprintf("/admin/orders?product_id=%d&sales_source=%d", f.productID, a.ID), "", ""); page.Code != http.StatusOK {
		t.Fatalf("sales source order page: %d", page.Code)
	}
	b := create(f.productID, false, "渠道 B")
	pa := create(f.serviceProductID, true, "周期 A")
	pb := create(f.serviceProductID, true, "周期 B")
	for _, path := range []string{
		fmt.Sprintf("/api/admin/wechat-pay/products/%d/sales-sources", f.serviceProductID),
		fmt.Sprintf("/api/admin/service-period-products/%d/sales-sources", f.productID),
	} {
		if wrongKind := admin("GET", path, "", ""); wrongKind.Code != http.StatusNotFound {
			t.Fatalf("source path product kind mismatch: %d", wrongKind.Code)
		}
	}
	type fact struct {
		OrderID  int64  `json:"order_id"`
		Merchant string `json:"merchant_order_no"`
	}
	next := func(key string) publicCommerceTrustedH5Session {
		return issuePublicCommerceTrustedH5SessionWithKey(t, f, "source-session-"+key)
	}
	source := func(id int64) any { return map[string]any{"kind": "channel", "id": id} }
	post := func(id int64, kind string, sel any, session publicCommerceTrustedH5Session, key string) *httptest.ResponseRecorder {
		body := map[string]any{"product_id": id, "product_kind": kind, "beneficiary_selection": "payer_self", "coupon_claim_id": 0, "checkout_session_binding": paymentport.CheckoutSessionBinding(session.token)}
		if sel != nil {
			body["sales_source"] = sel
		}
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/v1/wechat-pay/checkouts", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		r.AddCookie(&http.Cookie{Name: paymentport.TrustedSessionCookieName, Value: session.token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) fact {
		var v fact
		if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.OrderID < 1 {
			t.Fatalf("checkout %d %s", w.Code, w.Body)
		}
		return v
	}
	sa := next("a")
	first := decode(post(f.productID, "standard", source(a.ID), sa, "source-checkout-a-0001"))
	path := fmt.Sprintf("/api/admin/wechat-pay/products/%d/sales-sources/%d", f.productID, a.ID)
	if w := admin("PUT", path, `{"name":"新渠道名","expected_version":1}`, "source-rename-0001"); w.Code != 200 {
		t.Fatal("rename", w.Code, w.Body)
	}
	if w := admin("PUT", path, `{"name":"旧版本","expected_version":1}`, "source-stale-0001"); w.Code != 409 {
		t.Fatal("stale rename", w.Code, w.Body)
	}
	var page productport.SalesSourcePage
	w := admin("GET", fmt.Sprintf("/api/admin/wechat-pay/products/%d/sales-sources", f.productID), "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 2 || page.Items[0].Name != "新渠道名" || page.Items[0].PublicPath != a.PublicPath {
		t.Fatal("fresh source read", w.Code, w.Body)
	}
	if replay := decode(post(f.productID, "standard", source(a.ID), sa, "source-checkout-a-0001")); replay.OrderID != first.OrderID {
		t.Fatal("rename changed replay")
	}
	if w := post(f.productID, "standard", source(b.ID), sa, "source-checkout-a-0001"); w.Code != 409 {
		t.Fatal("same key changed source", w.Code)
	}
	second := decode(post(f.productID, "standard", source(b.ID), next("b"), "source-checkout-b-0001"))
	for _, tc := range []struct {
		key       string
		selection any
		kind      string
	}{{"cross", source(pa.ID), "unrecorded"}, {"invalid", source(999999999), "unrecorded"}, {"standard", map[string]any{"kind": "standard"}, "standard"}, {"old", nil, ""}} {
		v := decode(post(f.productID, "standard", tc.selection, next(tc.key), "source-checkout-"+tc.key+"-0001"))
		var kind string
		if e := pool.QueryRow(f.ctx, `SELECT sales_source_kind FROM order_checkout_snapshots WHERE order_id=$1`, v.OrderID).Scan(&kind); e != nil || kind != tc.kind {
			t.Fatal("source classification", tc.key, kind, e)
		}
	}
	var before, after int
	if e := pool.QueryRow(f.ctx, `SELECT count(*) FROM orders`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(f.ctx, `ALTER TABLE product_sales_sources RENAME TO product_sales_sources_fault`); e != nil {
		t.Fatal(e)
	}
	failed := post(f.productID, "standard", source(b.ID), next("fault"), "source-fault-0001")
	if _, e := pool.Exec(f.ctx, `ALTER TABLE product_sales_sources_fault RENAME TO product_sales_sources`); e != nil {
		t.Fatal(e)
	}
	if e := pool.QueryRow(f.ctx, `SELECT count(*) FROM orders`).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if failed.Code < 500 || before != after {
		t.Fatal("lookup fault leaked order", failed.Code, before, after)
	}
	if _, e := pool.Exec(f.ctx, `CREATE FUNCTION source_snapshot_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'source fixture rollback'; END $$; CREATE TRIGGER source_snapshot_fail BEFORE INSERT ON order_checkout_snapshots FOR EACH ROW EXECUTE FUNCTION source_snapshot_fail()`); e != nil {
		t.Fatal(e)
	}
	rollback := next("rollback")
	failed = post(f.productID, "standard", source(b.ID), rollback, "source-rollback-0001")
	if _, e := pool.Exec(f.ctx, `DROP TRIGGER source_snapshot_fail ON order_checkout_snapshots;DROP FUNCTION source_snapshot_fail()`); e != nil {
		t.Fatal(e)
	}
	if e := pool.QueryRow(f.ctx, `SELECT count(*) FROM orders`).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if failed.Code < 500 || before != after {
		t.Fatal("snapshot fault leaked order", failed.Code, before, after)
	}
	decode(post(f.productID, "standard", source(b.ID), rollback, "source-rollback-0001"))
	pay := func(v fact, amount int64) {
		svc := f.application.paymentDistribution.(*paymentapp.Service)
		tx := fmt.Sprintf("source-trade-%d", v.OrderID)
		cb := paymentprovider.CallbackResult{Kind: "payment", MerchantOrderNo: v.Merchant, ProviderTransactionReference: tx, AmountMinor: amount, Currency: "CNY", AppID: "wx-public-commerce-h5", OccurredAt: time.Now().UTC(), ProviderTransactionDigest: string(effectport.Hash("wechatpay.transaction", tx)), EventDigest: sha256.Sum256([]byte(tx)), BodyDigest: sha256.Sum256([]byte("body-" + tx))}
		for i := 0; i < 2; i++ {
			if e := svc.ApplyVerifiedCallback(f.ctx, cb); e != nil {
				t.Fatal("callback replay", i, e)
			}
		}
	}
	pay(first, 9900)
	if w := post(f.productID, "standard", source(b.ID), next("owned"), "source-owned-0001"); w.Code != 409 || !strings.Contains(w.Body.String(), "already_purchased") {
		t.Fatal("cross-source owned", w.Code, w.Body)
	}
	p1 := decode(post(f.serviceProductID, "service_period", source(pa.ID), next("period-a"), "source-period-a-0001"))
	pay(p1, 12800)
	var end1, end2 time.Time
	var count int
	if e := pool.QueryRow(f.ctx, `SELECT end_at FROM order_service_entitlements WHERE customer_id=$1 AND service_product_id=$2`, sa.payerCustomerID, f.serviceProductID).Scan(&end1); e != nil {
		t.Fatal(e)
	}
	p2 := decode(post(f.serviceProductID, "service_period", source(pb.ID), next("period-b"), "source-period-b-0001"))
	pay(p2, 12800)
	if e := pool.QueryRow(f.ctx, `SELECT count(*),max(end_at) FROM order_service_entitlements WHERE customer_id=$1 AND service_product_id=$2`, sa.payerCustomerID, f.serviceProductID).Scan(&count, &end2); e != nil || count != 1 || !end2.Equal(end1.AddDate(0, 0, 30)) {
		t.Fatal("one shared entitlement", count, end1, end2, e)
	}
	for _, v := range []struct {
		v    fact
		name string
	}{{first, "=渠道 A"}, {second, "渠道 B"}, {p1, "周期 A"}, {p2, "周期 B"}} {
		w := admin("GET", "/api/admin/orders/"+v.v.Merchant, "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), v.name) {
			t.Fatal("order immutable source", w.Code, w.Body)
		}
	}
	w = admin("GET", fmt.Sprintf("/api/admin/orders?product_id=%d&sales_source=%d", f.productID, a.ID), "", "")
	var orders struct {
		Total int               `json:"total"`
		Items []json.RawMessage `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &orders) != nil || orders.Total != 1 || len(orders.Items) != 1 || !bytes.Contains(orders.Items[0], []byte("=渠道 A")) {
		t.Fatal("source filter", w.Code, w.Body)
	}
	for _, format := range []string{"csv", "xlsx"} {
		body := fmt.Sprintf(`{"resource":"orders","format":%q,"filter":{"product_id":%d,"sales_source":%q}}`, format, f.productID, fmt.Sprint(a.ID))
		w := admin("POST", "/api/admin/exports", body, "source-export-"+format+"-0001")
		if w.Code != 200 {
			t.Fatal("export", format, w.Code, w.Body)
		}
		var rows [][]string
		if format == "csv" {
			rows, _ = csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
		} else {
			book, e := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
			if e != nil {
				t.Fatal(e)
			}
			rows, e = book.GetRows("订单信息")
			book.Close()
			if e != nil {
				t.Fatal(e)
			}
		}
		if len(rows) != 2 || len(rows[0]) != 37 || rows[0][36] != "售卖渠道" || !strings.HasSuffix(rows[1][36], "=渠道 A") {
			t.Fatal("export source last column", format, rows)
		}
	}
	var events int
	if e := pool.QueryRow(f.ctx, `SELECT count(*) FROM order_paid_events WHERE order_id=$1`, first.OrderID).Scan(&events); e != nil || events != 1 {
		t.Fatal("paid event replay", events, e)
	}
	if installed {
		config, _ := json.Marshal(map[string]any{"mode": "sales", "base": f.server.URL, "session": owner, "csrf": csrf, "screenshots": newCapabilitiesScreenshotDirectory(t, "sales")})
		runNewCapabilitiesChromium(t, f, string(config))
	}
}

func salesSourceFixtureHTTP(t *testing.T, f *productExternalPushChromiumFixture) http.Handler {
	t.Helper()
	target, err := url.Parse(f.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := f.server.Client()
	client.Timeout = 15 * time.Second
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := r.Clone(r.Context())
		request.URL.Scheme, request.URL.Host = target.Scheme, target.Host
		request.Host, request.RequestURI = target.Host, ""
		request.Header.Set("Origin", f.server.URL)
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("synthetic business HTTP transport: %v", err)
		}
		defer response.Body.Close()
		for name, values := range response.Header {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.WriteHeader(response.StatusCode)
		if _, err = io.Copy(w, response.Body); err != nil {
			t.Fatal("read synthetic business HTTP response")
		}
	})
}

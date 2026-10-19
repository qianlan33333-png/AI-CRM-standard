package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	accesshttp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/http"
	paymentport "github.com/qianlan33333-png/AI-CRM-standard/internal/payment/port"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
)

func TestPostgreSQLSalesSourceConcurrentCreateRenameAndOutboxRollback(t *testing.T) {
	f := newProductExternalPushChromiumFixtureWithOptions(t, 150*time.Second, productExternalPushChromiumFixtureOptions{deferEffectsWorker: true})
	pool := f.application.pool.Native()
	owner, csrf := adminAccessLogin(t, f.application.handler, "product-browser-owner", "product-browser-owner-password")
	handler := salesSourceFixtureHTTP(t, f)
	endpoint := fmt.Sprintf("/api/admin/wechat-pay/products/%d/sales-sources", f.productID)
	request := func(method, path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: accesshttp.SessionCookieName, Value: owner})
		r.AddCookie(&http.Cookie{Name: accesshttp.CSRFCookieName, Value: csrf})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	var wait sync.WaitGroup
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- request("POST", endpoint, `{"name":"同一来源"}`, "source-concurrent-create")
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var first productport.SalesSource
	for response := range results {
		var source productport.SalesSource
		if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &source) != nil || source.ID < 1 {
			t.Fatalf("concurrent source creation status=%d", response.Code)
		}
		if first.ID == 0 {
			first = source
		} else if source != first {
			t.Fatal("same create key generated distinct source facts")
		}
	}
	countFacts := func() string {
		t.Helper()
		var counts string
		if err := pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM product_sales_sources)::text||':'||(SELECT count(*) FROM product_operation_receipts WHERE operation='sales_source.save')::text||':'||(SELECT count(*) FROM audit_events WHERE action='product.sales_source.saved')::text||':'||(SELECT count(*) FROM outbox_events WHERE event_type='product.sales_source.saved')::text`).Scan(&counts); err != nil {
			t.Fatal(err)
		}
		return counts
	}
	if countFacts() != "1:1:1:1" {
		t.Fatal("concurrent create duplicated transactional facts")
	}
	renamePath := endpoint + fmt.Sprintf("/%d", first.ID)
	start = make(chan struct{})
	results = make(chan *httptest.ResponseRecorder, 2)
	for _, name := range []string{"并发A", "并发B"} {
		wait.Add(1)
		go func(name string) {
			defer wait.Done()
			<-start
			results <- request("PUT", renamePath, fmt.Sprintf(`{"name":%q,"expected_version":1}`, name), "source-concurrent-rename-"+name)
		}(name)
	}
	close(start)
	wait.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for response := range results {
		switch response.Code {
		case 200:
			succeeded++
		case 409:
			conflicted++
		default:
			t.Fatalf("concurrent rename status=%d", response.Code)
		}
	}
	if succeeded != 1 || conflicted != 1 || countFacts() != "1:2:2:2" {
		t.Fatal("rename CAS did not preserve exactly one receipt/audit/outbox commit")
	}
	if _, err := pool.Exec(f.ctx, `CREATE FUNCTION qa_source_outbox_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='product.sales_source.saved' THEN RAISE EXCEPTION 'synthetic outbox rollback'; END IF;RETURN NEW;END $$;CREATE TRIGGER qa_source_outbox_fail BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION qa_source_outbox_fail()`); err != nil {
		t.Fatal(err)
	}
	if response := request("POST", endpoint, `{"name":"回滚来源"}`, "source-outbox-rollback"); response.Code != 503 {
		t.Fatalf("source outbox fault status=%d", response.Code)
	}
	if countFacts() != "1:2:2:2" {
		t.Fatal("outbox failure leaked source/receipt/audit")
	}
	if _, err := pool.Exec(f.ctx, `DROP TRIGGER qa_source_outbox_fail ON outbox_events;DROP FUNCTION qa_source_outbox_fail()`); err != nil {
		t.Fatal(err)
	}
	if response := request("POST", endpoint, `{"name":"回滚来源"}`, "source-outbox-rollback"); response.Code != 201 || countFacts() != "2:3:3:3" {
		t.Fatal("original source command could not recover after transactional failure")
	}
}

func TestPostgreSQLSalesSourceForwardMigrationsPreserveHistory(t *testing.T) {
	f := newProductExternalPushChromiumFixtureWithOptions(t, 150*time.Second, productExternalPushChromiumFixtureOptions{enablePublicH5: true, deferEffectsWorker: true})
	pool := f.application.pool.Native()
	session := issuePublicCommerceTrustedH5SessionWithKey(t, f, "migration-source-history")
	body := fmt.Sprintf(`{"product_id":%d,"product_kind":"standard","beneficiary_selection":"payer_self","coupon_claim_id":0,"checkout_session_binding":%q}`, f.productID, paymentport.CheckoutSessionBinding(session.token))
	r := httptest.NewRequest("POST", "/api/v1/wechat-pay/checkouts", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "migration-source-history")
	r.AddCookie(&http.Cookie{Name: paymentport.TrustedSessionCookieName, Value: session.token})
	w := httptest.NewRecorder()
	f.application.handler.ServeHTTP(w, r)
	var created struct {
		OrderID int64 `json:"order_id"`
	}
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &created) != nil || created.OrderID < 1 {
		t.Fatalf("prepare legacy checkout status=%d", w.Code)
	}
	// Reconstruct the exact pre-0248/0249 shape only in this disposable clone.
	if _, err := pool.Exec(f.ctx, `ALTER TABLE order_checkout_snapshots DROP CONSTRAINT order_checkout_sales_source_shape,DROP COLUMN sales_source_kind,DROP COLUMN sales_source_id,DROP COLUMN sales_source_name;DROP TABLE product_sales_sources`); err != nil {
		t.Fatal(err)
	}
	facts := func() string {
		t.Helper()
		var out string
		if err := pool.QueryRow(f.ctx, `SELECT jsonb_build_object('order',to_jsonb(o),'snapshot',to_jsonb(s)-'sales_source_kind'-'sales_source_id'-'sales_source_name')::text FROM orders o JOIN order_checkout_snapshots s ON s.order_id=o.id WHERE o.id=$1`, created.OrderID).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := facts()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate canonical migration source")
	}
	for _, name := range []string{"0248_product_sales_sources.sql", "0249_order_sales_source_snapshot.sql"} {
		sql, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(f.ctx, string(sql)); err != nil {
			_ = tx.Rollback(f.ctx)
			t.Fatalf("apply canonical migration %s: %v", name, err)
		}
		if err = tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if facts() != before {
		t.Fatal("forward attribution migration changed an existing order or snapshot")
	}
	var kind, name string
	var id *int64
	if err := pool.QueryRow(f.ctx, `SELECT sales_source_kind,sales_source_id,sales_source_name FROM order_checkout_snapshots WHERE order_id=$1`, created.OrderID).Scan(&kind, &id, &name); err != nil || kind != "" || id != nil || name != "" {
		t.Fatal("historical order was relabeled as standard/channel")
	}
	t.Log("canonical 0248/0249 forward SQL preserved all original order/snapshot fields; historical attribution remained unrecorded")
}

func TestPostgreSQLSalesSourceListMeasuredAtVolume(t *testing.T) {
	f := newProductExternalPushChromiumFixtureWithOptions(t, 150*time.Second, productExternalPushChromiumFixtureOptions{deferEffectsWorker: true})
	pool := f.application.pool.Native()
	var actor int64
	if err := pool.QueryRow(f.ctx, `SELECT id FROM admin_users WHERE username='product-browser-owner'`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id    int64
		count int
	}{{f.productID, 1500}, {f.serviceProductID, 1000}} {
		if _, err := pool.Exec(f.ctx, `INSERT INTO product_sales_sources(product_id,name,created_by,updated_by,created_at,updated_at) SELECT $1,'合成容量渠道-'||g,$2,$2,now(),now() FROM generate_series(1,$3) g`, row.id, actor, row.count); err != nil {
			t.Fatal(err)
		}
	}
	owner, _ := adminAccessLogin(t, f.application.handler, "product-browser-owner", "product-browser-owner-password")
	handler := salesSourceFixtureHTTP(t, f)
	measurements := []float64{}
	for range 5 {
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/admin/wechat-pay/products/%d/sales-sources", f.productID), nil)
		r.AddCookie(&http.Cookie{Name: accesshttp.SessionCookieName, Value: owner})
		w := httptest.NewRecorder()
		start := time.Now()
		handler.ServeHTTP(w, r)
		measurements = append(measurements, float64(time.Since(start).Microseconds())/1000)
		var page productport.SalesSourcePage
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1500 {
			t.Fatalf("volume source list status=%d", w.Code)
		}
		for _, source := range page.Items {
			if source.ProductID != productport.ID(f.productID) {
				t.Fatal("source list leaked another product")
			}
		}
	}
	sort.Float64s(measurements)
	t.Logf("sales_source_volume rows=1500 other_product_rows=1000 samples=5 p50_ms=%.3f p95_sample_ms=%.3f; measured only, no invented service SLA", measurements[2], measurements[4])
}

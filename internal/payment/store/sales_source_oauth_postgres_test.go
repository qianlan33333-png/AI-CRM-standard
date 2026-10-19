package store_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/payment/h5oauth"
)

func TestPostgreSQLSalesSourceOAuthMigrationPreservesStatesAndRedirectBoundary(t *testing.T) {
	pool, cleanup := paymentIntegrationPool(t)
	defer cleanup()
	ctx := context.Background()
	_, file, _, _ := runtime.Caller(0)
	apply := func(name string) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		// The production migrator also applies each file in one transaction.
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	apply("0228_payment_h5_original_checkout_return.sql")
	insert := func(path string, consumed bool) error {
		digest := sha256.Sum256([]byte(path))
		_, err := pool.Exec(ctx, `INSERT INTO payment_h5_oauth_states(state_digest,return_path,expires_at,created_at,consumed_at) VALUES($1,$2,CASE WHEN $3 THEN now()-interval '1 minute' ELSE now()+interval '10 minutes' END,now()-interval '10 minutes',CASE WHEN $3 THEN now()-interval '5 minutes' ELSE NULL END)`, digest[:], path, consumed)
		return err
	}
	assertRejected := func(path string, err error) {
		t.Helper()
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.ConstraintName != "payment_h5_oauth_states_return_path_check" {
			t.Fatalf("return CHECK did not reject %q: %v", path, err)
		}
	}
	regression := "/pay/31314141?sales_source=1"
	if !h5oauth.ValidReturnPath(regression) {
		t.Fatal("regression must already pass the application boundary")
	}
	assertRejected(regression, insert(regression, false))
	promotion := "dpc_" + strings.Repeat("A", 43)
	legacy := []string{
		"/p/31314141", "/pay/31314141", "/s/period", "/s/period/pay", "/c/coupon-2026", "/my/orders",
		"/p/course?promotion_context=" + promotion, "/pay/course?resume_order=7", "/s/period/pay?resume_order=8",
		"/distribution", "/distribution?product_id=7&product_type=standard_product", "/distribution?product_id=8&product_type=service_period",
		"/referral", "/referral?campaign=7", "/referral?campaign=7&invite=rfi_" + strings.Repeat("A", 43),
	}
	for i, path := range legacy {
		if err := insert(path, i%2 == 0); err != nil {
			t.Fatalf("seed historical state %q: %v", path, err)
		}
	}
	snapshot := func() string {
		t.Helper()
		var rows string
		if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s) ORDER BY return_path)::text FROM payment_h5_oauth_states s`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	before := snapshot()
	apply("0254_payment_h5_sales_source_return.sql")
	if snapshot() != before {
		t.Fatal("migration changed historical state, expiry or consumed facts")
	}
	for _, base := range []string{"/p/course", "/pay/course", "/s/period", "/s/period/pay"} {
		for _, source := range []string{"1", "9223372036854775807", "unrecorded"} {
			queries := []string{"sales_source=" + source, "sales_source=" + source + "&promotion_context=" + promotion, "promotion_context=" + promotion + "&sales_source=" + source}
			if base == "/pay/course" || base == "/s/period/pay" {
				queries = append(queries, "sales_source="+source+"&resume_order=9223372036854775807", "resume_order=7&sales_source="+source)
			}
			for _, query := range queries {
				path := base + "?" + query
				if !h5oauth.ValidReturnPath(path) {
					t.Fatalf("application rejected canonical source %q", path)
				}
				if err := insert(path, false); err != nil {
					t.Fatalf("database rejected canonical source %q: %v", path, err)
				}
			}
		}
	}
	for _, path := range []string{
		"https://evil.example/p/course?sales_source=1", "//evil.example/p/course?sales_source=1",
		"/my/orders?sales_source=1", "/distribution?sales_source=1", "/referral?sales_source=1", "/c/coupon-2026?sales_source=1",
		"/p/course?sales_source=", "/p/course?sales_source=0", "/p/course?sales_source=-1", "/p/course?sales_source=01",
		"/p/course?sales_source=9223372036854775808", "/p/course?sales_source=%31", "/p/course?sales_source=standard",
		"/p/course?sales_source=1&sales_source=2", "/p/course?sales_source=1&next=/admin", "/p/course?sales_source=1#fragment",
		"/p/course?sales_source=1&resume_order=7", "/s/period?resume_order=7&sales_source=1",
		"/pay/course?resume_order=9223372036854775808&sales_source=1", "/pay/course?resume_order=01&sales_source=1",
		"/pay/course?sales_source=1&resume_order=7&promotion_context=" + promotion,
		"/p/course?sales_source=1&promotion_context=dpc_short", "/p/course?promotion_context=" + promotion + "&sales_source=1&sales_source=2",
		"/p/course%2Fadmin?sales_source=1", "/p/course%5Cadmin?sales_source=1", "/p/course%3Fadmin?sales_source=1", "/p/course%23admin?sales_source=1",
		"/p/course\\admin?sales_source=1", "/p/course\tadmin?sales_source=1",
	} {
		if h5oauth.ValidReturnPath(path) {
			t.Fatalf("application accepted unsafe source %q", path)
		}
		assertRejected(path, insert(path, false))
	}
}

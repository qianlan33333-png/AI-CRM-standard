package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPostgreSQLCLISessionForwardMigrationPreservesBrowserExpiry(t *testing.T) {
	f := newProductExternalPushChromiumFixtureWithOptions(t, 150*time.Second, productExternalPushChromiumFixtureOptions{deferEffectsWorker: true})
	pool := f.application.pool.Native()
	adminAccessLogin(t, f.application.handler, "product-browser-owner", "product-browser-owner-password")
	// This disposable clone contains browser sessions only. Reconstruct the
	// pre-0250 shape before applying the exact forward migration bytes.
	var cli int
	if err := pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_sessions WHERE credential_kind<>'browser' OR expires_at IS NULL`).Scan(&cli); err != nil || cli != 0 {
		t.Fatal("migration fixture must contain only expiring browser sessions")
	}
	if _, err := pool.Exec(f.ctx, `DROP TABLE admin_cli_policies;ALTER TABLE admin_sessions DROP CONSTRAINT ck_admin_sessions_expiry,DROP COLUMN credential_kind,DROP COLUMN cli_policy_version;ALTER TABLE admin_sessions ALTER COLUMN expires_at SET NOT NULL;ALTER TABLE admin_sessions ADD CONSTRAINT ck_admin_sessions_expiry CHECK(expires_at>created_at)`); err != nil {
		t.Fatal(err)
	}
	facts := func() string {
		t.Helper()
		var out string
		if err := pool.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(s)-'credential_kind'-'cli_policy_version' ORDER BY s.id),'[]'::jsonb)::text FROM admin_sessions s`).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := facts()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate canonical CLI migration")
	}
	sql, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "migrations", "0250_access_cli_sessions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, string(sql)); err != nil {
		_ = tx.Rollback(f.ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if facts() != before {
		t.Fatal("0250 altered an original browser session or expiry")
	}
	if err = pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_sessions WHERE credential_kind<>'browser' OR cli_policy_version<>1 OR expires_at IS NULL`).Scan(&cli); err != nil || cli != 0 {
		t.Fatal("0250 reclassified or extended historical browser credentials")
	}
	t.Log("canonical 0250 retained every browser session field and absolute expiry; CLI policy defaults did not grant a long-lived credential")
}

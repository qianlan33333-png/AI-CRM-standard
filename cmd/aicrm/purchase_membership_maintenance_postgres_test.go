package main

import (
	"context"
	"encoding/base64"
	"net/url"
	"testing"

	platformconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
)

func TestPostgreSQLPurchaseMembershipMaintenanceCompositionDoesNotWrite(t *testing.T) {
	f := newPromotionCheckoutFixture(t)
	defer f.close()
	uri, err := url.Parse(f.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	q := uri.Query()
	q.Set("options", "-c default_transaction_read_only=on")
	uri.RawQuery = q.Encode()
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	ctx := context.WithValue(context.Background(), purchaseMembershipMaintenanceContextKey{}, true)
	app, err := compose(ctx, platformconfig.Runtime{Role: platformconfig.RoleAPI, DatabaseURL: uri.String(), PublicOrigin: "https://crm.example.test", ReleaseSHA: "shop-maintenance-readonly-test", WorkerOwner: "shop-maintenance-readonly-test", WorkerLimit: 1, Referral: platformconfig.Referral{TokenDataKey: key}, GroupOps: platformconfig.GroupOps{WebhookSecret: "synthetic-shop-maintenance"}, Survey: platformconfig.Survey{DataKey: key, IdentityPhoneDataKey: key}})
	if err != nil {
		t.Fatalf("maintenance composition attempted a write or failed initialization: %v", err)
	}
	defer app.Close()
	var readOnly bool
	if err = app.pool.Native().QueryRow(ctx, `SELECT current_setting('transaction_read_only')::boolean`).Scan(&readOnly); err != nil || !readOnly {
		t.Fatalf("test did not enforce read-only database: %v %v", readOnly, err)
	}
}

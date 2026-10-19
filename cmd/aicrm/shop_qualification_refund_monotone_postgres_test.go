package main

import (
	"context"
	"crypto/sha256"
	d "github.com/qianlan33333-png/AI-CRM-standard/internal/order/domain"
	o "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	s "github.com/qianlan33333-png/AI-CRM-standard/internal/order/store"
	"testing"
	"time"
)

func TestPostgreSQLShopQualificationConfirmedRefundSurvivesImportReplay(t *testing.T) {
	f := newPromotionCheckoutFixture(t)
	defer f.close()
	ctx := context.Background()
	store, err := s.NewPostgreSQL(f.pool, f.uow)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	source := o.ShopOrder{AppID: "shop-refund-test", ID: "monotone-refund-order", TransactionID: "monotone-refund-tx", PaidMinor: 680, CreatedAt: now.Add(-time.Hour), PaidAt: now.Add(-time.Minute), UpdatedAt: now, QualificationRefundState: "clear", Lines: []o.ShopOrderLine{{ProductID: "p", SKUID: "s", Name: "课", UnitMinor: 680, PaidMinor: 680, Quantity: 1}}}
	value := d.Snapshot{Provider: d.ProviderWeChatShop, SourceSystem: "wechat-shop:shop-refund-test", SourceKey: source.ID, MerchantOrderNo: source.ID, ProviderTransactionNo: source.TransactionID, PayerCustomerID: &f.buyer, BeneficiaryCustomerID: &f.buyer, Amount: d.Money{AmountMinor: 680, Currency: "CNY"}, Status: d.StatusPaid, RecordOrigin: d.RecordOriginExternal, Version: 2, CreatedAt: source.CreatedAt, UpdatedAt: now, Items: []d.ItemSnapshot{{LineNo: 1, ProductCode: "monotone-shop", ProductName: "课", UnitAmountMinor: 680, Quantity: 1, LineAmountMinor: 680}}}
	digest := sha256.Sum256([]byte("immutable-refund-test"))
	var saved o.ShopSaved
	importOrder := func() {
		t.Helper()
		if err = f.uow.Within(ctx, func(tx context.Context) error {
			var e error
			saved, e = store.UpsertShopOrderWithin(tx, source, value, digest, now, []byte("synthetic-encrypted"), 1)
			return e
		}); err != nil {
			t.Fatal(err)
		}
	}
	importOrder()
	target := o.ShopQualificationRefreshTarget{OrderID: saved.Event.OrderID, AppID: source.AppID, SourceOrderID: source.ID, TransactionID: source.TransactionID, PaidAt: source.PaidAt, PaidMinor: source.PaidMinor}
	refresh := func(state string, amount int64) {
		t.Helper()
		if err = f.pool.QueryRow(ctx, `SELECT qualification_checked_at FROM order_wechat_shop_facts WHERE order_id=$1`, target.OrderID).Scan(&target.CheckedAt); err != nil {
			t.Fatal(err)
		}
		latest := source
		latest.QualificationRefundState = state
		latest.RefundedMinor = amount
		if err = f.uow.Within(ctx, func(tx context.Context) error {
			return store.UpdateShopQualificationRefreshWithin(tx, target, latest, nil)
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertState := func(want string) {
		t.Helper()
		var state string
		var refunded int64
		var paidEvents int
		if err = f.pool.QueryRow(ctx, `SELECT f.qualification_refund_state,x.refunded_minor,(SELECT count(*) FROM order_paid_events WHERE order_id=x.id) FROM order_wechat_shop_facts f JOIN orders x ON x.id=f.order_id WHERE f.order_id=$1`, target.OrderID).Scan(&state, &refunded, &paidEvents); err != nil || state != want || refunded != 0 || paidEvents != 1 {
			t.Fatalf("qualification regressed or payment facts replayed: want=%s state=%s amount=%d paid_events=%d err=%v", want, state, refunded, paidEvents, err)
		}
	}
	for _, state := range []string{"pending", "unknown"} {
		refresh(state, 0)
		// A callback/sync read begun before the committed qualification refresh
		// must not clear its hold, even with an unchanged order update time.
		source.QualificationReadStartedAt = now.Add(-time.Second)
		importOrder()
		assertState(state)
		// A genuinely later successful official read can resolve a cancellation.
		source.QualificationReadStartedAt = time.Now().UTC()
		importOrder()
		assertState("clear")
	}
	refresh("refunded", 680)
	// Even a later clear snapshot cannot undo a completed monetary refund.
	source.QualificationReadStartedAt = time.Now().UTC()
	importOrder()
	assertState("refunded")
}

package app

import (
	"context"
	"testing"
	"time"

	customerdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	identitydomain "github.com/qianlan33333-png/AI-CRM-standard/internal/identity/domain"
	identityport "github.com/qianlan33333-png/AI-CRM-standard/internal/identity/port"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/order/domain"
	orderport "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
)

type shopRefundGuardStore struct {
	qualificationState string
	upserts            int
	lastSource         orderport.ShopOrder
	buyerUpserts       int
	dispatchMarks      int
	dispatched         bool
}

func (s *shopRefundGuardStore) UpsertShopOrderWithin(_ context.Context, source orderport.ShopOrder, snapshot domain.Snapshot, _ [32]byte, _ time.Time, ciphertext []byte, keyVersion int16) (orderport.ShopSaved, error) {
	s.upserts++
	s.lastSource = source
	orderSnapshot := snapshot
	orderSnapshot.ID = 101
	orderSnapshot.Version = 2
	return orderport.ShopSaved{
		QualificationRefundState: s.qualificationState,
		Event: orderport.PaidEvent{
			ID: 7, OrderID: 101, OrderVersion: 2, DomainEventOutboxID: 9,
			OccurredAt: source.PaidAt, SourceDigest: [32]byte{1}, Order: orderSnapshot,
		},
		Eligible: true, Ciphertext: append([]byte(nil), ciphertext...), KeyVersion: keyVersion,
		Lines: append([]orderport.ShopOrderLine(nil), source.Lines...),
	}, nil
}

func TestImportShopOrderStaleClearCannotDispatchPersistedRefundHold(t *testing.T) {
	for _, state := range []string{"pending", "unknown", "refunded"} {
		t.Run(state, func(t *testing.T) {
			service, store, membership, consumer, source := shopRefundGuardFixture(t, 0, "clear")
			store.qualificationState = state
			if err := service.ImportShopOrderWithin(context.Background(), source, source.PaidAt.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			if consumer.calls != 0 || store.dispatchMarks != 0 || membership.calls != 1 {
				t.Fatalf("stale clear dispatched persisted hold: consumer=%d marks=%d membership=%d", consumer.calls, store.dispatchMarks, membership.calls)
			}
		})
	}
}

func (s *shopRefundGuardStore) MarkShopDispatchWithin(_ context.Context, _ int64) error {
	s.dispatchMarks++
	s.dispatched = true
	return nil
}

func (s *shopRefundGuardStore) PendingShopOrdersWithin(_ context.Context, _, _, _ string) ([]string, error) {
	if s.dispatched {
		return nil, nil
	}
	return []string{"shop-order-1"}, nil
}

func (s *shopRefundGuardStore) UpsertShopBuyerWithin(_ context.Context, _ int64, _ orderport.ShopBuyerMaterial) error {
	s.buyerUpserts++
	return nil
}

func (*shopRefundGuardStore) ReadShopBuyerCiphertexts(context.Context, []int64) ([]orderport.ShopBuyerCiphertext, error) {
	return nil, nil
}

func (*shopRefundGuardStore) PendingShopBuyerCiphertextsWithin(context.Context, string, [32]byte) ([]orderport.ShopBuyerCiphertext, error) {
	return nil, nil
}

func (*shopRefundGuardStore) AssociateShopBuyerWithin(context.Context, int64, int64, string, []int64) error {
	return nil
}

type shopRefundGuardCatalog struct{}

func (shopRefundGuardCatalog) ImportShopSKUWithin(context.Context, productport.ShopSKU, string, time.Time) (productport.ID, bool, error) {
	return 80, true, nil
}

func (shopRefundGuardCatalog) ResolveShopSKUWithin(context.Context, string, string, string) (productport.ID, bool, error) {
	return 80, true, nil
}

func (shopRefundGuardCatalog) ShopSKUCoordinatesWithin(context.Context, productport.ID) (productport.ShopSKU, bool, error) {
	return productport.ShopSKU{}, false, nil
}

type shopRefundGuardCipher struct{}

func (shopRefundGuardCipher) EncryptCommercePayload(value, _ []byte) ([]byte, int16, error) {
	return append([]byte(nil), value...), 1, nil
}

func (shopRefundGuardCipher) DecryptCommercePayload(value []byte, _ int16, _ []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

type shopRefundGuardResolver struct{}

func (shopRefundGuardResolver) Resolve(context.Context, identitydomain.Reference) (identityport.ResolveResult, error) {
	return identityport.ResolveResult{Status: identityport.ResolveFound, CustomerID: customerdomain.CustomerID(73), IdentityID: 91}, nil
}

type shopRefundGuardSubjects struct{}

func (shopRefundGuardSubjects) WriteLockedVerifiedIdentitySubject(context.Context, int64) (identityport.VerifiedIdentitySubject, bool, error) {
	return identityport.VerifiedIdentitySubject{
		CustomerID: customerdomain.CustomerID(73),
		Lineage:    []customerdomain.CustomerID{customerdomain.CustomerID(73)},
	}, true, nil
}

type shopRefundGuardMembership struct{ calls int }

func (m *shopRefundGuardMembership) PurchaseQualificationChangedWithin(context.Context, int64, string) error {
	m.calls++
	return nil
}

type shopRefundGuardConsumer struct{ calls int }

func (c *shopRefundGuardConsumer) ConsumeShopPaidEventWithin(context.Context, orderport.PaidEvent) (bool, error) {
	c.calls++
	return true, nil
}

func shopRefundGuardFixture(t *testing.T, refundMinor int64, refundState string) (ShopOrders, *shopRefundGuardStore, *shopRefundGuardMembership, *shopRefundGuardConsumer, orderport.ShopOrder) {
	t.Helper()
	paidAt := time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)
	union, err := identitydomain.NewVerifiedFact(identitydomain.ProviderVerifiedIdentityInput{
		Kind: identitydomain.KindUnionID, Scope: "wechat-open-platform:primary", Value: "union-test", Source: "wechat-shop.order-api",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &shopRefundGuardStore{}
	membership := &shopRefundGuardMembership{}
	consumer := &shopRefundGuardConsumer{}
	service := ShopOrders{
		Store: store, Products: shopRefundGuardCatalog{}, Consumer: consumer,
		Cipher: shopRefundGuardCipher{}, Identity: shopRefundGuardResolver{}, Subjects: shopRefundGuardSubjects{},
		Membership: membership, UnionScope: "wechat-open-platform:primary",
	}
	source := orderport.ShopOrder{
		AppID: "shop-app", ID: "shop-order-1", TransactionID: "transaction-1",
		CreatedAt: paidAt.Add(-time.Minute), UpdatedAt: paidAt, PaidAt: paidAt,
		PaidMinor: 100, RefundedMinor: refundMinor, QualificationRefundState: refundState,
		BuyerUnion: union,
		Lines:      []orderport.ShopOrderLine{{ProductID: "product-1", SKUID: "sku-1", Name: "课件", UnitMinor: 100, PaidMinor: 100, Quantity: 1}},
	}
	return service, store, membership, consumer, source
}

func TestImportShopOrderRefundStateSkipsFulfillmentButKeepsPaidAndMembershipFacts(t *testing.T) {
	tests := []struct {
		name        string
		refunded    int64
		refundState string
	}{
		{name: "first observed fully refunded", refunded: 100, refundState: "refunded"},
		{name: "refund processing", refundState: "pending"},
		{name: "refund state unreadable", refundState: "unknown"},
		{name: "refund amount present despite clear state", refunded: 1, refundState: "clear"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, store, membership, consumer, source := shopRefundGuardFixture(t, tt.refunded, tt.refundState)
			if err := service.ImportShopOrderWithin(context.Background(), source, source.PaidAt.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			if consumer.calls != 0 || store.dispatchMarks != 0 {
				t.Fatalf("refund must not dispatch fulfillment: consumer=%d marks=%d", consumer.calls, store.dispatchMarks)
			}
			if store.upserts != 1 || store.buyerUpserts != 1 || store.lastSource.RefundedMinor != tt.refunded || store.lastSource.QualificationRefundState != tt.refundState {
				t.Fatalf("order/refund facts were not persisted before dispatch guard: upserts=%d buyerUpserts=%d saved=%+v", store.upserts, store.buyerUpserts, store.lastSource)
			}
			if membership.calls != 1 {
				t.Fatalf("purchase-membership refresh must still be enqueued for qualification to reject: calls=%d", membership.calls)
			}
		})
	}
}

func TestImportShopOrderPaidDispatchesOnceAndAcceptsLegacyEmptyRefundState(t *testing.T) {
	for _, state := range []string{"clear", ""} {
		t.Run("state="+state, func(t *testing.T) {
			service, store, membership, consumer, source := shopRefundGuardFixture(t, 0, state)
			cutoff := source.PaidAt.Add(-time.Hour)
			if err := service.ImportShopOrderWithin(context.Background(), source, cutoff); err != nil {
				t.Fatal(err)
			}
			if err := service.ImportShopOrderWithin(context.Background(), source, cutoff); err != nil {
				t.Fatal(err)
			}
			if consumer.calls != 1 || store.dispatchMarks != 1 {
				t.Fatalf("paid fulfillment should be accepted once: consumer=%d marks=%d", consumer.calls, store.dispatchMarks)
			}
			if store.upserts != 2 || membership.calls != 2 {
				t.Fatalf("replay must preserve idempotent fact/qualification paths: orderUpserts=%d membershipNotifications=%d", store.upserts, membership.calls)
			}
		})
	}
}

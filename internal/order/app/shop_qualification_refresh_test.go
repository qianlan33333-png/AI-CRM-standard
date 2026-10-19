package app

import (
	"context"
	"errors"
	"testing"

	c "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	o "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
)

type qualificationRefreshStore struct {
	updates   int
	sourceErr error
}

func (*qualificationRefreshStore) ListShopQualificationRefreshTargetsWithin(ctx context.Context, _, _ []int64) ([]o.ShopQualificationRefreshTarget, error) {
	if ctx.Value(orderLineageContextKey{}) != true {
		return nil, errors.New("read outside transaction")
	}
	return []o.ShopQualificationRefreshTarget{{OrderID: 1, SourceOrderID: "synthetic-order"}}, nil
}
func (*qualificationRefreshStore) ListShopQualificationRefreshCoordinatesWithin(context.Context, []int64, string, string, string) ([]o.ShopQualificationRefreshTarget, error) {
	return []o.ShopQualificationRefreshTarget{{OrderID: 1, SourceOrderID: "synthetic-order"}}, nil
}
func (s *qualificationRefreshStore) UpdateShopQualificationRefreshWithin(ctx context.Context, _ o.ShopQualificationRefreshTarget, _ o.ShopOrder, err error) error {
	if ctx.Value(orderLineageContextKey{}) != true {
		return errors.New("update outside transaction")
	}
	s.updates++
	s.sourceErr = err
	return nil
}

type qualificationRefreshProvider struct {
	calls int
	err   error
}

func (s *qualificationRefreshProvider) ReadShopOrder(ctx context.Context, _ string) (o.ShopOrder, error) {
	if ctx.Value(orderLineageContextKey{}) != nil {
		return o.ShopOrder{}, errors.New("provider call inside transaction")
	}
	s.calls++
	return o.ShopOrder{}, s.err
}

type qualificationRefreshCatalog struct{ shopRefundGuardCatalog }

func (qualificationRefreshCatalog) ShopSKUCoordinatesWithin(context.Context, p.ID) (p.ShopSKU, bool, error) {
	return p.ShopSKU{AppID: "test", ProductID: "p", SKUID: "s"}, true, nil
}
func TestShopQualificationRefreshDeduplicatesAndCallsProviderOutsideTransaction(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "read-failed"}[failed], func(t *testing.T) {
			store := &qualificationRefreshStore{}
			provider := &qualificationRefreshProvider{}
			if failed {
				provider.err = errors.New("provider unavailable")
			}
			s := ShopQualificationRefresh{UOW: orderLineageUOW{}, Store: store, Identity: orderLineageReaderStub{ids: []c.CustomerID{73}}, Provider: provider, Products: qualificationRefreshCatalog{}}
			err := s.RefreshShopQualification(context.Background(), 73, []int64{80, 81})
			if (err != nil) != failed || provider.calls != 1 || store.updates != 1 || (store.sourceErr != nil) != failed {
				t.Fatalf("refresh err=%v provider=%d updates=%d sourceErr=%v", err, provider.calls, store.updates, store.sourceErr)
			}
		})
	}
}

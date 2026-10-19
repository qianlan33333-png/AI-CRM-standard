package app

import (
	"context"
	c "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	d "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/domain"
	o "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	"testing"
	"time"
)

type scopeOrders struct {
	qualificationOrderStub
	rows []o.ShopQualificationEvidence
}

func (s scopeOrders) ReadShopQualificationEvidenceWithin(context.Context, []int64, int64) ([]o.ShopQualificationEvidence, error) {
	return s.rows, nil
}
func TestShopPurchaseScopeIsolationRefundsAndAlternative(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		name, state                          string
		wrongCustomer, wrongSKU, alternative bool
		want                                 d.QualificationState
	}{
		{name: "shop-only", state: "clear", want: d.QualificationEligible},
		{name: "refund-pending", state: "pending", want: d.QualificationSuspended},
		{name: "unknown", state: "unknown", want: d.QualificationUnavailable},
		{name: "refunded", state: "refunded", want: d.QualificationIneligible},
		{name: "wrong-owner", state: "clear", wrongCustomer: true, want: d.QualificationIneligible},
		{name: "wrong-sku", state: "clear", wrongSKU: true, want: d.QualificationIneligible},
		{name: "alternative", state: "unknown", alternative: true, want: d.QualificationEligible},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := o.ShopQualificationEvidence{OrderID: 8, ProductID: 80, PayerCustomerID: 11, BeneficiaryCustomerID: 11, ItemPaidMinor: 680, Line: 1, PaidAt: now, RefundState: test.state}
			if test.wrongCustomer {
				v.BeneficiaryCustomerID = 12
			}
			if test.wrongSKU {
				v.ProductID = 81
			}
			rows := []o.ShopQualificationEvidence{v}
			if test.alternative {
				v.OrderID = 9
				v.RefundState = "clear"
				rows = append(rows, v)
			}
			s, e := NewQualificationService(lineageStub{roots: []c.CustomerID{11}}, scopeOrders{rows: rows}, qualificationPaymentStub{})
			if e != nil {
				t.Fatal(e)
			}
			legacy, e := s.CheckWithin(qualificationContext(), 11, 79, d.ProductTypeStandard)
			if e != nil || legacy.AllowsPromotion() {
				t.Fatalf("global qualification widened: %+v %v", legacy, e)
			}
			got, e := s.CheckScopeWithin(qualificationContext(), 11, 79, d.ProductTypeStandard, d.PurchaseScope{CampaignID: 6, CampaignVersion: 1, CompatibleProductIDs: []int64{80}})
			if e != nil || got.State != test.want {
				t.Fatalf("got %+v %v want %s", got, e, test.want)
			}
		})
	}
}

package app

import (
	"context"
	customerDomain "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	d "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/domain"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/port"
	o "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	"time"
)

type AttributionPurchaseScopeReader interface {
	ReadAttributionPurchaseScopeWithin(context.Context, int64) (d.PurchaseScope, bool, error)
}

func (s *QualificationService) BindPurchaseScopes(r AttributionPurchaseScopeReader) error {
	if r == nil || s.scopes != nil {
		return p.ErrUnavailable
	}
	s.scopes = r
	return nil
}
func (s *QualificationService) CheckAttributionWithin(ctx context.Context, attributionID, customer, product int64, kind d.ProductType) (d.Qualification, error) {
	if s.scopes == nil {
		return s.CheckWithin(ctx, customer, product, kind)
	}
	scope, found, e := s.scopes.ReadAttributionPurchaseScopeWithin(ctx, attributionID)
	if e != nil {
		return unavailableQualification(s.now(), "qualification_scope_unavailable"), nil
	}
	if !found {
		return s.CheckWithin(ctx, customer, product, kind)
	}
	return s.CheckScopeWithin(ctx, customer, product, kind, scope)
}
func (s *QualificationService) CheckScopeWithin(ctx context.Context, customer, product int64, kind d.ProductType, scope d.PurchaseScope) (d.Qualification, error) {
	if !scope.Valid() {
		return d.Qualification{}, p.ErrQualification
	}
	base, e := s.CheckWithin(ctx, customer, product, kind)
	if e != nil || base.AllowsPromotion() {
		return base, e
	}
	reader, ok := s.orders.(o.ShopQualificationEvidenceReader)
	if !ok {
		if len(scope.CompatibleProductIDs) > 0 {
			return unavailableQualification(s.now(), "shop_evidence_unavailable"), nil
		}
		return base, nil
	}
	roots, err := s.lineage.LockedCanonicalLineage(ctx, customerDomain.CustomerID(customer))
	if err != nil || len(roots) == 0 {
		return unavailableQualification(s.now(), "identity_evidence_unavailable"), nil
	}
	ids := make([]int64, len(roots))
	for i, id := range roots {
		ids[i] = int64(id)
	}
	for _, sourceID := range scope.CompatibleProductIDs {
		evidence, err := reader.ReadShopQualificationEvidenceWithin(ctx, ids, sourceID)
		if err != nil {
			base = unavailableQualification(s.now(), "shop_evidence_unavailable")
			continue
		}
		for _, v := range evidence {
			if v.OrderID < 1 || v.ProductID != sourceID || v.ItemPaidMinor < 1 || v.PaidAt.IsZero() {
				continue
			}
			matched, err := s.MatchesTrustedCustomerWithin(ctx, customer, v.PayerCustomerID, v.BeneficiaryCustomerID)
			if err != nil || !matched {
				continue
			}
			switch v.RefundState {
			case "clear":
				return d.Qualification{State: d.QualificationEligible, Reason: "valid_shop_purchase", EvidenceRef: v.Reference(), CheckedAt: s.now().UTC()}, nil
			case "pending":
				if base.State != d.QualificationUnavailable {
					base = d.Qualification{State: d.QualificationSuspended, Reason: "shop_refund_pending", CheckedAt: s.now().UTC()}
				}
			case "refunded":
			default:
				base = unavailableQualification(s.now(), "shop_refund_unknown")
			}
		}
	}
	if base.CheckedAt.IsZero() {
		base.CheckedAt = time.Now().UTC()
	}
	return base, nil
}

func (s *QualificationService) BindShopQualificationRefresh(r o.ShopQualificationRefresher) error {
	if r == nil || s.shopRefresh != nil {
		return p.ErrUnavailable
	}
	s.shopRefresh = r
	return nil
}
func (s *QualificationService) RefreshAttributionScope(ctx context.Context, uow interface {
	Within(context.Context, func(context.Context) error) error
}, attribution, customer, product int64, kind d.ProductType) error {
	if s.scopes == nil {
		return nil
	}
	var scope d.PurchaseScope
	var found bool
	e := uow.Within(ctx, func(tx context.Context) error {
		var err error
		scope, found, err = s.scopes.ReadAttributionPurchaseScopeWithin(tx, attribution)
		return err
	})
	if e != nil {
		return e
	}
	if !found || len(scope.CompatibleProductIDs) == 0 {
		return nil
	}
	if s.shopRefresh == nil {
		return p.ErrUnavailable
	}
	var native bool
	if e = uow.Within(ctx, func(tx context.Context) error {
		q, err := s.CheckWithin(tx, customer, product, kind)
		native = err == nil && q.AllowsPromotion()
		return nil
	}); e != nil {
		return e
	}
	if native {
		return nil
	}
	return s.shopRefresh.RefreshShopQualification(ctx, customer, scope.CompatibleProductIDs)
}

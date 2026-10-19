package app

import (
	"context"
	d "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/domain"
	product "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	r "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/domain"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/port"
	"slices"
)

func (s *AdminService) BindCompatibleProducts(c product.ShopQualificationCatalog) error {
	if c == nil || s.compatibleProducts != nil {
		return p.ErrUnavailable
	}
	s.compatibleProducts = c
	return nil
}
func (s *AdminService) validateCompatibleProducts(ctx context.Context, c r.Campaign) error {
	if len(c.CompatibleProductIDs) == 0 {
		return nil
	}
	if c.QualificationMode != r.QualificationProductPurchase || s.compatibleProducts == nil {
		return p.ErrInvalidRequest
	}
	seen := map[int64]bool{}
	for _, id := range c.CompatibleProductIDs {
		if id < 1 || id == c.ProductID || seen[id] {
			return p.ErrInvalidRequest
		}
		seen[id] = true
	}
	if e := s.compatibleProducts.ValidateShopQualificationProductsWithin(ctx, c.CompatibleProductIDs); e != nil {
		return p.ErrInvalidRequest
	}
	return nil
}
func campaignPurchaseScope(c r.Campaign) d.PurchaseScope {
	return d.PurchaseScope{CampaignID: c.ID, CampaignVersion: c.Version, CompatibleProductIDs: slices.Clone(c.CompatibleProductIDs)}
}

type scopedPurchaseReader interface {
	CheckScopeWithin(context.Context, int64, int64, d.ProductType, d.PurchaseScope) (d.Qualification, error)
}

func (s *Service) campaignQualificationWithin(ctx context.Context, c r.Campaign, customer int64) (d.Qualification, error) {
	if s.qualification == nil {
		return d.Qualification{}, p.ErrUnavailable
	}
	if q, ok := s.qualification.(scopedPurchaseReader); ok {
		return q.CheckScopeWithin(ctx, customer, c.ProductID, d.ProductType(c.ProductType), campaignPurchaseScope(c))
	}
	if len(c.CompatibleProductIDs) > 0 {
		return d.Qualification{}, p.ErrUnavailable
	}
	return s.qualification.CheckWithin(ctx, customer, c.ProductID, d.ProductType(c.ProductType))
}

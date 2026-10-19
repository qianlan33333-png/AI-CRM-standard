package app

import (
	"context"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
)

func (s *Service) ValidateShopQualificationProductsWithin(ctx context.Context, ids []int64) error {
	r, ok := s.store.(p.ShopQualificationCatalog)
	if !ok {
		return p.ErrProductReadUnavailable
	}
	return r.ValidateShopQualificationProductsWithin(ctx, ids)
}
func (s *Service) ListShopQualificationProducts(ctx context.Context, q p.ProductOptionQuery) (p.ProductOptionPage, error) {
	q, e := normalizeProductOptionQuery(q)
	if e != nil {
		return p.ProductOptionPage{}, e
	}
	r, ok := s.store.(p.ShopQualificationCatalog)
	if !ok {
		return p.ProductOptionPage{}, p.ErrProductReadUnavailable
	}
	var page p.ProductOptionPage
	e = s.uow.Within(ctx, func(tx context.Context) error {
		var err error
		page, err = r.ListShopQualificationProducts(tx, q)
		return err
	})
	return page, e
}

func (s *Service) ReadShopQualificationProduct(ctx context.Context, id p.ID) (p.ProductOption, error) {
	r, ok := s.store.(p.ShopQualificationCatalog)
	if !ok {
		return p.ProductOption{}, p.ErrProductReadUnavailable
	}
	var v p.ProductOption
	e := s.uow.Within(ctx, func(tx context.Context) error { var e error; v, e = r.ReadShopQualificationProduct(tx, id); return e })
	return v, e
}

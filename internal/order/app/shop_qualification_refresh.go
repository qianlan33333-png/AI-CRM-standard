package app

import (
	"context"
	c "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	i "github.com/qianlan33333-png/AI-CRM-standard/internal/identity/port"
	o "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/port"
	product "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
)

type ShopQualificationRefresh struct {
	Products product.ShopCatalog
	UOW      p.UnitOfWork
	Store    o.ShopQualificationRefreshStore
	Identity i.LockedCanonicalLineageReader
	Provider o.ShopQualificationProviderReader
}

func (s *ShopQualificationRefresh) RefreshShopQualification(ctx context.Context, customer int64, products []int64) error {
	if len(products) == 0 {
		return nil
	}
	if s == nil || s.Provider == nil {
		return o.ErrUnavailable
	}
	var targets []o.ShopQualificationRefreshTarget
	e := s.UOW.Within(ctx, func(tx context.Context) error {
		roots, e := s.Identity.LockedCanonicalLineage(tx, c.CustomerID(customer))
		if e != nil {
			return e
		}
		ids := make([]int64, len(roots))
		for j, id := range roots {
			ids[j] = int64(id)
		}
		targets, e = s.Store.ListShopQualificationRefreshTargetsWithin(tx, ids, products)
		if e != nil {
			return e
		}
		if s.Products != nil {
			for _, id := range products {
				coords, found, e := s.Products.ShopSKUCoordinatesWithin(tx, product.ID(id))
				if e != nil {
					return e
				}
				if !found {
					continue
				}
				extra, e := s.Store.ListShopQualificationRefreshCoordinatesWithin(tx, ids, coords.AppID, coords.ProductID, coords.SKUID)
				if e != nil {
					return e
				}
				targets = append(targets, extra...)
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	seen := map[int64]bool{}
	for _, target := range targets {
		if seen[target.OrderID] {
			continue
		}
		seen[target.OrderID] = true
		source, readErr := s.Provider.ReadShopOrder(ctx, target.SourceOrderID)
		if e = s.UOW.Within(ctx, func(tx context.Context) error {
			return s.Store.UpdateShopQualificationRefreshWithin(tx, target, source, readErr)
		}); e != nil {
			return e
		}
		if readErr != nil {
			return o.ErrUnavailable
		}
	}
	return nil
}

package app

import (
	"context"
	o "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"slices"
)

func (s *Service) ReadShopQualificationEvidenceWithin(ctx context.Context, c []int64, p int64) ([]o.ShopQualificationEvidence, error) {
	r, ok := s.store.(o.ShopQualificationEvidenceReader)
	if !ok {
		return nil, o.ErrUnavailable
	}
	if s.shopQualificationProducts != nil {
		coordinates, found, e := s.shopQualificationProducts.ShopSKUCoordinatesWithin(ctx, pkgProductID(p))
		if e != nil {
			return nil, e
		}
		if found {
			if r, ok := s.store.(interface {
				ReadShopQualificationCoordinatesWithin(context.Context, []int64, int64, string, string, string) ([]o.ShopQualificationEvidence, error)
			}); ok {
				return r.ReadShopQualificationCoordinatesWithin(ctx, c, p, coordinates.AppID, coordinates.ProductID, coordinates.SKUID)
			}
		}
	}
	return r.ReadShopQualificationEvidenceWithin(ctx, c, p)
}
func (s *Service) ListPurchaseMemberCandidatesWithin(ctx context.Context, p []int64, c, a int64, l int32) ([]int64, error) {
	r, ok := s.store.(o.PurchaseMemberCandidates)
	if !ok {
		return nil, o.ErrUnavailable
	}
	ids, e := r.ListPurchaseMemberCandidatesWithin(ctx, p, c, a, l)
	if e != nil {
		return nil, e
	}
	if s.shopQualificationProducts != nil {
		if store, ok := s.store.(interface {
			ListShopMemberCoordinatesWithin(context.Context, string, string, string, int64, int64, int32) ([]int64, error)
		}); ok {
			for _, id := range p {
				coordinates, found, err := s.shopQualificationProducts.ShopSKUCoordinatesWithin(ctx, pkgProductID(id))
				if err != nil {
					return nil, err
				}
				if !found {
					continue
				}
				more, err := store.ListShopMemberCoordinatesWithin(ctx, coordinates.AppID, coordinates.ProductID, coordinates.SKUID, c, a, l)
				if err != nil {
					return nil, err
				}
				ids = append(ids, more...)
			}
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > int(l) {
		ids = ids[:l]
	}
	return ids, nil
}

func pkgProductID(id int64) p.ID { return p.ID(id) }
func (s *Service) BindShopQualificationCatalog(c p.ShopCatalog) error {
	if c == nil || s.shopQualificationProducts != nil {
		return o.ErrUnavailable
	}
	s.shopQualificationProducts = c
	return nil
}

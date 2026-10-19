package app

import (
	"context"
	"errors"
	orderport "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
)

func (s *Service) readOrderSalesSourcesWithin(ctx context.Context, ids []int64) (map[int64]*orderport.SalesSourceSnapshot, error) {
	if reader, ok := s.store.(orderport.SalesSourceReader); ok {
		return reader.ReadOrderSalesSources(ctx, ids)
	}
	out := map[int64]*orderport.SalesSourceSnapshot{}
	for _, id := range ids {
		v, e := s.store.ReadCheckoutSnapshot(ctx, id)
		if errors.Is(e, orderport.ErrNotFound) {
			continue
		}
		if e != nil {
			return nil, e
		}
		out[id] = v.SalesSource
	}
	return out, nil
}
func (s *Service) ReadOrderSalesSources(ctx context.Context, ids []int64) (out map[int64]*orderport.SalesSourceSnapshot, err error) {
	if !ready(s) {
		return nil, orderport.ErrUnavailable
	}
	err = s.uow.Within(ctx, func(tx context.Context) error { var e error; out, e = s.readOrderSalesSourcesWithin(tx, ids); return e })
	return out, classify(err)
}

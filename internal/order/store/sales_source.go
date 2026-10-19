package store

import (
	"context"
	orderport "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
)

func (r *Repository) ReadOrderSalesSources(ctx context.Context, ids []int64) (map[int64]*orderport.SalesSourceSnapshot, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return nil, e
	}
	out := map[int64]*orderport.SalesSourceSnapshot{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, e := tx.Query(ctx, `SELECT order_id,sales_source_kind,COALESCE(sales_source_id,0),sales_source_name FROM order_checkout_snapshots WHERE order_id=ANY($1::bigint[]) AND sales_source_kind<>''`, ids)
	if e != nil {
		return nil, mapError(e)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		v := new(orderport.SalesSourceSnapshot)
		if e = rows.Scan(&id, &v.Kind, &v.ID, &v.Name); e != nil {
			return nil, e
		}
		if !v.Valid() {
			return nil, orderport.ErrUnavailable
		}
		out[id] = v
	}
	return out, mapError(rows.Err())
}

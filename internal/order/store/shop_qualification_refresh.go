package store

import (
	"context"
	o "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
)

func (r *Repository) ListShopQualificationRefreshTargetsWithin(ctx context.Context, customers, products []int64) ([]o.ShopQualificationRefreshTarget, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT DISTINCT x.id,f.app_id,x.source_key,x.provider_transaction_no,f.paid_at,x.amount_minor,f.qualification_checked_at FROM orders x JOIN order_items i ON i.order_id=x.id JOIN order_wechat_shop_facts f ON f.order_id=x.id WHERE i.product_id=ANY($1::bigint[]) AND x.beneficiary_customer_id=ANY($2::bigint[]) AND f.buyer_association_state='linked'`, products, customers)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []o.ShopQualificationRefreshTarget{}
	for rows.Next() {
		var v o.ShopQualificationRefreshTarget
		if e = rows.Scan(&v.OrderID, &v.AppID, &v.SourceOrderID, &v.TransactionID, &v.PaidAt, &v.PaidMinor, &v.CheckedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) UpdateShopQualificationRefreshWithin(ctx context.Context, target o.ShopQualificationRefreshTarget, source o.ShopOrder, readErr error) error {
	tx, e := transaction(ctx)
	if e != nil {
		return e
	}
	state := "unknown"
	if readErr == nil && source.ID == target.SourceOrderID && source.AppID == target.AppID && source.TransactionID == target.TransactionID && source.PaidMinor == target.PaidMinor && source.PaidAt.Equal(target.PaidAt) {
		state = source.QualificationRefundState
		if source.RefundedMinor > 0 {
			state = "refunded"
		}
	}
	if state != "clear" && state != "pending" && state != "refunded" {
		state = "unknown"
	}
	// Never regress a locally confirmed refund; the source read happens before this lock.
	tag, e := tx.Exec(ctx, `UPDATE order_wechat_shop_facts SET qualification_refund_state=CASE WHEN qualification_refund_state='refunded' THEN 'refunded' ELSE $2 END,qualification_checked_at=clock_timestamp() WHERE order_id=$1 AND qualification_checked_at IS NOT DISTINCT FROM $3`, target.OrderID, state, target.CheckedAt)
	if e == nil && tag.RowsAffected() != 1 {
		return o.ErrConflict
	}
	return e
}

func (r *Repository) ListShopQualificationRefreshCoordinatesWithin(ctx context.Context, customers []int64, app, product, sku string) ([]o.ShopQualificationRefreshTarget, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT DISTINCT x.id,f.app_id,x.source_key,x.provider_transaction_no,f.paid_at,x.amount_minor,f.qualification_checked_at FROM orders x JOIN order_items i ON i.order_id=x.id JOIN order_wechat_shop_facts f ON f.order_id=x.id JOIN order_wechat_shop_lines l ON l.order_id=x.id AND l.line_no=i.line_no WHERE i.product_id IS NULL AND x.beneficiary_customer_id=ANY($1::bigint[]) AND f.app_id=$2 AND l.source_product_id=$3 AND l.source_sku_id=$4 AND f.buyer_association_state='linked'`, customers, app, product, sku)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []o.ShopQualificationRefreshTarget{}
	for rows.Next() {
		var v o.ShopQualificationRefreshTarget
		if e = rows.Scan(&v.OrderID, &v.AppID, &v.SourceOrderID, &v.TransactionID, &v.PaidAt, &v.PaidMinor, &v.CheckedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

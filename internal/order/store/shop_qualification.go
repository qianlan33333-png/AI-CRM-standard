package store

import (
	"context"
	o "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
)

func (r *Repository) ReadShopQualificationEvidenceWithin(ctx context.Context, customers []int64, product int64) ([]o.ShopQualificationEvidence, error) {
	return r.ReadShopQualificationCoordinatesWithin(ctx, customers, product, "", "", "")
}
func (r *Repository) ReadShopQualificationCoordinatesWithin(ctx context.Context, customers []int64, product int64, app, sourceProduct, sku string) ([]o.ShopQualificationEvidence, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return nil, e
	}
	if len(customers) == 0 || product < 1 {
		return nil, ErrInvalid
	}
	// Both identity handles must be trusted; the Distribution service validates current canonical lineage.
	rows, e := tx.Query(ctx, `SELECT x.id,$1::bigint,x.payer_customer_id,x.beneficiary_customer_id,i.line_no,i.line_amount_minor,f.paid_at,CASE WHEN x.refunded_minor>0 THEN 'refunded' ELSE f.qualification_refund_state END FROM orders x JOIN order_items i ON i.order_id=x.id JOIN order_wechat_shop_facts f ON f.order_id=x.id JOIN order_wechat_shop_lines l ON l.order_id=x.id AND l.line_no=i.line_no WHERE (x.payer_customer_id=ANY($2::bigint[]) OR x.beneficiary_customer_id=ANY($2::bigint[])) AND x.provider='wechat_shop' AND x.record_origin='external' AND f.buyer_association_state='linked' AND f.buyer_union_digest IS NOT NULL AND x.payer_customer_id IS NOT NULL AND x.beneficiary_customer_id IS NOT NULL AND x.status IN('paid','partially_refunded','refunded') AND f.paid_at IS NOT NULL AND (i.product_id=$1 OR (i.product_id IS NULL AND f.app_id=$3 AND l.source_product_id=$4 AND l.source_sku_id=$5)) FOR SHARE OF x,f`, product, customers, app, sourceProduct, sku)
	// Product coordinate resolution is supplied as immutable Order-owned accepted import mapping (i.product_id).
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []o.ShopQualificationEvidence{}
	for rows.Next() {
		var v o.ShopQualificationEvidence
		if e = rows.Scan(&v.OrderID, &v.ProductID, &v.PayerCustomerID, &v.BeneficiaryCustomerID, &v.Line, &v.ItemPaidMinor, &v.PaidAt, &v.RefundState); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListPurchaseMemberCandidatesWithin(ctx context.Context, products []int64, only, after int64, limit int32) ([]int64, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT DISTINCT x.beneficiary_customer_id FROM orders x JOIN order_items i ON i.order_id=x.id WHERE i.product_id=ANY($1::bigint[]) AND x.beneficiary_customer_id>$2 AND ($3::bigint=0 OR x.beneficiary_customer_id=$3) AND x.status IN('paid','partially_refunded','refunded') ORDER BY x.beneficiary_customer_id LIMIT $4`, products, after, only, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *Repository) ListShopMemberCoordinatesWithin(ctx context.Context, app, product, sku string, only, after int64, limit int32) ([]int64, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT DISTINCT o.beneficiary_customer_id FROM orders o JOIN order_wechat_shop_facts f ON f.order_id=o.id JOIN order_wechat_shop_lines l ON l.order_id=o.id WHERE f.app_id=$1 AND l.source_product_id=$2 AND l.source_sku_id=$3 AND o.beneficiary_customer_id>$4 AND ($5::bigint=0 OR o.beneficiary_customer_id=$5) ORDER BY o.beneficiary_customer_id LIMIT $6`, app, product, sku, after, only, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

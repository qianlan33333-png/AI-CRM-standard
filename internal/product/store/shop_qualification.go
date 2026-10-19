package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
)

func (r *Repository) ValidateShopQualificationProductsWithin(ctx context.Context, ids []int64) error {
	tx, e := transaction(ctx)
	if e != nil {
		return e
	}
	for _, id := range ids {
		var ok bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products p JOIN product_wechat_shop_skus s ON s.product_id=p.id WHERE p.id=$1 AND p.source_kind='wechat_shop')`, id).Scan(&ok)
		if e != nil {
			return e
		}
		if !ok {
			return p.ErrProductReadNotFound
		}
	}
	return nil
}
func (r *Repository) ListShopQualificationProducts(ctx context.Context, q p.ProductOptionQuery) (p.ProductOptionPage, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return p.ProductOptionPage{}, e
	}
	var total int64
	search := "%" + escapeProductOptionSearch(q.Q) + "%"
	e = tx.QueryRow(ctx, `SELECT count(*) FROM products p WHERE p.source_kind='wechat_shop' AND p.currency='CNY' AND EXISTS(SELECT 1 FROM product_wechat_shop_skus s WHERE s.product_id=p.id) AND (p.name ILIKE $1 OR p.product_code ILIKE $1)`, search).Scan(&total)
	if e != nil {
		return p.ProductOptionPage{}, e
	}
	rows, e := tx.Query(ctx, `SELECT p.id,p.product_code,p.name,p.price_minor,p.currency FROM products p WHERE p.source_kind='wechat_shop' AND p.currency='CNY' AND EXISTS(SELECT 1 FROM product_wechat_shop_skus s WHERE s.product_id=p.id) AND (p.name ILIKE $1 OR p.product_code ILIKE $1) ORDER BY p.id LIMIT $2 OFFSET $3`, search, q.Limit, q.Offset)
	if e != nil {
		return p.ProductOptionPage{}, e
	}
	defer rows.Close()
	items := []p.ProductOption{}
	for rows.Next() {
		var v p.ProductOption
		if e = rows.Scan(&v.ID, &v.Code, &v.Name, &v.PriceMinor, &v.Currency); e != nil {
			return p.ProductOptionPage{}, e
		}
		v.ProductType = p.ProductOptionStandard
		v.SourceKind = "wechat_shop"
		items = append(items, v)
	}
	return p.ProductOptionPage{Items: items, Total: total, Limit: q.Limit, Offset: q.Offset}, rows.Err()
}

func (r *Repository) ReadShopQualificationProduct(ctx context.Context, id p.ID) (p.ProductOption, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return p.ProductOption{}, e
	}
	var v p.ProductOption
	e = tx.QueryRow(ctx, `SELECT p.id,p.product_code,p.name,p.price_minor,p.currency FROM products p WHERE p.id=$1 AND p.source_kind='wechat_shop' AND EXISTS(SELECT 1 FROM product_wechat_shop_skus s WHERE s.product_id=p.id)`, id).Scan(&v.ID, &v.Code, &v.Name, &v.PriceMinor, &v.Currency)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, p.ErrProductReadNotFound
	}
	if e != nil {
		return v, e
	}
	v.SourceKind = "wechat_shop"
	v.ProductType = p.ProductOptionStandard
	return v, nil
}

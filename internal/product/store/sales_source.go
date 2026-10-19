package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"time"
)

func (r *Repository) ListSalesSources(ctx context.Context, id productport.ID) ([]productport.SalesSource, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT id,product_id,name,version FROM product_sales_sources WHERE product_id=$1 ORDER BY id`, id)
	if e != nil {
		return nil, mapDatabaseError(e)
	}
	defer rows.Close()
	out := []productport.SalesSource{}
	for rows.Next() {
		var v productport.SalesSource
		if e = rows.Scan(&v.ID, &v.ProductID, &v.Name, &v.Version); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, mapDatabaseError(rows.Err())
}
func (r *Repository) ReadSalesSource(ctx context.Context, productID productport.ID, id int64) (productport.SalesSource, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return productport.SalesSource{}, e
	}
	var v productport.SalesSource
	e = tx.QueryRow(ctx, `SELECT id,product_id,name,version FROM product_sales_sources WHERE product_id=$1 AND id=$2`, productID, id).Scan(&v.ID, &v.ProductID, &v.Name, &v.Version)
	return v, mapDatabaseError(e)
}
func (r *Repository) SaveSalesSource(ctx context.Context, c productport.SalesSourceCommand, at time.Time) (productport.SalesSource, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return productport.SalesSource{}, e
	}
	var v productport.SalesSource
	if c.SourceID == 0 {
		e = tx.QueryRow(ctx, `INSERT INTO product_sales_sources(product_id,name,created_by,updated_by,created_at,updated_at) VALUES($1,$2,$3,$3,$4,$4) RETURNING id,product_id,name,version`, c.ProductID, c.Name, c.Actor, at).Scan(&v.ID, &v.ProductID, &v.Name, &v.Version)
	} else {
		e = tx.QueryRow(ctx, `UPDATE product_sales_sources SET name=$3,version=version+1,updated_by=$4,updated_at=$5 WHERE product_id=$1 AND id=$2 AND version=$6 RETURNING id,product_id,name,version`, c.ProductID, c.SourceID, c.Name, c.Actor, at, c.ExpectedVersion).Scan(&v.ID, &v.ProductID, &v.Name, &v.Version)
	}
	if c.SourceID > 0 && errors.Is(e, pgx.ErrNoRows) {
		return v, productport.ErrProductConflict
	}
	return v, mapDatabaseError(e)
}

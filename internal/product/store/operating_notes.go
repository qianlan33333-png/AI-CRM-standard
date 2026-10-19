package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"time"
)

func (r *Repository) ReadOperatingNotesWithin(ctx context.Context, id productport.ID) (out productport.OperatingNotes, err error) {
	tx, err := transaction(ctx)
	if err != nil {
		return out, err
	}
	out.ProductID = id
	err = tx.QueryRow(ctx, `SELECT notes,version,updated_at FROM product_operating_notes WHERE product_id=$1`, id).Scan(&out.Notes, &out.Version, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}
func (r *Repository) SaveOperatingNotesWithin(ctx context.Context, c productport.SaveOperatingNotes, at time.Time) (out productport.OperatingNotes, err error) {
	tx, err := transaction(ctx)
	if err != nil {
		return out, err
	}
	old, err := r.ReadOperatingNotesWithin(ctx, c.ProductID)
	if err != nil {
		return out, err
	}
	if old.Version != c.ExpectedVersion {
		return out, productport.ErrProductConflict
	}
	err = tx.QueryRow(ctx, `INSERT INTO product_operating_notes(product_id,notes,version,updated_by,updated_at) VALUES($1,$2,1,$3,$4) ON CONFLICT(product_id) DO UPDATE SET notes=excluded.notes,version=product_operating_notes.version+1,updated_by=excluded.updated_by,updated_at=excluded.updated_at RETURNING product_id,notes,version,updated_at`, c.ProductID, c.Notes, c.Actor, at).Scan(&out.ProductID, &out.Notes, &out.Version, &out.UpdatedAt)
	return
}

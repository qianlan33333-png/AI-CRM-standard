package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	d "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/domain"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/port"
	"reflect"
	"time"
)

func (r *Repository) SaveAttributionPurchaseScopeWithin(ctx context.Context, id int64, s d.PurchaseScope, at time.Time) error {
	tx, e := transaction(ctx)
	if e != nil {
		return e
	}
	if id < 1 || !s.Valid() || at.IsZero() {
		return ErrInvalid
	}
	ids := s.CompatibleProductIDs
	if ids == nil {
		ids = []int64{}
	}
	_, e = tx.Exec(ctx, `INSERT INTO distribution_attribution_purchase_scopes(attribution_id,campaign_id,campaign_version,compatible_product_ids,created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(attribution_id) DO NOTHING`, id, s.CampaignID, s.CampaignVersion, ids, at)
	if e != nil {
		return e
	}
	old, found, e := r.ReadAttributionPurchaseScopeWithin(ctx, id)
	if e != nil {
		return e
	}
	if !found || old.CampaignID != s.CampaignID || old.CampaignVersion != s.CampaignVersion || !reflect.DeepEqual(old.CompatibleProductIDs, ids) {
		return p.ErrConflict
	}
	return nil
}
func (r *Repository) ReadAttributionPurchaseScopeWithin(ctx context.Context, id int64) (d.PurchaseScope, bool, error) {
	tx, e := transaction(ctx)
	if e != nil {
		return d.PurchaseScope{}, false, e
	}
	var s d.PurchaseScope
	e = tx.QueryRow(ctx, `SELECT campaign_id,campaign_version,compatible_product_ids FROM distribution_attribution_purchase_scopes WHERE attribution_id=$1`, id).Scan(&s.CampaignID, &s.CampaignVersion, &s.CompatibleProductIDs)
	if errors.Is(e, pgx.ErrNoRows) {
		return s, false, nil
	}
	if e != nil {
		return s, false, e
	}
	if !s.Valid() {
		return s, false, p.ErrUnavailable
	}
	return s, true, nil
}

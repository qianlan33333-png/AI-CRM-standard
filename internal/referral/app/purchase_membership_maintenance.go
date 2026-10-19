package app

import (
	"context"
	d "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/domain"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/port"
	"slices"
	"strconv"
	"time"
)

func (s *AdminService) ConfigurePurchaseCompatibilityForOperator(ctx context.Context, campaignID int64, ids []int64, reference string, apply bool) (d.Campaign, error) {
	var result d.Campaign
	if campaignID < 1 || reference == "" {
		return result, p.ErrInvalidRequest
	}
	err := s.uow.Within(ctx, func(tx context.Context) error {
		current, e := s.store.ReadCampaignWithin(tx, campaignID, true)
		if e != nil {
			return e
		}
		next := current
		if ids != nil {
			next.CompatibleProductIDs = slices.Clone(ids)
		}
		if e = s.validateCompatibleProducts(tx, next); e != nil {
			return e
		}
		result = next
		if !apply || slices.Equal(current.CompatibleProductIDs, next.CompatibleProductIDs) {
			return nil
		}
		if !current.AcceptingAt(s.now().UTC()) {
			return p.ErrCampaignUnavailable
		}
		next.Version++
		next.UpdatedAt = s.now().UTC()
		result, e = s.store.UpdateCampaignWithin(tx, next, current.Version)
		if e != nil {
			return e
		}
		return appendPlatformEvent(tx, s.audit, s.outbox, "referral.campaign.compatible_products_configured", "campaign", campaignID, "operator", 0, "purchase-compatibility:"+strconv.FormatInt(campaignID, 10)+":"+reference, map[string]any{"campaign_id": campaignID, "version": next.Version, "compatible_product_ids": next.CompatibleProductIDs, "authorization_reference": reference}, time.Now().UTC())
	})
	return result, err
}

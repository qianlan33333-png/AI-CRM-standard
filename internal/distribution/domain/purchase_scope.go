package domain

// PurchaseScope is server-generated activity configuration frozen at checkout.
// Attributions without a stored scope retain exact-product qualification.
type PurchaseScope struct {
	CampaignID           int64   `json:"campaign_id"`
	CampaignVersion      int64   `json:"campaign_version"`
	CompatibleProductIDs []int64 `json:"compatible_product_ids"`
}

func (s PurchaseScope) Valid() bool {
	if s.CampaignID < 1 || s.CampaignVersion < 1 {
		return false
	}
	seen := map[int64]bool{}
	for _, id := range s.CompatibleProductIDs {
		if id < 1 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

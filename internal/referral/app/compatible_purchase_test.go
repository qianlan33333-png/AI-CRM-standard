package app

import (
	"fmt"
	d "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/domain"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/port"
	"testing"
	"time"
)

func TestCampaignCompatibilityReceiptsPreserveLegacyAndDistinguishClear(t *testing.T) {
	start := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	create := p.CreateCampaignCommand{Name: "legacy", StartsAt: start, EndsAt: start.Add(24 * time.Hour), TeamMode: d.TeamModeIndividual, QualificationMode: d.QualificationProductPurchase, ProductID: 79, ProductType: "standard_product", LeaderboardMetric: d.LeaderboardInvites}
	if got := fmt.Sprintf("%x", campaignCreatePayload(create)); got != "6a443f358d467f9726864c6097271458cf2de76a62593127a565272583468dde" {
		t.Fatalf("legacy create receipt drift: %s", got)
	}
	create.CompatibleProductIDs = []int64{}
	if got := fmt.Sprintf("%x", campaignCreatePayload(create)); got != "6a443f358d467f9726864c6097271458cf2de76a62593127a565272583468dde" {
		t.Fatal("empty create changed default receipt")
	}
	update := p.UpdateCampaignCommand{CampaignID: 6, ExpectedVersion: 2, Name: create.Name, StartsAt: create.StartsAt, EndsAt: create.EndsAt, TeamMode: create.TeamMode, QualificationMode: create.QualificationMode, ProductID: create.ProductID, ProductType: create.ProductType, LeaderboardMetric: create.LeaderboardMetric}
	legacy := campaignUpdatePayload(update)
	if got := fmt.Sprintf("%x", legacy); got != "88b742b0249750c2b699af724b9af9bb709aa08f83ec646c8b01fa03487e2b58" {
		t.Fatalf("legacy update receipt drift: %s", got)
	}
	empty := []int64{}
	update.CompatibleProductIDs = &empty
	clear := campaignUpdatePayload(update)
	if clear == legacy {
		t.Fatal("explicit clear reused omitted-field receipt")
	}
	products := []int64{80}
	update.CompatibleProductIDs = &products
	if campaignUpdatePayload(update) == clear {
		t.Fatal("different scope reused receipt")
	}
}

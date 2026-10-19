package app

import (
	"context"
	"errors"
	distributiondomain "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/domain"
	referraldomain "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/domain"
	referralport "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/port"
	"testing"
	"time"
)

type purchasedEntryQualification struct {
	state distributiondomain.QualificationState
	err   error
	calls int
}

func (q *purchasedEntryQualification) CheckWithin(context.Context, int64, int64, distributiondomain.ProductType) (distributiondomain.Qualification, error) {
	q.calls++
	return distributiondomain.Qualification{State: q.state, Reason: "synthetic", EvidenceRef: "order:1:item:1", CheckedAt: time.Now()}, q.err
}
func TestPostgreSQLReferralPurchasedEntryReadOnlyAndFailClosed(t *testing.T) {
	h := newReferralPostgreSQLHarness(t)
	defer h.cleanup()
	ctx := context.Background()
	c, err := h.admin.CreateCampaign(ctx, referralport.CreateCampaignCommand{ActorAdminID: 9001, Name: "已购入口", StartsAt: h.clock.Add(-time.Hour), EndsAt: h.clock.Add(time.Hour), TeamMode: referraldomain.TeamModeIndividual, QualificationMode: referraldomain.QualificationProductPurchase, ProductID: 79, ProductType: "standard_product", IdempotencyKey: "purchased-entry-create"})
	if err != nil {
		t.Fatal(err)
	}
	c, err = h.admin.SetCampaignState(ctx, referralport.SetCampaignStateCommand{ActorAdminID: 9001, CampaignID: c.ID, ExpectedVersion: c.Version, Target: referraldomain.CampaignActive, IdempotencyKey: "purchased-entry-active"})
	if err != nil {
		t.Fatal(err)
	}
	q := &purchasedEntryQualification{}
	if err = h.service.SetPurchaseQualificationReader(q); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		state                 distributiondomain.QualificationState
		err                   error
		eligible, unavailable bool
	}{
		{state: distributiondomain.QualificationEligible, eligible: true},
		{state: distributiondomain.QualificationIneligible},
		{state: distributiondomain.QualificationSuspended},
		{state: distributiondomain.QualificationUnavailable, unavailable: true},
		{state: distributiondomain.QualificationConflict, unavailable: true},
		{unavailable: true},
		{state: distributiondomain.QualificationEligible, err: errors.New("read failed"), unavailable: true},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			q.state, q.err = tc.state, tc.err
			mine, e := h.service.MyCampaign(ctx, referralActor(12484, h.clock), c.ID)
			if tc.unavailable {
				if !errors.Is(e, referralport.ErrUnavailable) {
					t.Fatalf("error=%v", e)
				}
			} else if e != nil || mine.PurchaseEligible != tc.eligible || mine.Participation != nil || mine.InvitationAvailable {
				t.Fatalf("mine=%+v err=%v", mine, e)
			}
			assertCount(t, h.pool, `SELECT count(*) FROM referral_participations WHERE campaign_id=$1`, 0, c.ID)
			assertCount(t, h.pool, `SELECT count(*) FROM referral_invitations WHERE campaign_id=$1`, 0, c.ID)
		})
	}
	q.state, q.err = distributiondomain.QualificationEligible, nil
	joined, err := h.service.JoinCampaign(ctx, referralport.JoinCampaignCommand{Actor: referralActor(12484, h.clock), CampaignID: c.ID, IdempotencyKey: "purchased-entry-join"})
	if err != nil || joined.Participation == nil {
		t.Fatal(err)
	}
	if _, err = h.pool.Exec(ctx, `UPDATE referral_participations SET state='reversed' WHERE id=$1`, joined.Participation.ID); err != nil {
		t.Fatal(err)
	}
	calls := q.calls
	mine, err := h.service.MyCampaign(ctx, referralActor(12484, h.clock), c.ID)
	if err != nil || mine.PurchaseEligible || mine.InvitationAvailable || q.calls != calls {
		t.Fatalf("reversed participation resurrected: %+v %v", mine, err)
	}
	h.clock = c.EndsAt.Add(time.Second)
	if _, err = h.service.MyCampaign(ctx, referralActor(12485, h.clock), c.ID); err != nil || q.calls != calls {
		t.Fatalf("closed campaign read checked eligibility: %v", err)
	}
}

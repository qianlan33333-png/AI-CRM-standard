package app

import (
	"context"
	"errors"
	"fmt"
	customer "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	distributiondomain "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/domain"
	identity "github.com/qianlan33333-png/AI-CRM-standard/internal/identity/port"
	order "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	domain "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/domain"
	port "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/port"
	"slices"
	"strconv"
	"time"
)

type PurchaseMembershipEnqueuer interface {
	EnqueuePurchaseMembershipWithin(context.Context, int64, int64, string, time.Time) error
}
type MembershipResult struct {
	Examined   int `json:"examined"`
	Created    int `json:"created"`
	Existing   int `json:"existing"`
	Ineligible int `json:"ineligible"`
	Pending    int `json:"pending"`
}

func (s *Service) BindPurchaseMembership(j PurchaseMembershipEnqueuer, l identity.LockedCanonicalLineageReader, o order.PurchaseMemberCandidates) error {
	if j == nil || l == nil || o == nil {
		return port.ErrUnavailable
	}
	s.membershipJobs = j
	s.membershipLineage = l
	s.membershipOrders = o
	return nil
}
func (s *AdminService) BindPurchaseMembershipJobs(j PurchaseMembershipEnqueuer) error {
	if j == nil {
		return port.ErrUnavailable
	}
	s.membershipJobs = j
	return nil
}
func (s *AdminService) queuePurchaseMembersWithin(ctx context.Context, c domain.Campaign) error {
	if s.membershipJobs == nil || c.QualificationMode != domain.QualificationProductPurchase {
		return nil
	}
	if c.State != domain.CampaignActive && c.State != domain.CampaignScheduled {
		return nil
	}
	return s.membershipJobs.EnqueuePurchaseMembershipWithin(ctx, c.ID, 0, "campaign:"+strconv.FormatInt(c.Version, 10), c.StartsAt)
}
func (s *Service) PurchaseQualificationChangedWithin(ctx context.Context, customerID int64, cause string) error {
	if s.membershipJobs == nil || customerID < 1 {
		return nil
	}
	return s.membershipJobs.EnqueuePurchaseMembershipWithin(ctx, 0, customerID, cause, s.now().UTC())
}

// This service creates membership only. It never accepts an invitation, scores a sale,
// consumes a payment event, provisions a receiver or calls fulfilment.
func (s *Service) ensurePurchaseMember(ctx context.Context, campaignID, customerID int64, proposed []int64, apply bool) (string, error) {
	var result string
	err := s.uow.Within(ctx, func(tx context.Context) error {
		var err error
		result, err = s.ensurePurchaseMemberWithin(tx, campaignID, customerID, proposed, apply)
		return err
	})
	return result, err
}

// Joins the caller's UoW so HTTP recovery and background/historical recovery
// commit the same membership, receipt and audit without nested transactions.
func (s *Service) ensurePurchaseMemberWithin(tx context.Context, campaignID, customerID int64, proposed []int64, apply bool) (string, error) {
	result := "ineligible"
	err := func() error {
		roots, e := s.membershipLineage.LockedCanonicalLineage(tx, customer.CustomerID(customerID))
		if e != nil || len(roots) == 0 {
			return port.ErrUnavailable
		}
		ids := make([]int64, len(roots))
		for i, id := range roots {
			ids[i] = int64(id)
		}
		customerID = ids[0]
		campaign, e := s.store.ReadCampaignWithin(tx, campaignID, true)
		if proposed != nil {
			campaign.CompatibleProductIDs = proposed
		}
		if e != nil {
			return e
		}
		if campaign.QualificationMode != domain.QualificationProductPurchase || !campaign.AcceptingAt(s.now().UTC()) {
			return nil
		}
		existing, e := s.ownParticipationWithin(tx, campaignID, ids, true)
		if e == nil {
			_ = existing
			result = "existing"
			return nil
		}
		if !errors.Is(e, port.ErrNotFound) {
			return e
		}
		q, e := s.campaignQualificationWithin(tx, campaign, customerID)
		if e != nil || q.State == "unavailable" || q.State == "conflict" || q.State == "suspended" {
			result = "pending"
			return nil
		}
		if !q.AllowsPromotion() {
			return nil
		}
		result = "created"
		if !apply {
			return nil
		}
		locks := slices.Clone(ids)
		slices.Sort(locks)
		for _, id := range locks {
			if e = s.store.LockParticipationWithin(tx, campaignID, id); e != nil {
				return e
			}
		}
		if _, e = s.ownParticipationWithin(tx, campaignID, ids, true); e == nil {
			result = "existing"
			return nil
		} else if !errors.Is(e, port.ErrNotFound) {
			return e
		}
		now := s.now().UTC()
		value := domain.Participation{CampaignID: campaignID, CustomerID: customerID, State: domain.ParticipationActive, JoinedAt: now}
		// A frozen Referral checkout is the only historical inviter source. Current URL tokens are never used.
		if reader, ok := s.store.(interface {
			PurchaseMembershipInvitationWithin(context.Context, int64, []int64, string) (domain.Participation, error)
		}); ok {
			snapshot, err := reader.PurchaseMembershipInvitationWithin(tx, campaignID, ids, q.EvidenceRef)
			if err != nil {
				return err
			}
			value.InvitationID = snapshot.InvitationID
			value.InviterCustomerID = snapshot.InviterCustomerID
			value.InviterTeamID = snapshot.InviterTeamID
			value.TeamID = snapshot.TeamID
		}
		member, e := s.store.InsertParticipationWithin(tx, value)
		if e != nil {
			return e
		}
		key := fmt.Sprintf("purchase-member:%d:%d", campaignID, customerID)
		payload := digestStrings(key, q.EvidenceRef)
		if e = s.store.AppendOperationReceiptWithin(tx, "join", "customer:"+strconv.FormatInt(customerID, 10), key, payload, "participation", member.ID, now); e != nil {
			return e
		}
		return s.appendEvent(tx, "referral.participation.purchase_confirmed", "participation", member.ID, "customer", customerID, key, map[string]any{"campaign_id": campaignID, "participation_id": member.ID, "qualification_reference": q.EvidenceRef, "campaign_version": campaign.Version}, now)
	}()
	return result, err
}
func (s *Service) ReconcilePurchaseMembers(ctx context.Context, campaignID, onlyCustomer int64, apply bool) (MembershipResult, error) {
	return s.reconcilePurchaseMembers(ctx, campaignID, onlyCustomer, nil, apply)
}
func (s *Service) ReconcilePurchaseMembersWithProducts(ctx context.Context, campaign int64, products []int64, apply bool) (MembershipResult, error) {
	if apply {
		return s.ReconcilePurchaseMembers(ctx, campaign, 0, true)
	}
	return s.reconcilePurchaseMembers(ctx, campaign, 0, products, false)
}
func (s *Service) reconcilePurchaseMembers(ctx context.Context, campaignID, onlyCustomer int64, proposed []int64, apply bool) (MembershipResult, error) {
	var result MembershipResult
	if s.membershipOrders == nil || s.membershipLineage == nil {
		return result, port.ErrUnavailable
	}
	campaigns := []domain.Campaign{}
	e := s.uow.Within(ctx, func(tx context.Context) error {
		if campaignID > 0 {
			c, e := s.store.ReadCampaignWithin(tx, campaignID, false)
			if e == nil {
				campaigns = append(campaigns, c)
			}
			return e
		}
		for offset := int32(0); ; offset += 100 {
			page, e := s.store.ListCampaignsWithin(tx, offset, 100, false)
			if e != nil {
				return e
			}
			campaigns = append(campaigns, page...)
			if len(page) < 100 {
				return nil
			}
		}
	})
	if e != nil {
		return result, e
	}
	for _, c := range campaigns {
		if proposed != nil {
			c.CompatibleProductIDs = proposed
		}
		if c.QualificationMode != domain.QualificationProductPurchase || !c.AcceptingAt(s.now().UTC()) {
			continue
		}
		products := append([]int64{c.ProductID}, c.CompatibleProductIDs...)
		after := int64(0)
		for {
			var ids []int64
			e = s.uow.Within(ctx, func(tx context.Context) error {
				var err error
				ids, err = s.membershipOrders.ListPurchaseMemberCandidatesWithin(tx, products, onlyCustomer, after, 100)
				return err
			})
			if e != nil {
				return result, e
			}
			if len(ids) == 0 {
				break
			}
			for _, id := range ids {
				if apply && s.membershipRefresh != nil && len(c.CompatibleProductIDs) > 0 {
					if refreshErr := s.membershipRefresh.RefreshShopQualification(ctx, id, c.CompatibleProductIDs); refreshErr != nil {
						nativeOK := false
						if e = s.uow.Within(ctx, func(tx context.Context) error {
							q, err := s.qualification.CheckWithin(tx, id, c.ProductID, distributiondomain.ProductType(c.ProductType))
							nativeOK = err == nil && q.AllowsPromotion()
							return nil
						}); e != nil {
							return result, e
						}
						if !nativeOK {
							result.Examined++
							result.Pending++
							after = id
							continue
						}
					}
				}
				state, e := s.ensurePurchaseMember(ctx, c.ID, id, proposed, apply)
				if e != nil {
					return result, e
				}
				result.Examined++
				switch state {
				case "created":
					result.Created++
				case "existing":
					result.Existing++
				case "pending":
					result.Pending++
				default:
					result.Ineligible++
				}
				after = id
			}
			if len(ids) < 100 {
				break
			}
		}
	}
	return result, nil
}

func (s *Service) BindPurchaseMembershipRefresh(r order.ShopQualificationRefresher) {
	s.membershipRefresh = r
}

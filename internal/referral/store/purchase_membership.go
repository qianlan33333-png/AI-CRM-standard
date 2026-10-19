package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	d "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/domain"
	"strconv"
	"strings"
)

func (r *Repository) PurchaseMembershipInvitationWithin(ctx context.Context, campaign int64, customers []int64, ref string) (d.Participation, error) {
	var out d.Participation
	tx, e := transaction(ctx)
	if e != nil {
		return out, e
	}
	parts := strings.Split(ref, ":")
	if len(parts) != 4 || parts[0] != "order" {
		return out, nil
	}
	orderID, _ := strconv.ParseInt(parts[1], 10, 64)
	if orderID < 1 {
		return out, nil
	}
	var credential string
	var promoter int64
	e = tx.QueryRow(ctx, `SELECT promotion_credential_ref,promoter_customer_id FROM referral_product_sale_checkouts WHERE order_id=$1 AND campaign_id=$2 AND buyer_customer_id=ANY($3::bigint[]) AND beneficiary_customer_id=ANY($3::bigint[])`, orderID, campaign, customers).Scan(&credential, &promoter)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	if !strings.HasPrefix(credential, "referral.invitation:") || promoter < 1 {
		return out, nil
	}
	for _, id := range customers {
		if id == promoter {
			return out, nil
		}
	}
	invitationID, _ := strconv.ParseInt(strings.TrimPrefix(credential, "referral.invitation:"), 10, 64)
	if invitationID < 1 {
		return out, nil
	}
	var found bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM referral_invitations WHERE id=$1 AND campaign_id=$2 AND inviter_customer_id=$3)`, invitationID, campaign, promoter).Scan(&found)
	if e != nil || !found {
		return out, e
	}
	out.InvitationID = invitationID
	out.InviterCustomerID = promoter
	return out, nil
}

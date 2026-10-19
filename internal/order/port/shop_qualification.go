package port

import (
	"context"
	"strconv"
	"time"
)

type ShopQualificationEvidence struct {
	OrderID, ProductID, PayerCustomerID, BeneficiaryCustomerID, ItemPaidMinor int64
	Line                                                                      int32
	PaidAt                                                                    time.Time
	RefundState                                                               string
}

func (e ShopQualificationEvidence) Reference() string {
	return "order:" + strconv.FormatInt(e.OrderID, 10) + ":item:" + strconv.FormatInt(int64(e.Line), 10)
}

type ShopQualificationEvidenceReader interface {
	ReadShopQualificationEvidenceWithin(context.Context, []int64, int64) ([]ShopQualificationEvidence, error)
}
type PurchaseMemberCandidates interface {
	ListPurchaseMemberCandidatesWithin(context.Context, []int64, int64, int64, int32) ([]int64, error)
}
type PurchaseMembershipObserver interface {
	PurchaseQualificationChangedWithin(context.Context, int64, string) error
}

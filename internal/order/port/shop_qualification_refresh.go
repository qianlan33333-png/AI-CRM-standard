package port

import (
	"context"
	"time"
)

type ShopQualificationRefreshTarget struct {
	CheckedAt                           *time.Time
	OrderID                             int64
	AppID, SourceOrderID, TransactionID string
	PaidAt                              time.Time
	PaidMinor                           int64
}
type ShopQualificationRefreshStore interface {
	ListShopQualificationRefreshCoordinatesWithin(context.Context, []int64, string, string, string) ([]ShopQualificationRefreshTarget, error)
	ListShopQualificationRefreshTargetsWithin(context.Context, []int64, []int64) ([]ShopQualificationRefreshTarget, error)
	UpdateShopQualificationRefreshWithin(context.Context, ShopQualificationRefreshTarget, ShopOrder, error) error
}
type ShopQualificationProviderReader interface {
	ReadShopOrder(context.Context, string) (ShopOrder, error)
}
type ShopQualificationRefresher interface {
	RefreshShopQualification(context.Context, int64, []int64) error
}

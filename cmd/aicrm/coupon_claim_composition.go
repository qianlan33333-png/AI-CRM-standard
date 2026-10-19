package main

import (
	couponapp "github.com/qianlan33333-png/AI-CRM-standard/internal/coupon/app"
	couponport "github.com/qianlan33333-png/AI-CRM-standard/internal/coupon/port"
	platformport "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/port"
)

// HTTP requests have no repository transaction until the application opens it.
func composeCouponClaimAdmin(uow platformport.UnitOfWork, store couponapp.CouponClaimAdminStore) (couponport.CouponClaimAdminReader, error) {
	return couponapp.NewCouponClaimAdminApplication(uow, store)
}

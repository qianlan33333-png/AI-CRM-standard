package port

import (
	"context"
	"time"
)

// RPAPrincipal grants only the dedicated local executor routes. It is never a
// browser/admin/CLI session and cannot be adapted into one.
type RPAPrincipal struct {
	DeviceID     int64
	StaffID      int64
	SenderUserID string
	DisplayName  string
}
type RPAAuthenticator interface {
	AuthenticateRPA(context.Context, string) (RPAPrincipal, error)
}
type RPAAccountSession struct {
	StaffID      int64
	SenderUserID string
	IssuedAt     time.Time
}
type RPAAccountVerifier interface {
	VerifyRPAAccountSession(context.Context, string) (RPAAccountSession, error)
}
type RPADevice struct {
	ID             int64
	TokenDigest    [32]byte
	UserCodeDigest [32]byte
	Name           string
	StaffID        int64
	SenderUserID   string
	SessionVersion int64
	State          string
	PairExpiresAt  time.Time
	ExpiresAt      time.Time
	CreatedAt      time.Time
}
type RPADevices interface {
	CreateRPADevice(context.Context, RPADevice) error
	RPADeviceByToken(context.Context, [32]byte, bool) (RPADevice, error)
	RPADeviceByUserCode(context.Context, [32]byte, bool) (RPADevice, error)
	ApproveRPADevice(context.Context, RPADevice) error
	RevokeRPADevice(context.Context, int64, int64) error
}

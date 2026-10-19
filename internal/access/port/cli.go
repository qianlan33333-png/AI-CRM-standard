package port

import (
	"context"
	"net/netip"
)

// CLIRequest is installed only by the HTTP Bearer adapter, never from cookies.
type CLIRequest struct {
	Token  string
	Source netip.Addr
}
type cliRequestKey struct{}

func WithCLIRequest(ctx context.Context, request CLIRequest) context.Context {
	return context.WithValue(ctx, cliRequestKey{}, request)
}
func CLIRequestFrom(ctx context.Context) (CLIRequest, bool) {
	value, ok := ctx.Value(cliRequestKey{}).(CLIRequest)
	return value, ok
}

type CLIPolicy struct {
	AllowedCIDRs []string `json:"allowed_cidrs"`
	Version      int64    `json:"version"`
}
type CLIRepository interface {
	CLIPolicy(context.Context, int64) (CLIPolicy, error)
	ReplaceCLIPolicy(context.Context, int64, []string) (CLIPolicy, error)
}

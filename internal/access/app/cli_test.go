package app

import (
	"context"
	"errors"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	accessport "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	"net/netip"
	"testing"
	"time"
)

type cliMemory struct {
	*memoryRepository
	policy accessport.CLIPolicy
}

func (r *cliMemory) CLIPolicy(context.Context, int64) (accessport.CLIPolicy, error) {
	return r.policy, nil
}
func (r *cliMemory) ReplaceCLIPolicy(_ context.Context, _ int64, c []string) (accessport.CLIPolicy, error) {
	r.policy.Version++
	r.policy.AllowedCIDRs = c
	return r.policy, nil
}
func TestCLIAccountTokenLifetimeIPPurposeAndRevocation(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(map[bool]string{false: "24hours", true: "whitelisted"}[bound], func(t *testing.T) {
			_, base, clock := authenticationFixture(t, 5)
			repo := &cliMemory{base, accessport.CLIPolicy{AllowedCIDRs: []string{}, Version: 1}}
			if bound {
				repo.policy.AllowedCIDRs = []string{"203.0.113.0/24"}
			}
			service, e := NewAuthentication(repo, testUOW{}, testPasswords{}, AuthenticationConfig{Now: func() time.Time { return *clock }, DummyPHCHash: "hash:dummy"})
			if e != nil {
				t.Fatal(e)
			}
			command := LoginCommand{Username: "operator", Password: "correct-password", Remote: "203.0.113.10", CLI: true}
			issued, e := service.Login(context.Background(), command)
			if e != nil {
				t.Fatal(e)
			}
			if issued.Kind != "cli" || issued.ExpiresAt.IsZero() != bound {
				t.Fatal("wrong CLI policy")
			}
			ctx := accessport.WithCLIRequest(context.Background(), accessport.CLIRequest{Token: issued.SessionToken, Source: netip.MustParseAddr(command.Remote)})
			if _, e = service.Authenticate(ctx, issued.SessionToken); e != nil {
				t.Fatal(e)
			}
			if _, e = service.Authenticate(context.Background(), issued.SessionToken); !errors.Is(e, domain.ErrAuthentication) {
				t.Fatal("CLI cookie accepted", e)
			}
			if _, e = service.AuthorizeCSRF(ctx, issued.SessionToken, "wrong", issued.CSRFToken); !errors.Is(e, domain.ErrCSRFRequired) {
				t.Fatal("write CSRF bypass", e)
			}
			*clock = clock.Add(24*time.Hour - time.Second)
			if _, e = service.Authenticate(ctx, issued.SessionToken); e != nil {
				t.Fatal("early expiry", e)
			}
			*clock = clock.Add(time.Second)
			_, e = service.Authenticate(ctx, issued.SessionToken)
			if (e == nil) != bound {
				t.Fatal("24h boundary", e)
			}
			if bound {
				bad := accessport.WithCLIRequest(context.Background(), accessport.CLIRequest{Token: issued.SessionToken, Source: netip.MustParseAddr("198.51.100.1")})
				if _, e = service.Authenticate(bad, issued.SessionToken); !errors.Is(e, domain.ErrPermissionDenied) {
					t.Fatal("source bypass", e)
				}
			}
			*clock = testNow
			browser, e := service.Login(context.Background(), LoginCommand{Username: "operator", Password: "correct-password", Remote: command.Remote})
			if e != nil {
				t.Fatal(e)
			}
			if !browser.ExpiresAt.Equal(testNow.Add(8 * time.Hour)) {
				t.Fatal("browser changed")
			}
			if _, e = service.Authenticate(ctx, browser.SessionToken); !errors.Is(e, domain.ErrAuthentication) {
				t.Fatal("browser used as CLI")
			}
			if _, e = service.ReadCLIPolicy(context.Background(), domain.Principal{Roles: []domain.Role{domain.RoleViewer}}, 1); !errors.Is(e, domain.ErrPermissionDenied) {
				t.Fatal("viewer policy access")
			}
			user := repo.users[1]
			actor := domain.Principal{Kind: domain.KindAdmin, InternalID: 1, Roles: user.Roles, SessionVersion: user.SessionVersion}
			if _, e = service.SetCLIPolicy(context.Background(), actor, 1, []string{}); e != nil {
				t.Fatal(e)
			}
			if _, e = service.Authenticate(ctx, issued.SessionToken); !errors.Is(e, domain.ErrAuthentication) {
				t.Fatal("old policy survived", e)
			}
			if _, e = service.Authenticate(context.Background(), browser.SessionToken); e != nil {
				t.Fatal("policy revoked browser", e)
			}
			fresh, e := service.Login(context.Background(), command)
			if e != nil {
				t.Fatal(e)
			}
			freshCtx := accessport.WithCLIRequest(context.Background(), accessport.CLIRequest{Token: fresh.SessionToken, Source: netip.MustParseAddr(command.Remote)})
			if e = repo.SetPasswordHash(context.Background(), 1, "hash:new-password", *clock); e != nil {
				t.Fatal(e)
			}
			if _, e = service.Authenticate(freshCtx, fresh.SessionToken); !errors.Is(e, domain.ErrAuthentication) {
				t.Fatal("password reset token survived", e)
			}
			if _, e = service.Authenticate(context.Background(), browser.SessionToken); !errors.Is(e, domain.ErrAuthentication) {
				t.Fatal("password reset browser survived", e)
			}
			if _, e = service.Login(context.Background(), command); !errors.Is(e, domain.ErrInvalidCredentials) {
				t.Fatal("old password login accepted", e)
			}
		})
	}
}

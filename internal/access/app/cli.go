package app

import (
	"context"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	accessport "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	"net/netip"
	"strings"
	"time"
)

func (service *Authentication) cliRepository() (accessport.CLIRepository, error) {
	repo, ok := service.repository.(accessport.CLIRepository)
	if !ok {
		return nil, domain.ErrAuthentication
	}
	return repo, nil
}
func (service *Authentication) ReadCLIPolicy(ctx context.Context, actor domain.Principal, userID int64) (accessport.CLIPolicy, error) {
	if !actor.IsSuperAdmin() {
		return accessport.CLIPolicy{}, domain.ErrPermissionDenied
	}
	repo, err := service.cliRepository()
	if err != nil {
		return accessport.CLIPolicy{}, err
	}
	var result accessport.CLIPolicy
	err = service.uow.Within(ctx, func(tx context.Context) error {
		if _, err := service.repository.UserByID(tx, userID, false); err != nil {
			return err
		}
		var e error
		result, e = repo.CLIPolicy(tx, userID)
		return e
	})
	return result, err
}
func (service *Authentication) SetCLIPolicy(ctx context.Context, actor domain.Principal, userID int64, cidrs []string) (accessport.CLIPolicy, error) {
	if !actor.IsSuperAdmin() {
		return accessport.CLIPolicy{}, domain.ErrPermissionDenied
	}
	// The account policy accepts human-entered IPs; keep machine-client CIDR
	// contracts unchanged. Storage and source matching still use canonical CIDRs.
	sources := make([]string, len(cidrs))
	for i, raw := range cidrs {
		value := strings.TrimSpace(raw)
		if addr, e := netip.ParseAddr(value); e == nil && addr.Zone() == "" {
			value = netip.PrefixFrom(addr, addr.BitLen()).String()
		}
		sources[i] = value
	}
	normalized, err := domain.NormalizeCIDRs(sources)
	if err != nil {
		return accessport.CLIPolicy{}, err
	}
	repo, err := service.cliRepository()
	if err != nil {
		return accessport.CLIPolicy{}, err
	}
	var result accessport.CLIPolicy
	err = service.uow.Within(ctx, func(tx context.Context) error {
		// Serialize password/policy changes and login on the existing account row.
		current, e := service.repository.UserByID(tx, actor.InternalID, true)
		if e != nil {
			return e
		}
		if !current.Active || !current.LoginEnabled || current.SessionVersion != actor.SessionVersion || !current.HasRole(domain.RoleSuperAdmin) {
			return domain.ErrPermissionDenied
		}
		if _, e = service.repository.UserByID(tx, userID, true); e != nil {
			return e
		}
		result, e = repo.ReplaceCLIPolicy(tx, userID, normalized)
		if e != nil {
			return e
		}
		return service.repository.AppendAccessAudit(tx, domain.AccessAudit{ActorAdminUserID: actor.InternalID, TargetAdminUserID: userID, Action: "cli_policy_changed", Details: []byte("{}"), CreatedAt: service.config.Now().UTC()})
	})
	return result, err
}
func (service *Authentication) validateSession(ctx context.Context, s domain.Session, token string, now time.Time) error {
	cli, bearer := accessport.CLIRequestFrom(ctx)
	isCLI := s.CredentialKind == "cli"
	if isCLI != bearer || (bearer && (cli.Token != token || !cli.Source.IsValid())) {
		return domain.ErrAuthentication
	}
	if s.RevokedAt != nil || !s.User.Active || !s.User.LoginEnabled || s.User.AccessGrantedAt == nil || s.SessionVersion != s.User.SessionVersion {
		return domain.ErrAuthentication
	}
	if !s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt) {
		return domain.ErrAuthentication
	}
	if !isCLI {
		if s.ExpiresAt.IsZero() {
			return domain.ErrAuthentication
		}
		return nil
	}
	repo, err := service.cliRepository()
	if err != nil {
		return err
	}
	policy, err := repo.CLIPolicy(ctx, s.AdminUserID)
	if err != nil {
		return err
	}
	if s.CLIPolicyVersion != policy.Version {
		return domain.ErrAuthentication
	}
	if len(policy.AllowedCIDRs) == 0 && s.ExpiresAt.IsZero() {
		return domain.ErrAuthentication
	}
	if !domain.MachineSourceAllowed(policy.AllowedCIDRs, cli.Source) {
		return domain.ErrPermissionDenied
	}
	return nil
}
func (service *Authentication) cliSessionPolicy(ctx context.Context, userID int64, source string) (accessport.CLIPolicy, error) {
	repo, err := service.cliRepository()
	if err != nil {
		return accessport.CLIPolicy{}, err
	}
	policy, err := repo.CLIPolicy(ctx, userID)
	if err != nil {
		return policy, err
	}
	addr, err := netip.ParseAddr(source)
	if err != nil {
		return policy, domain.ErrAuthentication
	}
	if !domain.MachineSourceAllowed(policy.AllowedCIDRs, addr) {
		return policy, domain.ErrPermissionDenied
	}
	return policy, nil
}

type cliLoginSourceKey struct{}

func withCLILoginSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, cliLoginSourceKey{}, source)
}
func cliLoginSource(ctx context.Context) string {
	s, _ := ctx.Value(cliLoginSourceKey{}).(string)
	return s
}

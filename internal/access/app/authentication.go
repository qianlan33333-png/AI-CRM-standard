package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/credential"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	accessport "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	platformport "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/port"
)

type Passwords interface {
	Hash(string) (string, error)
	Verify(string, string) bool
}

type AuthenticationConfig struct {
	SessionTTL   time.Duration
	Window       time.Duration
	MaxFailures  int
	BlockFor     time.Duration
	Now          func() time.Time
	DummyPHCHash string
}

type Authentication struct {
	repository accessport.Repository
	uow        platformport.UnitOfWork
	passwords  Passwords
	config     AuthenticationConfig
}

type LoginCommand struct {
	CLI      bool
	Username string
	Password string
	Remote   string
}

type WeComLoginCommand struct {
	WeComUserID string
	Remote      string
}

type IssuedSession struct {
	Kind         string
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
	User         UserSummary
}

func NewAuthentication(repository accessport.Repository, uow platformport.UnitOfWork, passwords Passwords, config AuthenticationConfig) (*Authentication, error) {
	if repository == nil || uow == nil || passwords == nil {
		return nil, errors.New("access authentication dependencies are required")
	}
	if config.SessionTTL <= 0 {
		config.SessionTTL = 8 * time.Hour
	}
	if config.Window <= 0 {
		config.Window = 15 * time.Minute
	}
	if config.MaxFailures <= 0 {
		config.MaxFailures = 5
	}
	if config.BlockFor <= 0 {
		config.BlockFor = 15 * time.Minute
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.DummyPHCHash == "" {
		return nil, errors.New("dummy Argon2id hash is required")
	}
	return &Authentication{repository: repository, uow: uow, passwords: passwords, config: config}, nil
}

func (service *Authentication) Login(ctx context.Context, command LoginCommand) (IssuedSession, error) {
	if command.CLI {
		ctx = withCLILoginSource(ctx, command.Remote)
	}
	candidateUsername := strings.ToLower(strings.TrimSpace(command.Username))
	username, usernameErr := domain.NormalizeUsername(candidateUsername)
	now := service.config.Now().UTC()
	identifierDigest := credential.Digest(candidateUsername)
	remoteDigest := credential.Digest(strings.TrimSpace(command.Remote))
	rateDigest := credential.Digest(candidateUsername + "\x00" + strings.TrimSpace(command.Remote))
	var issued IssuedSession
	var decisionErr error
	err := service.uow.Within(ctx, func(txContext context.Context) error {
		limit, loadErr := service.repository.LoginRateLimit(txContext, rateDigest, true)
		if loadErr != nil {
			return loadErr
		}
		if now.Sub(limit.WindowStartedAt) >= service.config.Window {
			limit.WindowStartedAt = now
			limit.FailureCount = 0
			limit.BlockedUntil = nil
		}
		if limit.BlockedUntil != nil && now.Before(*limit.BlockedUntil) {
			if err := service.auditLogin(txContext, nil, identifierDigest, remoteDigest, "rate_limited", "threshold_exceeded", now); err != nil {
				return err
			}
			if usernameErr != nil {
				decisionErr = domain.ErrInvalidCredentials
			} else {
				decisionErr = domain.ErrRateLimited
			}
			return nil
		}

		lookupUsername := username
		if usernameErr != nil {
			// This value cannot satisfy the database username constraint, but still
			// executes the same indexed lookup as a syntactically valid identifier.
			lookupUsername = strings.Repeat("x", 121)
		}
		user, userErr := service.repository.UserByUsername(txContext, lookupUsername, true)
		validPassword := false
		if userErr == nil {
			validPassword = service.passwords.Verify(command.Password, user.PasswordHash)
		} else if errors.Is(userErr, domain.ErrNotFound) {
			_ = service.passwords.Verify(command.Password, service.config.DummyPHCHash)
		} else {
			return userErr
		}
		if userErr != nil || !validPassword || !user.Active || !user.LoginEnabled || user.AccessGrantedAt == nil {
			limit.FailureCount++
			limit.UpdatedAt = now
			if limit.FailureCount >= service.config.MaxFailures {
				blockedUntil := now.Add(service.config.BlockFor)
				limit.BlockedUntil = &blockedUntil
			}
			if saveErr := service.repository.SaveLoginRateLimit(txContext, limit); saveErr != nil {
				return saveErr
			}
			outcome := "invalid_credentials"
			reason := "credentials_rejected"
			if usernameErr != nil {
				reason = "malformed_identifier"
			}
			var userID *int64
			if userErr == nil {
				userID = &user.ID
				if !user.Active || !user.LoginEnabled || user.AccessGrantedAt == nil {
					outcome, reason = "disabled", "account_disabled"
				}
			}
			if err := service.auditLogin(txContext, userID, identifierDigest, remoteDigest, outcome, reason, now); err != nil {
				return err
			}
			decisionErr = domain.ErrInvalidCredentials
			return nil
		}
		if len(user.Roles) == 0 {
			if err := service.auditLogin(txContext, &user.ID, identifierDigest, remoteDigest, "invalid_credentials", "no_roles", now); err != nil {
				return err
			}
			decisionErr = domain.ErrPermissionDenied
			return nil
		}

		limit.FailureCount = 0
		limit.BlockedUntil = nil
		limit.WindowStartedAt = now
		limit.UpdatedAt = now
		if err := service.repository.SaveLoginRateLimit(txContext, limit); err != nil {
			return err
		}
		var issueErr error
		issued, issueErr = service.issueSession(txContext, user, identifierDigest, remoteDigest, func() string {
			if command.CLI {
				return "cli_password"
			}
			return "local_password"
		}(), now)
		return issueErr
	})
	if err != nil {
		return IssuedSession{}, err
	}
	if decisionErr != nil {
		return IssuedSession{}, decisionErr
	}
	return issued, nil
}

// LoginWithWeComUserID is the sole access-owned bridge from a provider-verified
// WeCom employee identity to the same database session used by local passwords.
// Composition root adapters may expose it to wecom ports; wecom must not import app.
func (service *Authentication) LoginWithWeComUserID(ctx context.Context, command WeComLoginCommand) (IssuedSession, error) {
	candidate := strings.TrimSpace(command.WeComUserID)
	wecomUserID, validationErr := domain.NormalizeWeComUserID(candidate)
	now := service.config.Now().UTC()
	identifierDigest := credential.Digest(candidate)
	remoteDigest := credential.Digest(strings.TrimSpace(command.Remote))
	var issued IssuedSession
	var decisionErr error
	err := service.uow.Within(ctx, func(txContext context.Context) error {
		if validationErr != nil {
			if err := service.auditLogin(txContext, nil, identifierDigest, remoteDigest, "invalid_credentials", "malformed_wecom_userid", now); err != nil {
				return err
			}
			decisionErr = domain.ErrInvalidCredentials
			return nil
		}
		user, err := service.repository.UserByWeComUserID(txContext, wecomUserID, true)
		if errors.Is(err, domain.ErrNotFound) {
			if auditErr := service.auditLogin(txContext, nil, identifierDigest, remoteDigest, "invalid_credentials", "wecom_user_not_authorized", now); auditErr != nil {
				return auditErr
			}
			decisionErr = domain.ErrInvalidCredentials
			return nil
		}
		if err != nil {
			return err
		}
		if !user.Active || !user.LoginEnabled || user.AccessGrantedAt == nil {
			if auditErr := service.auditLogin(txContext, &user.ID, identifierDigest, remoteDigest, "disabled", "account_disabled", now); auditErr != nil {
				return auditErr
			}
			decisionErr = domain.ErrInvalidCredentials
			return nil
		}
		if len(user.Roles) == 0 {
			if auditErr := service.auditLogin(txContext, &user.ID, identifierDigest, remoteDigest, "invalid_credentials", "no_roles", now); auditErr != nil {
				return auditErr
			}
			decisionErr = domain.ErrPermissionDenied
			return nil
		}
		issued, err = service.issueSession(txContext, user, identifierDigest, remoteDigest, "wecom_oauth", now)
		return err
	})
	if err != nil {
		return IssuedSession{}, err
	}
	if decisionErr != nil {
		return IssuedSession{}, decisionErr
	}
	return issued, nil
}

func (service *Authentication) issueSession(ctx context.Context, user domain.User, identifierDigest, remoteDigest [32]byte, reason string, now time.Time) (IssuedSession, error) {
	kind, prefix := "browser", "as_"
	if reason == "cli_password" {
		kind, prefix = "cli", "acl_"
	}
	sessionToken, sessionDigest, err := credential.IssueOpaque(prefix)
	if err != nil {
		return IssuedSession{}, err
	}
	csrfToken, csrfDigest, err := credential.IssueOpaque("ac_")
	if err != nil {
		return IssuedSession{}, err
	}
	expires := now.Add(service.config.SessionTTL)
	policyVersion := int64(1)
	if kind == "cli" {
		policy, e := service.cliSessionPolicy(ctx, user.ID, cliLoginSource(ctx))
		if e != nil {
			return IssuedSession{}, e
		}
		policyVersion = policy.Version
		expires = now.Add(24 * time.Hour)
		if len(policy.AllowedCIDRs) > 0 {
			expires = time.Time{}
		}
	}
	if _, err = service.repository.CreateSession(ctx, domain.Session{
		AuthenticationMethod: reason,
		TokenDigest:          sessionDigest, CSRFTokenDigest: csrfDigest,
		AdminUserID: user.ID, SessionVersion: user.SessionVersion, ExpiresAt: expires, CredentialKind: kind, CLIPolicyVersion: policyVersion,
	}); err != nil {
		return IssuedSession{}, err
	}
	if err = service.repository.SetLastLogin(ctx, user.ID, now); err != nil {
		return IssuedSession{}, err
	}
	if err = service.repository.AppendLoginAudit(ctx, domain.LoginAudit{
		AdminUserID: &user.ID, IdentifierDigest: identifierDigest, RemoteDigest: remoteDigest,
		Outcome: "succeeded", Reason: reason, CreatedAt: now,
	}); err != nil {
		return IssuedSession{}, err
	}
	user.LastLoginAt = &now
	return IssuedSession{Kind: kind, SessionToken: sessionToken, CSRFToken: csrfToken, ExpiresAt: expires, User: summarizeUser(user)}, nil
}

func (service *Authentication) Authenticate(ctx context.Context, sessionToken string) (domain.Principal, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return domain.Principal{}, domain.ErrAuthentication
	}
	now := service.config.Now().UTC()
	var principal domain.Principal
	err := service.uow.Within(ctx, func(txContext context.Context) error {
		session, err := service.repository.SessionByTokenDigest(txContext, credential.Digest(sessionToken), false)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return domain.ErrAuthentication
			}
			return err
		}
		if validationErr := service.validateSession(txContext, session, sessionToken, now); validationErr != nil {
			return validationErr
		}
		if len(session.User.Roles) == 0 {
			return domain.ErrPermissionDenied
		}
		if err = service.repository.TouchSession(txContext, session.ID, now); err != nil {
			return err
		}
		principal = domain.Principal{Kind: domain.KindAdmin, InternalID: session.User.ID, Roles: session.User.Roles, SessionVersion: session.SessionVersion}
		return nil
	})
	return principal, err
}

func (service *Authentication) AuthorizeCSRF(ctx context.Context, sessionToken, csrfCookie, csrfRequest string) (domain.Principal, error) {
	if sessionToken == "" || csrfCookie == "" || csrfRequest == "" {
		return domain.Principal{}, domain.ErrCSRFRequired
	}
	now := service.config.Now().UTC()
	var principal domain.Principal
	err := service.uow.Within(ctx, func(txContext context.Context) error {
		session, err := service.repository.SessionByTokenDigest(txContext, credential.Digest(sessionToken), false)
		if err != nil || service.validateSession(txContext, session, sessionToken, now) != nil {
			return domain.ErrAuthentication
		}
		if !credential.Matches(csrfCookie, session.CSRFTokenDigest) || !credential.Matches(csrfRequest, session.CSRFTokenDigest) {
			return domain.ErrCSRFRequired
		}
		principal = domain.Principal{Kind: domain.KindAdmin, InternalID: session.User.ID, Roles: session.User.Roles, SessionVersion: session.SessionVersion}
		return nil
	})
	return principal, err
}

func (service *Authentication) Logout(ctx context.Context, sessionToken, csrfCookie, csrfRequest string) error {
	if _, err := service.AuthorizeCSRF(ctx, sessionToken, csrfCookie, csrfRequest); err != nil {
		return err
	}
	return service.uow.Within(ctx, func(txContext context.Context) error {
		_, err := service.repository.RevokeSession(txContext, credential.Digest(sessionToken), "logout", service.config.Now().UTC())
		return err
	})
}

func (service *Authentication) auditLogin(ctx context.Context, userID *int64, identifier, remote [32]byte, outcome, reason string, now time.Time) error {
	return service.repository.AppendLoginAudit(ctx, domain.LoginAudit{
		AdminUserID: userID, IdentifierDigest: identifier, RemoteDigest: remote,
		Outcome: outcome, Reason: reason, CreatedAt: now,
	})
}

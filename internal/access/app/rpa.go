package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/credential"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	platform "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/port"
)

type RPADeviceService struct {
	UOW     platform.UnitOfWork
	Users   p.Repository
	Devices p.RPADevices
	Origin  string
	Now     func() time.Time
}
type RPAPairStart struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
}
type RPACredential struct {
	Token        string `json:"token"`
	StaffID      string `json:"staff_id"`
	SenderUserID string `json:"sender_userid"`
	DisplayName  string `json:"display_name"`
}
type RPAPairPoll struct {
	State      string         `json:"state"`
	Credential *RPACredential `json:"credential,omitempty"`
}
type RPAPairPreview struct {
	Name  string `json:"device_name"`
	State string `json:"state"`
}

func (s *Authentication) VerifyRPAAccountSession(ctx context.Context, token string) (out p.RPAAccountSession, e error) {
	e = s.uow.Within(ctx, func(tx context.Context) error {
		session, e := s.repository.SessionByTokenDigest(tx, credential.Digest(token), false)
		if e != nil || s.validateSession(tx, session, token, s.config.Now().UTC()) != nil || session.AuthenticationMethod != "wecom_oauth" || !rpaUser(session.User) {
			return domain.ErrAuthentication
		}
		out = p.RPAAccountSession{StaffID: session.User.ID, SenderUserID: session.User.WeComUserID, IssuedAt: session.CreatedAt}
		return nil
	})
	return
}

func (s *RPADeviceService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *RPADeviceService) limit(ctx context.Context, subject string, max int) error {
	now := s.now()
	key := sha256.Sum256([]byte("rpa-device-pair:" + subject))
	l, e := s.Users.LoginRateLimit(ctx, key, true)
	if e != nil {
		return e
	}
	if now.Sub(l.WindowStartedAt) >= 10*time.Minute {
		l.FailureCount = 0
		l.WindowStartedAt = now
	}
	if l.FailureCount >= max {
		return domain.ErrRateLimited
	}
	l.FailureCount++
	l.UpdatedAt = now
	return s.Users.SaveLoginRateLimit(ctx, l)
}
func (s *RPADeviceService) Begin(ctx context.Context, name, source string) (out RPAPairStart, e error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 || strings.ContainsAny(name, "\x00\r\n") || source == "" {
		return out, domain.ErrInvalidInput
	}
	token, _, e := credential.IssueOpaque("arp_")
	if e != nil {
		return out, e
	}
	var random [5]byte
	if _, e = rand.Read(random[:]); e != nil {
		return out, e
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random[:])
	now := s.now()
	d := p.RPADevice{TokenDigest: sha256.Sum256([]byte(token)), UserCodeDigest: sha256.Sum256([]byte(code)), Name: name, State: "pending", PairExpiresAt: now.Add(10 * time.Minute), ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now}
	e = s.UOW.Within(ctx, func(tx context.Context) error {
		if e := s.limit(tx, "begin:"+source, 10); e != nil {
			return e
		}
		return s.Devices.CreateRPADevice(tx, d)
	})
	if e == nil {
		out = RPAPairStart{token, code, strings.TrimRight(s.Origin, "/") + "/admin/automation-conversion/group-ops/ui?rpa_pair=" + code}
	}
	return
}
func rpaUser(u domain.User) bool {
	return u.Active && u.LoginEnabled && u.AccessGrantedAt != nil && u.WeComUserID != "" && (u.HasRole(domain.RoleAdmin) || u.HasRole(domain.RoleSuperAdmin))
}
func (s *RPADeviceService) authenticated(ctx context.Context, d p.RPADevice) (p.RPAPrincipal, error) {
	if d.State != "active" || !s.now().Before(d.ExpiresAt) {
		return p.RPAPrincipal{}, domain.ErrAuthentication
	}
	u, e := s.Users.UserByID(ctx, d.StaffID, false)
	if e != nil || !rpaUser(u) || u.SessionVersion != d.SessionVersion || u.WeComUserID != d.SenderUserID {
		return p.RPAPrincipal{}, domain.ErrAuthentication
	}
	return p.RPAPrincipal{DeviceID: d.ID, StaffID: u.ID, SenderUserID: u.WeComUserID, DisplayName: u.DisplayName}, nil
}
func (s *RPADeviceService) AuthenticateRPA(ctx context.Context, token string) (out p.RPAPrincipal, e error) {
	if !strings.HasPrefix(token, "arp_") || len(token) > 100 {
		return out, domain.ErrAuthentication
	}
	e = s.UOW.Within(ctx, func(tx context.Context) error {
		d, e := s.Devices.RPADeviceByToken(tx, sha256.Sum256([]byte(token)), false)
		if e != nil {
			return domain.ErrAuthentication
		}
		out, e = s.authenticated(tx, d)
		return e
	})
	return
}
func (s *RPADeviceService) Poll(ctx context.Context, token string) (out RPAPairPoll, e error) {
	if !strings.HasPrefix(token, "arp_") || len(token) > 100 {
		return out, domain.ErrAuthentication
	}
	e = s.UOW.Within(ctx, func(tx context.Context) error {
		d, e := s.Devices.RPADeviceByToken(tx, sha256.Sum256([]byte(token)), false)
		if e != nil {
			return domain.ErrAuthentication
		}
		if d.State == "pending" && s.now().Before(d.PairExpiresAt) {
			out.State = "pending"
			return nil
		}
		a, e := s.authenticated(tx, d)
		if e != nil {
			out.State = "expired"
			return nil
		}
		out.State = "approved"
		out.Credential = &RPACredential{token, strconv.FormatInt(a.StaffID, 10), a.SenderUserID, a.DisplayName}
		return nil
	})
	return
}
func normalizeRPACode(code string) (string, error) {
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if len(code) != 8 {
		return "", domain.ErrInvalidInput
	}
	if _, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(code); e != nil {
		return "", domain.ErrInvalidInput
	}
	return code, nil
}
func (s *RPADeviceService) Pair(ctx context.Context, actor domain.Principal, code string, approve bool) (out RPAPairPreview, e error) {
	if actor.Kind != domain.KindAdmin {
		return out, domain.ErrPermissionDenied
	}
	code, e = normalizeRPACode(code)
	if e != nil {
		return out, e
	}
	var decision error
	e = s.UOW.Within(ctx, func(tx context.Context) error {
		u, e := s.Users.UserByID(tx, actor.InternalID, true)
		if e != nil || !rpaUser(u) || u.SessionVersion != actor.SessionVersion {
			return domain.ErrPermissionDenied
		}
		if e = s.limit(tx, "approval:"+strconv.FormatInt(u.ID, 10), 30); e != nil {
			return e
		}
		d, e := s.Devices.RPADeviceByUserCode(tx, sha256.Sum256([]byte(code)), true)
		if e != nil || !s.now().Before(d.PairExpiresAt) || (d.State != "pending" && d.StaffID != u.ID) {
			decision = domain.ErrNotFound
			return nil
		}
		out = RPAPairPreview{d.Name, d.State}
		if !approve || d.State == "active" {
			return nil
		}
		d.StaffID = u.ID
		d.SenderUserID = u.WeComUserID
		d.SessionVersion = u.SessionVersion
		d.ExpiresAt = s.now().Add(90 * 24 * time.Hour)
		if e = s.Devices.ApproveRPADevice(tx, d); e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"device_id": d.ID, "scope": "wecom_rpa", "initial_state": "paused"})
		if e = s.Users.AppendAccessAudit(tx, domain.AccessAudit{ActorAdminUserID: u.ID, TargetAdminUserID: u.ID, Action: "access.rpa_device_approved", Details: payload, CreatedAt: s.now()}); e != nil {
			return e
		}
		out.State = "active"
		return nil
	})
	if e == nil {
		e = decision
	}
	return
}
func (s *RPADeviceService) Revoke(ctx context.Context, a p.RPAPrincipal) error {
	return s.UOW.Within(ctx, func(tx context.Context) error {
		if e := s.Devices.RevokeRPADevice(tx, a.DeviceID, a.StaffID); e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"device_id": a.DeviceID})
		return s.Users.AppendAccessAudit(tx, domain.AccessAudit{ActorAdminUserID: a.StaffID, TargetAdminUserID: a.StaffID, Action: "access.rpa_device_revoked", Details: payload, CreatedAt: s.now()})
	})
}

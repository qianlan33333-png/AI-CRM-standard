package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	nethttp "net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/app"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	accessport "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
)

func (handler *Handler) cliSource(r *nethttp.Request) (netip.Addr, error) {
	if handler.source != nil {
		return handler.source(r)
	}
	if r.TLS == nil {
		return netip.Addr{}, domain.ErrAuthentication
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return netip.ParseAddr(host)
}
func decodeCLI(w nethttp.ResponseWriter, r *nethttp.Request, dst any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return domain.ErrInvalidInput
	}
	r.Body = nethttp.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return domain.ErrInvalidInput
	}
	if !errors.Is(d.Decode(&struct{}{}), io.EOF) {
		return domain.ErrInvalidInput
	}
	return nil
}
func (handler *Handler) cliLogin(w nethttp.ResponseWriter, r *nethttp.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if origin := r.Header.Get("Origin"); origin != "" && origin != handler.publicOrigin {
		handler.writeError(w, r, domain.ErrPermissionDenied)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeCLI(w, r, &input); err != nil {
		handler.writeError(w, r, err)
		return
	}
	source, err := handler.cliSource(r)
	if err != nil {
		handler.writeError(w, r, domain.ErrAuthentication)
		return
	}
	issued, err := handler.auth.Login(r.Context(), app.LoginCommand{Username: input.Username, Password: input.Password, Remote: source.String(), CLI: true})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	// Fail closed for an older adapter: never turn a browser session into a CLI token.
	if issued.Kind != "cli" || !strings.HasPrefix(issued.SessionToken, "acl_") {
		handler.writeError(w, r, domain.ErrAuthentication)
		return
	}
	var expiry any
	if !issued.ExpiresAt.IsZero() {
		expiry = issued.ExpiresAt
	}
	writeJSON(w, 200, map[string]any{"access_token": issued.SessionToken, "token_type": "Bearer", "csrf_token": issued.CSRFToken, "expires_at": expiry, "automatic_renewal": false})
}
func (handler *Handler) cliLogout(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, ok := accessport.CLIRequestFrom(r.Context()); !ok {
		handler.writeError(w, r, domain.ErrAuthentication)
		return
	}
	session, cookie, request := requestCredentials(r)
	if err := handler.auth.Logout(r.Context(), session, cookie, request); err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

type cliPolicyManager interface {
	ReadCLIPolicy(context.Context, domain.Principal, int64) (accessport.CLIPolicy, error)
	SetCLIPolicy(context.Context, domain.Principal, int64, []string) (accessport.CLIPolicy, error)
}

func (handler *Handler) cliPolicy(w nethttp.ResponseWriter, r *nethttp.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := handler.authenticatedActor(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		handler.writeError(w, r, domain.ErrInvalidInput)
		return
	}
	manager, ok := handler.auth.(cliPolicyManager)
	if !ok {
		handler.writeError(w, r, domain.ErrPermissionDenied)
		return
	}
	var result accessport.CLIPolicy
	if r.Method == "GET" {
		result, err = manager.ReadCLIPolicy(r.Context(), actor, id)
	} else {
		session, cookie, header := requestCredentials(r)
		if _, err = handler.auth.AuthorizeCSRF(r.Context(), session, cookie, header); err != nil {
			handler.writeError(w, r, err)
			return
		}
		var input struct {
			AllowedCIDRs *[]string `json:"allowed_cidrs"`
		}
		if err = decodeCLI(w, r, &input); err != nil || input.AllowedCIDRs == nil {
			handler.writeError(w, r, domain.ErrInvalidInput)
			return
		}
		result, err = manager.SetCLIPolicy(r.Context(), actor, id, *input.AllowedCIDRs)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, 200, result)
}

// CLIBearer adapts a verified terminal credential to existing owner HTTP ports.
// The context purpose remains mandatory in Access: cookies alone cannot use CLI tokens.
// CSRF remains verified by the existing write authorization; no owner is bypassed.
func (handler *Handler) CLIBearer(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		fields := strings.Fields(r.Header.Get("Authorization"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || !strings.HasPrefix(fields[1], "acl_") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if len(r.Header.Values("Authorization")) != 1 || !strings.HasPrefix(r.URL.Path, "/api/") {
			handler.writeError(w, r, domain.ErrAuthentication)
			return
		}
		if _, err := r.Cookie(SessionCookieName); err == nil {
			handler.writeError(w, r, domain.ErrAuthentication)
			return
		}
		source, err := handler.cliSource(r)
		if err != nil {
			handler.writeError(w, r, domain.ErrAuthentication)
			return
		}
		request := r.Clone(accessport.WithCLIRequest(r.Context(), accessport.CLIRequest{Token: fields[1], Source: source}))
		if _, err = handler.auth.Authenticate(request.Context(), fields[1]); err != nil {
			handler.writeError(w, r, err)
			return
		}
		request.AddCookie(&nethttp.Cookie{Name: SessionCookieName, Value: fields[1]})
		if csrf := request.Header.Get("X-CSRF-Token"); csrf != "" {
			request.AddCookie(&nethttp.Cookie{Name: CSRFCookieName, Value: csrf})
		}
		next.ServeHTTP(w, request)
	})
}

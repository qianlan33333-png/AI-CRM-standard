package http

import (
	"context"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/app"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	nethttp "net/http"
	"strings"
)

type RPADeviceApplication interface {
	p.RPAAuthenticator
	Revoke(context.Context, p.RPAPrincipal) error
	Begin(context.Context, string, string) (app.RPAPairStart, error)
	Poll(context.Context, string) (app.RPAPairPoll, error)
	Pair(context.Context, domain.Principal, string, bool) (app.RPAPairPreview, error)
}

func (h *Handler) RPADeviceRoutes(service RPADeviceApplication) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if service == nil || r.Method != nethttp.MethodPost {
			nethttp.NotFound(w, r)
			return
		}
		var result any
		var err error
		switch r.URL.Path {
		case "/api/rpa/v1/device/revoke":
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || parts[0] != "Bearer" || len(r.Header.Values("Authorization")) != 1 || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" {
				h.writeError(w, r, domain.ErrAuthentication)
				return
			}
			actor, e := service.AuthenticateRPA(r.Context(), parts[1])
			if e != nil {
				err = e
				break
			}
			var body struct{}
			if err = decodeCLI(w, r, &body); err != nil {
				break
			}
			err = service.Revoke(r.Context(), actor)
			result = map[string]bool{"ok": true}
		case "/api/rpa/v1/pair/begin", "/api/rpa/v1/pair/poll":
			// Native pairing does not consume browser cookies or ambient credentials.
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Origin") != "" {
				h.writeError(w, r, domain.ErrPermissionDenied)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/begin") {
				var body struct {
					Name string `json:"device_name"`
				}
				if err = decodeCLI(w, r, &body); err != nil {
					break
				}
				source, e := h.cliSource(r)
				if e != nil {
					err = domain.ErrAuthentication
					break
				}
				result, err = service.Begin(r.Context(), body.Name, source.String())
			} else {
				var body struct {
					Code string `json:"device_code"`
				}
				if err = decodeCLI(w, r, &body); err != nil {
					break
				}
				result, err = service.Poll(r.Context(), body.Code)
			}
		case "/api/admin/access/rpa-pair/preview", "/api/admin/access/rpa-pair/approve":
			session, cookie, header := requestCredentials(r)
			actor, e := h.auth.AuthorizeCSRF(r.Context(), session, cookie, header)
			if e != nil {
				err = e
				break
			}
			var body struct {
				Code string `json:"user_code"`
			}
			if err = decodeCLI(w, r, &body); err != nil {
				break
			}
			result, err = service.Pair(r.Context(), actor, body.Code, strings.HasSuffix(r.URL.Path, "/approve"))
		default:
			nethttp.NotFound(w, r)
			return
		}
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		writeJSON(w, 200, result)
	})
}

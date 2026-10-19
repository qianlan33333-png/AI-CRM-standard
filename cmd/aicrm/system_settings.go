package main

import (
	"net/http"
	"net/url"

	accesshttp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/http"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/webshell"
)

// The page exposes only existing Owner APIs. Authorization also happens at
// each API; hiding a tab is never the permission boundary.
func mountSystemSettings(next http.Handler, renderer *webshell.Renderer, auth accessAuthentication) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/config/system" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", 405)
			return
		}
		actor, err := auth.Authenticate(r.Context(), cookieValue(r, accesshttp.SessionCookieName))
		if err != nil {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		if !actor.IsSuperAdmin() {
			http.Error(w, "仅超级管理员可以访问系统配置", http.StatusForbidden)
			return
		}
		data := webshell.AdminPageForRequest(r, "系统配置", "统一管理系统能力与访问设置", "api.admin_config")
		if err = renderer.RenderSystemSettings(w, data); err != nil {
			http.Error(w, "system settings unavailable", 500)
		}
	})
}

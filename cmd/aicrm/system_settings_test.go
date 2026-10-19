package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	accessdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/webshell"
)

func TestSystemSettingsPageRequiresSuperAdmin(t *testing.T) {
	renderer, err := webshell.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		role    accessdomain.Role
		authErr error
		status  int
	}{
		{"super", accessdomain.RoleSuperAdmin, nil, 200}, {"admin", accessdomain.RoleAdmin, nil, 403}, {"viewer", accessdomain.RoleViewer, nil, 403}, {"anonymous", "", errors.New("session missing"), 303},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := &fakeAccessAuthentication{principal: accessdomain.Principal{InternalID: 2, Kind: accessdomain.KindAdmin, Roles: []accessdomain.Role{tc.role}}, err: tc.authErr}
			h := mountSystemSettings(http.NotFoundHandler(), renderer, auth)
			r := httptest.NewRequest("GET", "/admin/config/system", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("sensitive page cacheable")
			}
			if tc.status == 200 {
				for _, text := range []string{"data-system-settings-root", "system_settings.js", "system_settings.css", "CLI 账号策略"} {
					if !strings.Contains(w.Body.String(), text) {
						t.Fatalf("missing %s", text)
					}
				}
				if strings.Count(w.Body.String(), `class="admin-sidebar"`) != 1 {
					t.Fatal("not one admin shell")
				}
			} else if strings.Contains(w.Body.String(), "data-system-settings-root") {
				t.Fatal("unauthorized settings markup")
			}
			next := httptest.NewRecorder()
			h.ServeHTTP(next, httptest.NewRequest("GET", "/admin/config/system/unknown", nil))
			if next.Code != 404 {
				t.Fatal("route widened")
			}
			write := httptest.NewRecorder()
			h.ServeHTTP(write, httptest.NewRequest("POST", "/admin/config/system", nil))
			if write.Code != 405 {
				t.Fatal("page accepted write")
			}
		})
	}
}

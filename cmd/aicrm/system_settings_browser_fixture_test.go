package main

import (
	"context"
	"encoding/base64"
	accesshttp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/http"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/webshell"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	accessapp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/app"
	accessdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	platformconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
)

// Explicit local fixture for manual browser QA against real Access/Config/PG.
// The listener is loopback only and the database helper rejects production URLs.
// No cookies, auth principal, CSRF verification, or HTTP API is replaced.
func TestSystemSettingsLocalBrowserFixture(t *testing.T) {
	stop := os.Getenv("AICRM_SYSTEM_SETTINGS_PREVIEW_STOP")
	if stop == "" {
		t.Skip("explicit local browser fixture not requested")
	}
	if !filepath.IsAbs(stop) {
		t.Fatal("absolute stop marker required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	databaseURL, cleanup := adminAccessCompositionDatabase(t, ctx)
	defer cleanup()
	bootstrap := platformconfig.Bootstrap{Enabled: true, Username: "settings-owner", Password: "synthetic-settings-owner-password", DisplayName: "当前超级管理员"}
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	app, err := compose(ctx, platformconfig.Runtime{
		Role: platformconfig.RoleAPI, DatabaseURL: databaseURL, PublicOrigin: "https://localhost:8791", ReleaseSHA: "system-settings-local-fixture", WorkerOwner: "system-settings-local-fixture", WorkerLimit: 1,
		GroupOps: platformconfig.GroupOps{WebhookSecret: "synthetic-system-settings-webhook-secret"},
		Survey:   platformconfig.Survey{DataKey: key, IdentityPhoneDataKey: key}, Bootstrap: bootstrap,
		AutomationOperations: platformconfig.AutomationOperations{ProviderMode: platformconfig.AutomationProviderDisabled, MaxRecipientsPerRun: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err = app.bootstrap(ctx, bootstrap); err != nil {
		t.Fatal(err)
	}
	owner := accessdomain.Principal{Kind: accessdomain.KindAdmin, InternalID: 1, Roles: []accessdomain.Role{accessdomain.RoleSuperAdmin}, SessionVersion: 1}
	for _, user := range []accessapp.AddUserInput{
		{Username: "settings-operator", Password: "synthetic-settings-operator-password", DisplayName: "运营管理员", Roles: []accessdomain.Role{accessdomain.RoleAdmin}},
		{Username: "settings-viewer", Password: "synthetic-settings-viewer-password", DisplayName: "只读账号", Roles: []accessdomain.Role{accessdomain.RoleViewer}},
	} {
		if _, err = app.management.AddUser(ctx, owner, user); err != nil {
			t.Fatal(err)
		}
	}
	// Seed only this synthetic owner's policy, then exercise all modifications
	// through the browser's authenticated production HTTP handlers.
	if _, err = app.pool.Native().Exec(ctx, `INSERT INTO admin_cli_policies(admin_user_id,allowed_cidrs,version) VALUES(1,ARRAY['115.190.113.5/32','115.190.115.55/32'],2)`); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:8791")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := webshell.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	accessHandler, err := accesshttp.NewHandler(accesshttp.Config{PublicOrigin: "http://localhost:8791", Renderer: renderer, Auth: app.authentication.(accesshttp.Authentication), Management: app.management, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	localAccess := accessHandler.Routes()
	// Use the real Access HTTP handler with the loopback browser origin. Other
	// modules retain their HTTPS public-origin contracts; no Provider is called.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || r.URL.Path == "/logout" || strings.HasPrefix(r.URL.Path, "/api/admin/access/") {
			if isUnsafeMethod(r.Method) && !usesIndependentLoginCSRF(r) && r.Header.Get("Origin") != "http://localhost:8791" {
				http.Error(w, "local fixture origin required", 403)
				return
			}
			securityHeaders(localAccess).ServeHTTP(w, r)
			return
		}
		app.handler.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	t.Log("SYSTEM_SETTINGS_FIXTURE_READY http://localhost:8791/admin/config/system")
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("browser fixture timed out")
		case <-ticker.C:
			if _, err = os.Stat(stop); err == nil {
				return
			}
		}
	}
}

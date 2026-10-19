package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a "github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	app "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/app"
	h "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/http"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

type sidebarEmployee struct{ staff int64 }

func (s sidebarEmployee) SidebarEmployee(_ context.Context, id int64) (p.SidebarActor, error) {
	return p.SidebarActor{StaffID: s.staff, WeComUserID: "current"}, nil
}

type sidebarApplication struct {
	calls int
	actor p.SidebarActor
	err   error
}

func (s *sidebarApplication) SidebarContext(_ context.Context, a p.SidebarActor, _ string, _ string, _ int32, _ int32) (p.SidebarContext, error) {
	s.calls++
	s.actor = a
	return p.SidebarContext{ChatID: "owned"}, s.err
}
func (s *sidebarApplication) BindSidebarGroup(_ context.Context, a p.SidebarActor, c p.SidebarBindCommand) (p.SidebarBindResult, error) {
	s.calls++
	s.actor = a
	return p.SidebarBindResult{State: "bound"}, s.err
}

func TestSidebarGroupBindingUsesManagementPermissionAndCSRF(t *testing.T) {
	for _, tc := range []struct {
		name     string
		security securityStub
		staff    int64
		body     string
		want     int
	}{
		{"admin", adminSecurity(nil), 7, `{"chat_id":"owned","plan_id":"1","expected_revision":1}`, 200},
		{"missing csrf", adminSecurity(errors.New("bad csrf")), 7, `{"chat_id":"owned","plan_id":"1","expected_revision":1}`, 403},
		{"viewer", securityStub{principal: a.Principal{InternalID: 7, Kind: a.KindStaff, Roles: []a.Role{a.RoleViewer}}}, 7, `{}`, 403},
		{"no session", securityStub{}, 7, `{}`, 403},
		{"employee mismatch", adminSecurity(nil), 8, `{}`, 403},
		{"untrusted owner", adminSecurity(nil), 7, `{"chat_id":"owned","plan_id":"1","expected_revision":1,"owner":"current"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &sidebarApplication{}
			handler := h.NewSidebarHandler(s, tc.security, sidebarEmployee{tc.staff})
			r := httptest.NewRequest("POST", h.SidebarBindPath, strings.NewReader(tc.body))
			r.Header.Set("Idempotency-Key", "sidebar-bind-key")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want || (tc.want != 200 && s.calls != 0) {
				t.Fatalf("status=%d calls=%d", w.Code, s.calls)
			}
			if tc.want == 200 && s.actor.StaffID != 7 {
				t.Fatal("caller actor was not the authenticated staff")
			}
		})
	}
}
func TestSidebarGroupContextNoIdentityOrProviderMutationPayload(t *testing.T) {
	s := &sidebarApplication{}
	handler := h.NewSidebarHandler(s, adminSecurity(nil), sidebarEmployee{7})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", h.SidebarContextPath, strings.NewReader(`{"chat_id":"owned"}`)))
	if w.Code != http.StatusOK || s.calls != 1 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("status=%d calls=%d", w.Code, s.calls)
	}
	s.err = app.ErrSidebarForbidden
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", h.SidebarContextPath, strings.NewReader(`{"chat_id":"other"}`)))
	if w.Code != 403 || !strings.Contains(w.Body.String(), "group_scope_denied") {
		t.Fatalf("scope failure %d", w.Code)
	}
}

package http_test

import (
	"context"
	"errors"
	h "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/http"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	"net/http/httptest"
	"strings"
	"testing"
)

type navigationApplication struct {
	calls   int
	command p.NavigationCommand
}

func (s *navigationApplication) ReadNavigation(context.Context, p.SidebarActor) (p.NavigationView, error) {
	s.calls++
	return p.NavigationView{}, nil
}
func (s *navigationApplication) MutateNavigation(_ context.Context, _ p.SidebarActor, c p.NavigationCommand) (p.NavigationRun, error) {
	s.calls++
	s.command = c
	return p.NavigationRun{}, nil
}
func TestNavigationTransportRequiresEmployeeCSRFAndServerTarget(t *testing.T) {
	for _, tc := range []struct {
		name, action, body string
		csrf               error
		staff              int64
		want               int
	}{
		{"start", "start", `{"plan_id":"1","plan_revision":2}`, nil, 7, 200},
		{"csrf", "open", `{"run_id":"1","revision":2}`, errors.New("invalid"), 7, 403},
		{"foreign employee", "open", `{"run_id":"1","revision":2}`, nil, 8, 403},
		{"no arbitrary target", "open", `{"run_id":"1","revision":2,"chatId":"untrusted"}`, nil, 7, 400},
		{"no messages", "open", `{"run_id":"1","revision":2,"msg":"text"}`, nil, 7, 400},
		{"no second scheduler", "claim", `{}`, nil, 7, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &navigationApplication{}
			handler := h.NewNavigationHandler(s, adminSecurity(tc.csrf), sidebarEmployee{tc.staff})
			r := httptest.NewRequest("POST", h.NavigationPath+"/"+tc.action, strings.NewReader(tc.body))
			r.Header.Set("Idempotency-Key", "navigation-test-key")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want || (tc.want != 200 && s.calls != 0) {
				t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body.String(), s.calls)
			}
		})
	}
	s := &navigationApplication{}
	handler := h.NewNavigationHandler(s, adminSecurity(nil), sidebarEmployee{7})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", h.NavigationPath+"?chat_id=arbitrary", nil))
	if w.Code != 200 || s.command.ChatID != "" || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("query affected navigation")
	}
}

package main

import (
	"context"
	"net/http"
	"strings"

	a "github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	platformport "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/port"
)

type sidebarAccessSecurity struct{ authentication accessAuthentication }

func (s sidebarAccessSecurity) Authenticate(ctx context.Context, r *http.Request) (a.Principal, error) {
	return s.authentication.Authenticate(ctx, cookieValue(r, "aicrm_sidebar_session"))
}

func (s sidebarAccessSecurity) AuthorizeCSRF(ctx context.Context, r *http.Request) (a.Principal, error) {
	return s.authentication.AuthorizeCSRF(ctx, cookieValue(r, "aicrm_sidebar_session"), cookieValue(r, "aicrm_sidebar_csrf"), strings.TrimSpace(r.Header.Get("X-CSRF-Token")))
}

type sidebarEmployeeReader struct {
	users accessUserReader
	uow   platformport.UnitOfWork
}

func (s sidebarEmployeeReader) SidebarEmployee(ctx context.Context, id int64) (out p.SidebarActor, err error) {
	err = s.uow.Within(ctx, func(tx context.Context) error {
		u, e := s.users.UserByID(tx, id, false)
		if e != nil || !u.Active || u.WeComUserID == "" {
			return a.ErrAuthentication
		}
		out = p.SidebarActor{StaffID: id, WeComUserID: u.WeComUserID, DisplayName: u.DisplayName}
		return nil
	})
	return
}

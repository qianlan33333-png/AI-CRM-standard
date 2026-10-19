package http

import (
	"context"
	"errors"
	stdhttp "net/http"

	a "github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	app "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/app"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

const SidebarContextPath = "/api/sidebar/v2/group-ops/context"
const SidebarBindPath = "/api/sidebar/v2/group-ops/bind"

type SidebarEmployeeReader interface {
	SidebarEmployee(context.Context, int64) (p.SidebarActor, error)
}

// Security is the existing Access service with the sidebar cookie names.
// Authorization deliberately uses the same canWrite predicate as admin APIs.
func NewSidebarHandler(service p.SidebarBindings, security RequestSecurity, employees SidebarEmployeeReader) stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	for _, path := range []string{SidebarContextPath, SidebarBindPath} {
		mux.HandleFunc("POST "+path, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			if service == nil || security == nil || employees == nil {
				writeError(w, 503, "group_ops_unavailable")
				return
			}
			var principal a.Principal
			var err error
			principal, err = security.Authenticate(r.Context(), r)
			if err != nil {
				writeError(w, 401, "authentication_required")
				return
			}
			if !canWrite(principal) {
				writeError(w, 403, "permission_denied")
				return
			}
			if r.URL.Path == SidebarBindPath {
				checked, csrfErr := security.AuthorizeCSRF(r.Context(), r)
				if csrfErr != nil || checked.InternalID != principal.InternalID || !canWrite(checked) {
					writeError(w, 403, "permission_denied")
					return
				}
			}
			actor, err := employees.SidebarEmployee(r.Context(), principal.InternalID)
			if err != nil || actor.StaffID != principal.InternalID || actor.WeComUserID == "" {
				writeError(w, 403, "permission_denied")
				return
			}
			var result any
			if r.URL.Path == SidebarContextPath {
				var body struct {
					ChatID string `json:"chat_id"`
					Query  string `json:"q"`
					Limit  int32  `json:"limit"`
					Offset int32  `json:"offset"`
				}
				if !decodeJSON(r, &body) {
					writeError(w, 400, "invalid_request")
					return
				}
				if body.Limit == 0 {
					body.Limit = app.DefaultLimit
				}
				result, err = service.SidebarContext(r.Context(), actor, body.ChatID, body.Query, body.Limit, body.Offset)
			} else {
				var body struct {
					ChatID           string `json:"chat_id"`
					PlanID           int64  `json:"plan_id,string"`
					ExpectedRevision int64  `json:"expected_revision"`
				}
				key, ok := requiredIdempotencyKey(r)
				if !ok || !decodeJSON(r, &body) {
					writeError(w, 400, "invalid_request")
					return
				}
				result, err = service.BindSidebarGroup(r.Context(), actor, p.SidebarBindCommand{ChatID: body.ChatID, PlanID: body.PlanID, ExpectedRevision: body.ExpectedRevision, IdempotencyKey: key})
			}
			if errors.Is(err, app.ErrSidebarForbidden) {
				writeError(w, 403, "group_scope_denied")
				return
			}
			(&Handler{}).respond(w, result, err)
		})
	}
	return mux
}

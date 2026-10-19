package http

import (
	"errors"
	access "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	"net/http"
	"strings"

	app "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/app"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

const NavigationPath = "/api/sidebar/v2/group-navigation"

func NewNavigationHandler(service p.Navigation, security RequestSecurity, employees SidebarEmployeeReader, accounts ...access.RPAAccountVerifier) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if service == nil || security == nil || employees == nil {
			writeError(w, 503, "group_ops_unavailable")
			return
		}
		principal, err := security.Authenticate(r.Context(), r)
		if err != nil {
			writeError(w, 401, "authentication_required")
			return
		}
		if !canWrite(principal) {
			writeError(w, 403, "permission_denied")
			return
		}
		actor, err := employees.SidebarEmployee(r.Context(), principal.InternalID)
		if err != nil || actor.StaffID != principal.InternalID || actor.WeComUserID == "" {
			writeError(w, 403, "permission_denied")
			return
		}
		var result any
		if r.Method == http.MethodGet && r.URL.Path == NavigationPath {
			result, err = service.ReadNavigation(r.Context(), actor)
		} else if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, NavigationPath+"/") {
			checked, e := security.AuthorizeCSRF(r.Context(), r)
			if e != nil || checked.InternalID != principal.InternalID || !canWrite(checked) {
				writeError(w, 403, "permission_denied")
				return
			}
			action := strings.TrimPrefix(r.URL.Path, NavigationPath+"/")
			switch action {
			case "start", "open", "confirm", "pause", "resume", "cancel":
			default:
				http.NotFound(w, r)
				return
			}
			var body struct {
				PlanID       int64  `json:"plan_id,string"`
				PlanRevision int64  `json:"plan_revision"`
				RunID        int64  `json:"run_id,string"`
				Revision     int64  `json:"revision"`
				Step         int    `json:"step"`
				ChatID       string `json:"chat_id"`
				Mode         string `json:"mode"`
			}
			if !decodeJSON(r, &body) {
				writeError(w, 400, "invalid_request")
				return
			}
			key := ""
			if action == "start" {
				var ok bool
				key, ok = requiredIdempotencyKey(r)
				if !ok {
					writeError(w, 400, "invalid_request")
					return
				}
			}
			command := p.NavigationCommand{Action: action, PlanID: body.PlanID, PlanRevision: body.PlanRevision, RunID: body.RunID, Revision: body.Revision, Step: body.Step, ChatID: body.ChatID, Key: key, Mode: body.Mode}
			if action == "confirm" && len(accounts) == 1 {
				if cookie, e := r.Cookie("aicrm_sidebar_session"); e == nil {
					if fact, e := accounts[0].VerifyRPAAccountSession(r.Context(), cookie.Value); e == nil {
						command.AccountStaffID = fact.StaffID
						command.AccountSenderUserID = fact.SenderUserID
						command.AccountIssuedAt = fact.IssuedAt
					}
				}
			}
			result, err = service.MutateNavigation(r.Context(), actor, command)
		} else {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, app.ErrSidebarForbidden) {
			writeError(w, 403, "group_scope_denied")
			return
		}
		(&Handler{}).respond(w, result, err)
	})
}

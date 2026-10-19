package http

import (
	"errors"
	"net/http"
	"strconv"

	access "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	app "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/app"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

const TaskPackagePath = "/api/admin/automation-conversion/group-ops/rpa-package"

func NewTaskPackageHandler(service p.TaskPackages, security RequestSecurity, employees SidebarEmployeeReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		principal, e := security.Authenticate(r.Context(), r)
		if e != nil {
			writeError(w, 401, "authentication_required")
			return
		}
		if !canWrite(principal) {
			writeError(w, 403, "permission_denied")
			return
		}
		actor, e := employees.SidebarEmployee(r.Context(), principal.InternalID)
		if e != nil || actor.StaffID != principal.InternalID {
			writeError(w, 403, "permission_denied")
			return
		}
		var result any
		switch r.Method {
		case "GET":
			plan, err := strconv.ParseInt(r.URL.Query().Get("plan_id"), 10, 64)
			if err != nil || plan < 1 {
				writeError(w, 400, "invalid_request")
				return
			}
			result, e = service.ReadTaskPackage(r.Context(), actor, plan)
		case "POST":
			if _, ok := access.CLIRequestFrom(r.Context()); !ok {
				writeError(w, 403, "cli_required")
				return
			}
			checked, err := security.AuthorizeCSRF(r.Context(), r)
			if err != nil || checked.InternalID != principal.InternalID || !canWrite(checked) {
				writeError(w, 403, "permission_denied")
				return
			}
			key, ok := requiredIdempotencyKey(r)
			if !ok {
				writeError(w, 400, "invalid_request")
				return
			}
			var input struct {
				Markdown string `json:"markdown"`
			}
			if !decodeJSON(r, &input) {
				writeError(w, 400, "invalid_request")
				return
			}
			result, e = service.SaveTaskPackage(r.Context(), actor, input.Markdown, key)
		default:
			http.NotFound(w, r)
			return
		}
		if errors.Is(e, app.ErrSidebarForbidden) {
			writeError(w, 403, "permission_denied")
			return
		}
		(&Handler{}).respond(w, result, e)
	})
}

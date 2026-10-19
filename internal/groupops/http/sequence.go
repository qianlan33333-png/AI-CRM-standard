package http

import (
	"encoding/hex"
	"errors"
	access "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	app "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/app"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	"net/http"
	"strconv"
	"strings"
)

const SequenceConfigPath = "/api/admin/automation-conversion/group-ops/rpa-config"
const SequenceStartPath = "/api/admin/automation-conversion/group-ops/rpa-start"

type sequenceApplication interface {
	p.Sequences
	p.Navigation
}

func NewSequenceConfigHandler(service sequenceApplication, security RequestSecurity, employees SidebarEmployeeReader) http.Handler {
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
		if r.Method == "GET" && r.URL.Path == SequenceStartPath {
			result, e = service.ReadNavigation(r.Context(), actor)
		} else if r.Method == "GET" && r.URL.Path == SequenceConfigPath {
			id, err := strconv.ParseInt(r.URL.Query().Get("plan_id"), 10, 64)
			if err != nil || id < 1 {
				writeError(w, 400, "invalid_request")
				return
			}
			result, e = service.ReadSequenceConfig(r.Context(), actor, id)
		} else if r.Method == "POST" && (r.URL.Path == SequenceConfigPath || r.URL.Path == SequenceStartPath) {
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
			if r.URL.Path == SequenceConfigPath {
				var c p.SequenceConfig
				if !decodeJSON(r, &c) {
					writeError(w, 400, "invalid_request")
					return
				}
				c.Key = key
				result, e = service.SaveSequenceConfig(r.Context(), actor, c)
			} else {
				var c struct {
					PlanID   int64 `json:"plan_id,string"`
					Revision int64 `json:"revision"`
				}
				if !decodeJSON(r, &c) {
					writeError(w, 400, "invalid_request")
					return
				}
				result, e = service.MutateNavigation(r.Context(), actor, p.NavigationCommand{Action: "start", PlanID: c.PlanID, PlanRevision: c.Revision, Key: key, Mode: "rpa"})
			}
		} else {
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
func NewNativeSequenceHandler(service p.Sequences, auth access.RPAAuthenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		tokens := strings.Fields(r.Header.Get("Authorization"))
		if len(tokens) != 2 || tokens[0] != "Bearer" || len(r.Header.Values("Authorization")) != 1 || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" {
			writeError(w, 401, "authentication_required")
			return
		}
		principal, e := auth.AuthenticateRPA(r.Context(), tokens[1])
		if e != nil {
			writeError(w, 401, "authentication_required")
			return
		}
		actor := p.SidebarActor{StaffID: principal.StaffID, WeComUserID: principal.SenderUserID, DisplayName: principal.DisplayName}
		var result any
		if r.URL.Path == "/api/rpa/v1/state" && r.Method == "GET" {
			result, e = service.ReadSequenceStep(r.Context(), actor, principal.DeviceID)
		} else if r.Method == "POST" {
			action := strings.TrimPrefix(r.URL.Path, "/api/rpa/v1/")
			if action != "prepare" && action != "complete" && action != "pause" && action != "resume" && action != "verify" && action != "reconcile" {
				http.NotFound(w, r)
				return
			}
			var body struct {
				RunID        int64  `json:"run_id,string"`
				Revision     int64  `json:"revision"`
				GroupIndex   int    `json:"group_index"`
				MessageIndex int    `json:"message_index"`
				Grant        string `json:"grant"`
				Outcome      string `json:"outcome"`
				Evidence     string `json:"evidence_digest"`
			}
			if !decodeJSON(r, &body) {
				writeError(w, 400, "invalid_request")
				return
			}
			c := p.SequenceCommand{Action: action, RunID: body.RunID, Revision: body.Revision, GroupIndex: body.GroupIndex, MessageIndex: body.MessageIndex, DeviceID: principal.DeviceID, Grant: body.Grant, Outcome: body.Outcome}
			if action == "complete" || action == "reconcile" {
				raw, err := hex.DecodeString(body.Evidence)
				if err != nil || len(raw) != 32 {
					writeError(w, 400, "invalid_request")
					return
				}
				copy(c.EvidenceDigest[:], raw)
			}
			result, e = service.MutateSequence(r.Context(), actor, c)
		} else {
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

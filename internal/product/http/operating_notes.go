package http

import (
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"net/http"
	"strconv"
	"strings"
)

func (h *Handler) operatingNotes(w http.ResponseWriter, r *http.Request) {
	service, ok := h.catalog.(productport.OperatingNotesService)
	if !ok {
		writeError(w, 503, "unavailable")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		writeError(w, 400, "invalid_request")
		return
	}
	id, e := strconv.ParseInt(parts[len(parts)-2], 10, 64)
	if e != nil || id < 1 {
		writeError(w, 400, "invalid_request")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !h.read(w, r) {
			return
		}
		v, e := service.ReadOperatingNotes(r.Context(), productport.ID(id))
		if e != nil {
			resultError(w, e)
			return
		}
		writeJSON(w, 200, v)
	case http.MethodPut:
		p, ok := h.write(w, r)
		if !ok {
			return
		}
		var body struct {
			Notes           string `json:"notes"`
			ExpectedVersion int64  `json:"expected_version"`
		}
		if decodeJSON(r, &body) != nil {
			writeError(w, 400, "invalid_request")
			return
		}
		v, e := service.SaveOperatingNotes(r.Context(), productport.SaveOperatingNotes{ProductID: productport.ID(id), Notes: body.Notes, ExpectedVersion: body.ExpectedVersion, Actor: p.InternalID, IdempotencyKey: r.Header.Get("Idempotency-Key")})
		if e != nil {
			resultError(w, e)
			return
		}
		writeJSON(w, 200, v)
	default:
		writeError(w, 405, "method_not_allowed")
	}
}

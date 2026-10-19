package http

import (
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"net/http"
	"strings"
)

func (h *Handler) salesSources(w http.ResponseWriter, r *http.Request, parts []string, kind productport.ProductOptionType) {
	app, ok := h.catalog.(productport.SalesSourceApplication)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if len(parts) != 2 && len(parts) != 3 {
		writeError(w, 404, "not_found")
		return
	}
	id, e := parseID(parts[0])
	if e != nil {
		writeError(w, 404, "not_found")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request")
		return
	}
	if len(parts) == 2 && r.Method == http.MethodGet {
		if !h.read(w, r) {
			return
		}
		out, e := app.ListSalesSources(r.Context(), productport.ID(id), kind)
		if e != nil {
			resultError(w, e)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	if !(len(parts) == 2 && r.Method == http.MethodPost || len(parts) == 3 && r.Method == http.MethodPut) {
		methodNotAllowed(w, "GET, POST, PUT")
		return
	}
	principal, ok := h.write(w, r)
	if !ok {
		return
	}
	var body struct {
		Name            string `json:"name"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if decodeJSON(r, &body) != nil {
		writeError(w, 400, "invalid_request")
		return
	}
	key, e := requestIdempotencyKey(r)
	if e != nil {
		writeError(w, 400, "invalid_request")
		return
	}
	c := productport.SalesSourceCommand{ProductID: productport.ID(id), ProductType: kind, Name: strings.TrimSpace(body.Name), ExpectedVersion: body.ExpectedVersion, Actor: principal.InternalID, IdempotencyKey: key}
	if len(parts) == 3 {
		c.SourceID, e = parseID(parts[2])
		if e != nil {
			writeError(w, 404, "not_found")
			return
		}
	}
	out, e := app.SaveSalesSource(r.Context(), c)
	if e != nil {
		resultError(w, e)
		return
	}
	status := 200
	if c.SourceID == 0 {
		status = 201
	}
	writeJSON(w, status, out)
}

package http

import (
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func withoutSalesSource(r *http.Request) *http.Request {
	parts := strings.Split(r.URL.RawQuery, "&")
	kept := make([]string, 0, len(parts))
	removed := false
	for _, p := range parts {
		if p == "sales_source" || strings.HasPrefix(p, "sales_source=") {
			removed = true
			continue
		}
		kept = append(kept, p)
	}
	if !removed {
		return r
	}
	clone := r.Clone(r.Context())
	u := *r.URL
	u.RawQuery = strings.Join(kept, "&")
	clone.URL = &u
	return clone
}
func publicSalesSource(r *http.Request) productport.SalesSourceSelection {
	values, present := r.URL.Query()["sales_source"]
	if !present {
		return productport.SalesSourceSelection{Kind: "standard"}
	}
	out := productport.SalesSourceSelection{Kind: "unrecorded"}
	if len(values) != 1 {
		return out
	}
	id, e := strconv.ParseInt(values[0], 10, 64)
	if e == nil && id > 0 && id <= 9007199254740991 && strconv.FormatInt(id, 10) == values[0] {
		out.Kind = "channel"
		out.ID = id
	}
	return out
}
func salesSourcePath(path string, r *http.Request) string {
	if _, present := r.URL.Query()["sales_source"]; !present {
		return path
	}
	v := publicSalesSource(r)
	key := "unrecorded"
	if v.Kind == "channel" {
		key = strconv.FormatInt(v.ID, 10)
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "sales_source=" + url.QueryEscape(key)
}

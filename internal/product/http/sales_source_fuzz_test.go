package http

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func FuzzPublicSalesSourceKeepsOriginalPaymentContext(f *testing.F) {
	for _, value := range []string{"8", "0", "01", "-1", "9007199254740991", "9007199254740992", "unrecorded", "8&next=//outside.invalid", "%38", "\x00", "渠道"} {
		f.Add(value, true)
	}
	f.Fuzz(func(t *testing.T, value string, resume bool) {
		query := "sales_source=" + url.QueryEscape(value)
		promotion := "dpc_" + strings.Repeat("A", 43)
		if resume {
			query += "&resume_order=7"
		} else {
			query += "&promotion_context=" + promotion
		}
		r := &http.Request{URL: &url.URL{Path: "/pay/course", RawQuery: query}}
		got, id, ok := publicPaymentContext(r, true)
		if !ok || resume && (id != 7 || got != "") || !resume && (id != 0 || got != promotion) {
			t.Fatal("attribution changed original payment context")
		}
		selection := publicSalesSource(r)
		parsed, err := strconv.ParseInt(value, 10, 64)
		canonical := err == nil && parsed > 0 && parsed <= 9007199254740991 && strconv.FormatInt(parsed, 10) == value
		if canonical {
			if selection.Kind != "channel" || selection.ID != parsed {
				t.Fatal("canonical attribution lost")
			}
		} else if selection.Kind != "unrecorded" || selection.ID != 0 {
			t.Fatal("malformed attribution accepted")
		}
		if _, _, valid := publicPaymentContext(&http.Request{URL: &url.URL{Path: "/pay/course", RawQuery: query + "&next=%2Fadmin"}}, true); valid {
			t.Fatal("attribution opened an unknown return parameter")
		}
	})
}

package h5oauth

import (
	"net/url"
	"strings"
	"testing"
)

func FuzzSalesSourceOAuthReturnsRemainSameOrigin(f *testing.F) {
	for _, path := range []string{"/p/course?sales_source=8", "/pay/course?resume_order=7&sales_source=8", "/s/course/pay?sales_source=unrecorded", "//outside.invalid/p/course?sales_source=8", "/p/course?sales_source=8&next=/admin", "/p/course?sales_source=8&sales_source=9", "/p/course?sales_source=%38", "/p/course%2Fadmin?sales_source=8"} {
		f.Add(path)
	}
	f.Fuzz(func(t *testing.T, path string) {
		if !ValidReturnPath(path) {
			return
		}
		u, err := url.Parse(path)
		if err != nil {
			t.Fatal("OAuth accepted an invalid HTTP return URL")
		}
		if u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
			t.Fatal("OAuth accepted a cross-origin return")
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			t.Fatal("OAuth accepted malformed return query")
		}
		if sources, ok := query["sales_source"]; ok {
			if len(sources) != 1 {
				t.Fatal("OAuth accepted ambiguous attribution")
			}
			if !(strings.HasPrefix(u.Path, "/p/") || strings.HasPrefix(u.Path, "/pay/") || strings.HasPrefix(u.Path, "/s/")) {
				t.Fatal("attribution escaped product routes")
			}
			for key, values := range query {
				if len(values) != 1 || key != "sales_source" && key != "promotion_context" && key != "resume_order" {
					t.Fatal("attribution widened return contract")
				}
			}
		}
	})
}

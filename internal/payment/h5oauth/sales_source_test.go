package h5oauth

import (
	"strings"
	"testing"
)

func TestSalesSourceReturnPathRemainsClosed(t *testing.T) {
	promotion := "dpc_" + strings.Repeat("A", 43)
	for _, p := range []string{"/p/course?sales_source=8", "/pay/course?promotion_context=" + promotion + "&sales_source=8", "/s/course?sales_source=unrecorded", "/s/course/pay?sales_source=8", "/pay/course?resume_order=7&sales_source=8"} {
		if !ValidReturnPath(p) {
			t.Fatal("source lost", p)
		}
	}
	for _, p := range []string{"/distribution?sales_source=8", "//evil.test/p/course?sales_source=8", "/p/course?sales_source=8&next=/admin", "/p/course?sales_source=8&sales_source=9", "/p/course?sales_source=%38", "/p/course?sales_source=0", "/p/course?sales_source=8#fragment"} {
		if ValidReturnPath(p) {
			t.Fatal("unsafe source return", p)
		}
	}
	for _, control := range []byte{0, 1, 9, 10, 13, 16, 31, 127} {
		for _, suffix := range []string{"", "?sales_source=8"} {
			if ValidReturnPath("/p/course" + string(control) + suffix) {
				t.Fatal("invalid HTTP control character accepted on OAuth return")
			}
		}
	}
}

package http

import (
	"bytes"
	"fmt"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"html/template"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSalesSourcePublicContextPreservesPromotionAndResume(t *testing.T) {
	promotion := "dpc_" + strings.Repeat("A", 43)
	for _, raw := range []string{"sales_source=8", "sales_source=bogus", "sales_source=8&sales_source=9"} {
		r := httptest.NewRequest("GET", "/pay/course?"+raw+"&promotion_context="+promotion, nil)
		got, resume, ok := publicPaymentContext(r, true)
		if !ok || got != promotion || resume != 0 {
			t.Fatal("source broke promotion", raw, got, resume, ok)
		}
	}
	r := httptest.NewRequest("GET", "/pay/course?resume_order=7&sales_source=8", nil)
	_, id, ok := publicPaymentContext(r, true)
	if !ok || id != 7 {
		t.Fatal("source replaced original order")
	}
	for _, raw := range []string{"sales_source=8&next=/admin", "sales_source=8&promotion_context=%64pc_" + strings.Repeat("A", 43)} {
		_, _, ok := publicPaymentContext(httptest.NewRequest("GET", "/pay/course?"+raw, nil), true)
		if ok {
			t.Fatal("source weakened closed URL", raw)
		}
	}
	for _, raw := range []string{"8&sales_source=9", "9007199254740992", "bogus", "-1", "01"} {
		if s := publicSalesSource(httptest.NewRequest("GET", "/p/course?sales_source="+raw, nil)); s.Kind != "unrecorded" {
			t.Fatal(raw, s)
		}
	}
}
func TestSalesSourceCheckoutTemplateKeepsFrozenInput(t *testing.T) {
	var out bytes.Buffer
	view := publicProductPageView{Payment: true, WeChatPayEnabled: true, Product: publicProduct{ID: 7, Name: "商品", ProductKind: "standard", RegionOptionsJSON: template.JS("[]")}}
	if e := publicProductPage.Execute(&out, view); e != nil {
		t.Fatal(e)
	}
	html := out.String()
	for _, part := range []string{"normalized.sales_source=", "sales_source:currentSalesSource", "value.sales_source.kind", "aicrm.checkout.tab.v2:"} {
		if !strings.Contains(html, part) {
			t.Fatal("missing checkout behavior", part)
		}
	}
	if strings.Contains(html, "localStorage") {
		t.Fatal("source must not use cross-tab latest attribution")
	}
}

func TestPublicSalesSourceBrowserJourney(t *testing.T) {
	preparePublicBrowserDependencies(t)
	dir := t.TempDir()
	for _, kind := range []string{"standard", "service_period"} {
		for _, pair := range []struct {
			name string
			id   int64
		}{{"a", 11}, {"b", 22}} {
			path := "/pay/course"
			if kind == "service_period" {
				path = "/s/course/pay"
			}
			view := publicProductPageView{Payment: true, WeChatPayEnabled: true, Product: publicProduct{ID: 7, Name: "渠道测试商品", PriceMinor: 8800, ProductKind: kind, ContactCollectionLevel: "none", RegionOptionsJSON: template.JS("[]"), SalesSource: productport.SalesSourceSelection{Kind: "channel", ID: pair.id}, OAuthReturnPath: path + "?sales_source=" + fmt.Sprint(pair.id)}}
			var out bytes.Buffer
			if e := publicProductPage.Execute(&out, view); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(dir, kind+"-"+pair.name+".html"), out.Bytes(), 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	out, e := exec.Command("node", "public_sales_sources_journey.mjs", dir).CombinedOutput()
	if e != nil {
		t.Fatalf("%v\n%s", e, out)
	}
	t.Log(string(out))
}

package main

import (
	"strings"
	"testing"

	testconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config/tests"
)

func TestPostgreSQLSalesSourceH5OAuthStartAndCompleteRoundTrip(t *testing.T) {
	promotion := "dpc_" + strings.Repeat("A", 43)
	for _, path := range []string{
		"/p/31314141?sales_source=1",
		"/pay/31314141?sales_source=1",
		"/s/period?sales_source=2",
		"/s/period/pay?sales_source=unrecorded",
		"/pay/31314141?promotion_context=" + promotion + "&sales_source=1",
		"/pay/31314141?resume_order=7&sales_source=1",
	} {
		t.Run(path, func(t *testing.T) { testH5OAuthStartAndCompleteRoundTrip(t, path) })
	}
}

func TestSalesSourceH5OAuthInstalled(t *testing.T) {
	if !testconfig.InstalledProductContractsRequested() {
		t.Skip("explicit installed Product package required")
	}
	if testconfig.InstalledProduct().Binary == "" {
		t.Fatal("installed Product package inputs are incomplete")
	}
	TestPostgreSQLSalesSourceH5OAuthStartAndCompleteRoundTrip(t)
}

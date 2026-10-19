package main

import (
	"context"
	"errors"
	"testing"
	"time"

	customerdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	customerport "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/port"
	hxcport "github.com/qianlan33333-png/AI-CRM-standard/internal/hxcdashboard/port"
	identitydomain "github.com/qianlan33333-png/AI-CRM-standard/internal/identity/domain"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/sidebar"
)

type profileIdentityFixture struct {
	value, scope string
	found        bool
	reads        int
	changed      bool
	root         customerdomain.CustomerID
}

func (f *profileIdentityFixture) CanonicalLineage(context.Context, customerdomain.CustomerID) ([]customerdomain.CustomerID, error) {
	return []customerdomain.CustomerID{f.root}, nil
}
func (f *profileIdentityFixture) VerifiedExternalIdentityValue(_ context.Context, _ customerdomain.CustomerID, kind identitydomain.Kind, scope string) (string, bool, error) {
	if kind != identitydomain.KindUnionID || scope != f.scope {
		return "", false, errors.New("wrong identity scope")
	}
	f.reads++
	if f.changed && f.reads > 1 {
		return "other-subject", true, nil
	}
	return f.value, f.found, nil
}

type profileCustomerFixture struct{}

func (profileCustomerFixture) ReadSidebarProfile(context.Context, customerdomain.CustomerID) (customerport.SidebarProfile, error) {
	return customerport.SidebarProfile{CustomerID: 42, DisplayName: "企微合成昵称", UpdatedAt: time.Now()}, nil
}
func (profileCustomerFixture) UpdateSidebarProfile(context.Context, customerport.SidebarProfileUpdate) (customerport.SidebarProfile, error) {
	panic("not a write")
}
func (profileCustomerFixture) BindSidebarPhone(context.Context, customerport.SidebarPhoneBind) (customerport.SidebarPhoneResult, error) {
	panic("not a write")
}

type profileSourceFixture struct {
	calls    int
	expected string
	callback func()
}

func (f *profileSourceFixture) ReadCustomerProfile(_ context.Context, union string) (hxcport.CustomerProfile, error) {
	f.calls++
	if union != f.expected {
		return hxcport.CustomerProfile{}, errors.New("wrong subject")
	}
	if f.callback != nil {
		f.callback()
	}
	return hxcport.CustomerProfile{Mapped: true, Version: "source-v1", UpdatedAt: time.Now(), Basic: []hxcport.ProfileField{{Key: "industry", Label: "行业", Value: "教育"}}, Background: []hxcport.ProfileField{{Key: "history", Label: "背景与经历", Value: "完整合成经历"}}}, nil
}
func TestSidebarDatabaseProfileUsesVerifiedScopeAndRechecksIdentity(t *testing.T) {
	identity := &profileIdentityFixture{root: 42, value: "verified-source", found: true, scope: "wechat-open-platform:fixture"}
	source := &profileSourceFixture{expected: identity.value}
	adapter := sidebarDatabaseProfile{uow: timelineUOW{}, identity: identity, profiles: profileCustomerFixture{}, source: source, scope: identity.scope}
	result, err := adapter.ReadAIOperations(context.Background(), sidebar.Principal{CorpID: "corp", EmployeeID: "staff"}, 42)
	if err != nil || source.calls != 1 || identity.reads != 2 || len(result.BackgroundFields) != 1 || len(result.BasicFields) != 1 || result.BasicFields[0].Value != "教育" || result.LastCustomerMessage != nil {
		t.Fatalf("scoped read failed: %v", err)
	}
	identity.reads = 0
	identity.changed = true
	if _, err = adapter.ReadAIOperations(context.Background(), sidebar.Principal{CorpID: "corp", EmployeeID: "staff"}, 42); err == nil {
		t.Fatal("identity change during source read leaked prior profile")
	}
}
func TestSidebarDatabaseProfileMissingIdentityNeverGuessesAndMergedContextStops(t *testing.T) {
	identity := &profileIdentityFixture{root: 42, scope: "wechat-open-platform:fixture"}
	source := &profileSourceFixture{}
	adapter := sidebarDatabaseProfile{uow: timelineUOW{}, identity: identity, profiles: profileCustomerFixture{}, source: source, scope: identity.scope}
	if result, err := adapter.ReadAIOperations(context.Background(), sidebar.Principal{CorpID: "corp", EmployeeID: "staff"}, 42); err != nil || source.calls != 0 || result.ProfileMode != "fields" || len(result.BasicFields) != 0 {
		t.Fatal("missing identity caused a guessed source read")
	}
	identity.root = 43
	if _, err := adapter.ReadAIOperations(context.Background(), sidebar.Principal{CorpID: "corp", EmployeeID: "staff"}, 42); err == nil || source.calls != 0 {
		t.Fatal("retired customer context read a different root")
	}
}

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	customerdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	customerport "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/port"
	hxcport "github.com/qianlan33333-png/AI-CRM-standard/internal/hxcdashboard/port"
	identitydomain "github.com/qianlan33333-png/AI-CRM-standard/internal/identity/domain"
	identityport "github.com/qianlan33333-png/AI-CRM-standard/internal/identity/port"
	platformport "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/port"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/sidebar"
)

type sidebarProfileIdentity interface {
	identityport.ExternalIdentityValueReader
	identityport.CanonicalLineageReader
}

type sidebarDatabaseProfile struct {
	uow      platformport.UnitOfWork
	identity sidebarProfileIdentity
	profiles customerport.SidebarProfileService
	source   hxcport.CustomerProfileReader
	scope    string
}

func (adapter sidebarDatabaseProfile) subject(ctx context.Context, id customerdomain.CustomerID) (string, bool, error) {
	var value string
	var found bool
	err := adapter.uow.Within(ctx, func(tx context.Context) error {
		lineage, err := adapter.identity.CanonicalLineage(tx, id)
		if err != nil {
			return err
		}
		if len(lineage) == 0 || lineage[0] != id {
			return errors.New("sidebar profile identity changed")
		}
		value, found, err = adapter.identity.VerifiedExternalIdentityValue(tx, id, identitydomain.KindUnionID, adapter.scope)
		return err
	})
	return value, found, err
}

func (adapter sidebarDatabaseProfile) ReadAIOperations(ctx context.Context, p sidebar.Principal, id customerdomain.CustomerID) (sidebar.AIWarehouseSnapshot, error) {
	if id < 1 || p.CorpID == "" || p.EmployeeID == "" || adapter.source == nil || adapter.scope == "" {
		return sidebar.AIWarehouseSnapshot{}, errors.New("sidebar database profile unavailable")
	}
	union, found, err := adapter.subject(ctx, id)
	if err != nil {
		return sidebar.AIWarehouseSnapshot{}, err
	}
	profile, err := adapter.profiles.ReadSidebarProfile(ctx, id)
	if err != nil {
		return sidebar.AIWarehouseSnapshot{}, err
	}
	if profile.CustomerID != id {
		return sidebar.AIWarehouseSnapshot{}, errors.New("sidebar profile identity changed")
	}
	var remote hxcport.CustomerProfile
	if found {
		remote, err = adapter.source.ReadCustomerProfile(ctx, union)
		if err != nil {
			return sidebar.AIWarehouseSnapshot{}, err
		}
	}
	current, currentFound, err := adapter.subject(ctx, id)
	if err != nil {
		return sidebar.AIWarehouseSnapshot{}, err
	}
	if currentFound != found || current != union {
		return sidebar.AIWarehouseSnapshot{}, errors.New("sidebar profile identity changed")
	}
	fields := []sidebar.AIProfileField{}
	if profile.CorpName != "" {
		fields = append(fields, sidebar.AIProfileField{Key: "company", Label: "公司／机构", Value: profile.CorpName})
	}
	for _, field := range remote.Basic {
		fields = append(fields, sidebar.AIProfileField{Key: field.Key, Label: field.Label, Value: field.Value})
	}
	background := []sidebar.AIProfileField{}
	for _, field := range remote.Background {
		background = append(background, sidebar.AIProfileField{Key: field.Key, Label: field.Label, Value: field.Value})
	}
	diagnosis := []sidebar.AIProfileField{}
	for _, field := range remote.Diagnosis {
		diagnosis = append(diagnosis, sidebar.AIProfileField{Key: field.Key, Label: field.Label, Value: field.Value})
	}
	at := remote.UpdatedAt
	if at.IsZero() {
		at = profile.UpdatedAt
	}
	result := sidebar.AIWarehouseSnapshot{CustomerID: id, CorpID: p.CorpID, EmployeeID: p.EmployeeID, ProfileMode: "fields", BasicFields: fields, BackgroundFields: background, DiagnosisFields: diagnosis, SummaryUpdatedAt: &at, SyncedAt: &at}
	binding, _ := json.Marshal(struct {
		Profile                      customerport.SidebarProfile
		RemoteVersion                string
		Basic, Background, Diagnosis []sidebar.AIProfileField
	}{profile, remote.Version, fields, background, diagnosis})
	digest := sha256.Sum256(binding)
	result.SourceVersion = "sidebar-database-profile.v1:" + hex.EncodeToString(digest[:])
	return result, nil
}

var _ sidebar.AIWarehouseReader = sidebarDatabaseProfile{}

package app

import (
	"context"
	"errors"
	"strings"

	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

var ErrSidebarForbidden = errors.New("group sidebar access denied")

type SidebarBindingService struct {
	Plans   *Service
	Catalog p.Catalog
	Reader  p.SidebarPlanReader
}

func (s *SidebarBindingService) ownedGroup(ctx context.Context, actor p.SidebarActor, chat string) (p.CatalogGroup, error) {
	if s == nil || s.Plans == nil || s.Catalog == nil || s.Reader == nil || s.Plans.groupOwners == nil {
		return p.CatalogGroup{}, ErrUnavailable
	}
	if actor.StaffID < 1 || actor.WeComUserID == "" || !opaque(chat) {
		return p.CatalogGroup{}, ErrInvalid
	}
	// Refresh uses the existing catalog: Provider I/O precedes its local
	// transaction. Its timestamp and owner projection also feed AddGroupAsset.
	g, err := s.Catalog.RefreshCatalogGroup(ctx, chat)
	if err != nil {
		return p.CatalogGroup{}, ErrUnavailable
	}
	if g.ChatID != chat || g.State != "ready" || g.ObservedAt == nil {
		return p.CatalogGroup{}, ErrUnavailable
	}
	if g.OwnerUserID != actor.WeComUserID || g.OwnerStaffID != actor.StaffID {
		return p.CatalogGroup{}, ErrSidebarForbidden
	}
	return g, nil
}

func (s *SidebarBindingService) SidebarContext(ctx context.Context, actor p.SidebarActor, chat, query string, limit, offset int32) (p.SidebarContext, error) {
	if !validPage(limit, offset) {
		return p.SidebarContext{}, ErrInvalid
	}
	g, err := s.ownedGroup(ctx, actor, chat)
	if err != nil {
		return p.SidebarContext{}, err
	}
	var plans p.SidebarPlans
	err = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		var e error
		plans, e = s.Reader.ReadSidebarPlans(tx, actor.StaffID, chat, strings.TrimSpace(query), limit, offset)
		return e
	})
	if err != nil {
		return p.SidebarContext{}, ErrUnavailable
	}
	return p.SidebarContext{ChatID: chat, Name: g.Name, OwnerName: actor.DisplayName, CheckedAt: *g.ObservedAt, SidebarPlans: plans, ProviderReadExecuted: true}, nil
}

func (s *SidebarBindingService) BindSidebarGroup(ctx context.Context, actor p.SidebarActor, c p.SidebarBindCommand) (p.SidebarBindResult, error) {
	if c.PlanID < 1 || c.ExpectedRevision < 1 || !validKey(c.IdempotencyKey) {
		return p.SidebarBindResult{}, ErrInvalid
	}
	if _, err := s.ownedGroup(ctx, actor, c.ChatID); err != nil {
		return p.SidebarBindResult{}, err
	}
	detail, err := s.Plans.Detail(ctx, c.PlanID)
	if err != nil {
		return p.SidebarBindResult{}, err
	}
	if len(detail.Members) != 1 || detail.Members[0].StaffID != actor.StaffID {
		return p.SidebarBindResult{}, ErrSidebarForbidden
	}
	if detail.Plan.Status == p.PlanArchived {
		return p.SidebarBindResult{}, ErrNotFound
	}
	// Reconciliation after a lost response is a read, including for a plan
	// subsequently activated. It never changes the plan's status or revision.
	for _, asset := range detail.GroupAssets {
		if asset.AssetRef == c.ChatID {
			return sidebarBindResult("already_bound", detail), nil
		}
	}
	// The existing command rechecks live local owner, sole plan owner, active
	// employee, lifecycle and CAS under its original lock/receipt/UoW.
	detail, err = s.Plans.AddGroupAsset(ctx, p.GroupAssetCommand{PlanID: c.PlanID, ExpectedRevision: c.ExpectedRevision, AssetRef: c.ChatID, Actor: actor.StaffID, IdempotencyKey: c.IdempotencyKey, RequiredOwnerStaffID: actor.StaffID})
	if err != nil {
		return p.SidebarBindResult{}, err
	}
	return sidebarBindResult("bound", detail), nil
}

func sidebarBindResult(state string, d p.Detail) p.SidebarBindResult {
	return p.SidebarBindResult{State: state, Plan: p.SidebarPlan{ID: d.Plan.ID, Name: d.Plan.Name, Status: d.Plan.Status, Revision: d.Plan.Revision}}
}

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

type sidebarCatalog struct {
	p.Catalog
	group p.CatalogGroup
	err   error
	reads int
}

func (s *sidebarCatalog) RefreshCatalogGroup(context.Context, string) (p.CatalogGroup, error) {
	s.reads++
	return s.group, s.err
}

type sidebarPlans struct {
	actor int64
	chat  string
}

func (s *sidebarPlans) ReadSidebarPlans(_ context.Context, actor int64, chat, _ string, limit, offset int32) (p.SidebarPlans, error) {
	s.actor, s.chat = actor, chat
	return p.SidebarPlans{Bindings: []p.SidebarPlan{}, Candidates: []p.SidebarPlan{}, Limit: limit, Offset: offset}, nil
}
func sidebarFixture(t *testing.T) (*SidebarBindingService, *testStore, *sidebarCatalog, p.Detail) {
	t.Helper()
	s, store, _ := newTestService()
	s.groupOwners = bindingOwners{"owned": 19, "other": 20}
	d, err := s.Create(context.Background(), p.CreatePlanCommand{Name: "Sidebar plan", Actor: 19, IdempotencyKey: "sidebar-plan-create"})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.AddMember(context.Background(), p.MemberCommand{PlanID: d.Plan.ID, ExpectedRevision: d.Plan.Revision, StaffID: 19, Actor: 19, IdempotencyKey: "sidebar-plan-member"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	catalog := &sidebarCatalog{group: p.CatalogGroup{ChatID: "owned", OwnerStaffID: 19, OwnerUserID: "owner", State: "ready", ObservedAt: &now}}
	return &SidebarBindingService{Plans: s, Catalog: catalog, Reader: &sidebarPlans{}}, store, catalog, d
}

func TestSidebarBindingContextRequiresLiveMatchingOwner(t *testing.T) {
	s, _, catalog, _ := sidebarFixture(t)
	actor := p.SidebarActor{StaffID: 19, WeComUserID: "owner", DisplayName: "群主"}
	if out, err := s.SidebarContext(context.Background(), actor, "owned", "", 50, 0); err != nil || !out.ProviderReadExecuted || out.OwnerName != "群主" || out.Name != "" {
		t.Fatalf("context=%+v err=%v", out, err)
	}
	catalog.group.OwnerUserID = "someone-else"
	if _, err := s.SidebarContext(context.Background(), actor, "owned", "", 50, 0); !errors.Is(err, ErrSidebarForbidden) {
		t.Fatalf("wrong owner: %v", err)
	}
	catalog.group.OwnerUserID = "owner"
	catalog.group.OwnerStaffID = 20
	if _, err := s.SidebarContext(context.Background(), actor, "owned", "", 50, 0); !errors.Is(err, ErrSidebarForbidden) {
		t.Fatalf("wrong local owner: %v", err)
	}
	catalog.err = errors.New("provider unavailable")
	if _, err := s.SidebarContext(context.Background(), actor, "owned", "", 50, 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("provider failure: %v", err)
	}
}

func TestSidebarBindingAddsOnlyOneGroupAndReadsBackWithoutActivating(t *testing.T) {
	s, store, _, d := sidebarFixture(t)
	ctx := context.Background()
	actor := p.SidebarActor{StaffID: 19, WeComUserID: "owner"}
	c := p.SidebarBindCommand{ChatID: "owned", PlanID: d.Plan.ID, ExpectedRevision: d.Plan.Revision, IdempotencyKey: "sidebar-add-group"}
	result, err := s.BindSidebarGroup(ctx, actor, c)
	if err != nil || result.State != "bound" || result.Plan.Status != p.PlanDraft {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved := store.details[d.Plan.ID]
	if len(saved.GroupAssets) != 1 || saved.GroupAssets[0].AssetRef != "owned" {
		t.Fatal("binding not saved")
	}
	result, err = s.BindSidebarGroup(ctx, actor, c)
	if err != nil || result.State != "already_bound" || result.Plan.Revision != saved.Plan.Revision {
		t.Fatalf("duplicate=%+v err=%v", result, err)
	}
	// Reconciliation works after activation, without invoking activation itself.
	saved.Plan.Status = p.PlanActive
	store.details[d.Plan.ID] = saved
	result, err = s.BindSidebarGroup(ctx, actor, c)
	if err != nil || result.State != "already_bound" || result.Plan.Status != p.PlanActive {
		t.Fatalf("readback=%+v err=%v", result, err)
	}
}

func TestSidebarBindingRejectsForeignPlanActiveAndStaleRevision(t *testing.T) {
	s, store, _, d := sidebarFixture(t)
	ctx := context.Background()
	actor := p.SidebarActor{StaffID: 19, WeComUserID: "owner"}
	c := p.SidebarBindCommand{ChatID: "owned", PlanID: d.Plan.ID, ExpectedRevision: d.Plan.Revision - 1, IdempotencyKey: "sidebar-stale-add"}
	if _, err := s.BindSidebarGroup(ctx, actor, c); !errors.Is(err, ErrConflict) {
		t.Fatalf("CAS: %v", err)
	}
	c.ExpectedRevision = d.Plan.Revision
	c.IdempotencyKey = "sidebar-active-add"
	d.Plan.Status = p.PlanActive
	store.details[d.Plan.ID] = d
	if _, err := s.BindSidebarGroup(ctx, actor, c); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("active: %v", err)
	}
	d.Plan.Status = p.PlanPaused
	c.IdempotencyKey = "sidebar-foreign-add"
	d.Members = []p.Member{{StaffID: 20}}
	store.details[d.Plan.ID] = d
	if _, err := s.BindSidebarGroup(ctx, actor, c); !errors.Is(err, ErrSidebarForbidden) {
		t.Fatalf("foreign plan: %v", err)
	}
	if len(store.details[d.Plan.ID].GroupAssets) != 0 {
		t.Fatal("rejection wrote binding")
	}
}

type sidebarRacingStore struct {
	Store
	owner int64
}

func (s sidebarRacingStore) Lock(ctx context.Context, id int64) (p.Detail, error) {
	d, e := s.Store.Lock(ctx, id)
	d.Members = []p.Member{{StaffID: s.owner}}
	return d, e
}
func TestSidebarBindingRechecksActorUnderPlanLock(t *testing.T) {
	s, store, _, d := sidebarFixture(t)
	s.Plans.store = sidebarRacingStore{Store: store, owner: 20}
	s.Plans.groupOwners = bindingOwners{"owned": 20}
	_, err := s.BindSidebarGroup(context.Background(), p.SidebarActor{StaffID: 19, WeComUserID: "owner"}, p.SidebarBindCommand{ChatID: "owned", PlanID: d.Plan.ID, ExpectedRevision: d.Plan.Revision, IdempotencyKey: "sidebar-owner-race"})
	if !errors.Is(err, ErrSidebarForbidden) || len(store.details[d.Plan.ID].GroupAssets) != 0 {
		t.Fatalf("owner race: %v", err)
	}
}

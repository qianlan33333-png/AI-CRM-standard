package app

import (
	"context"
	"errors"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	"testing"
	"time"
)

type navigationMemory struct {
	runs map[int64]p.NavigationRun
	keys map[[32]byte]int64
}

func navCopy(r p.NavigationRun) p.NavigationRun {
	r.Targets = append([]p.NavigationTarget{}, r.Targets...)
	r.Observations = append([]p.NavigationObservation{}, r.Observations...)
	return r
}
func (n *navigationMemory) NavigationPlans(context.Context, int64) ([]p.SidebarPlan, error) {
	return []p.SidebarPlan{}, nil
}
func (n *navigationMemory) LatestNavigation(_ context.Context, staff int64) (*p.NavigationRun, error) {
	var out *p.NavigationRun
	for _, r := range n.runs {
		if r.StaffID == staff && (out == nil || out.ID < r.ID) {
			c := navCopy(r)
			out = &c
		}
	}
	return out, nil
}
func (n *navigationMemory) NavigationByKey(_ context.Context, staff int64, k [32]byte) (*p.NavigationRun, error) {
	if id, ok := n.keys[k]; ok {
		r := navCopy(n.runs[id])
		if r.StaffID == staff {
			return &r, nil
		}
	}
	return nil, nil
}
func (n *navigationMemory) LockNavigation(_ context.Context, id int64) (p.NavigationRun, error) {
	r, ok := n.runs[id]
	if !ok {
		return r, ErrNotFound
	}
	return navCopy(r), nil
}
func (n *navigationMemory) CreateNavigation(_ context.Context, r p.NavigationRun, k [32]byte) (p.NavigationRun, error) {
	for _, v := range n.runs {
		if v.StaffID == r.StaffID && v.State != "completed" && v.State != "cancelled" {
			return p.NavigationRun{}, ErrConflict
		}
	}
	r.ID = int64(len(n.runs) + 1)
	n.runs[r.ID] = navCopy(r)
	n.keys[k] = r.ID
	return r, nil
}
func (n *navigationMemory) SaveNavigation(_ context.Context, r p.NavigationRun, old int64) error {
	if n.runs[r.ID].Revision != old {
		return ErrConflict
	}
	n.runs[r.ID] = navCopy(r)
	return nil
}

type navigationCatalog struct {
	p.Catalog
	groups map[string]p.CatalogGroup
}

func (n navigationCatalog) RefreshCatalogGroup(_ context.Context, id string) (p.CatalogGroup, error) {
	g, ok := n.groups[id]
	if !ok {
		return g, errors.New("no access")
	}
	return g, nil
}
func navigationFixture(t *testing.T) (*NavigationService, *testStore, *navigationMemory, p.SidebarActor, p.NavigationCommand) {
	t.Helper()
	side, store, _, d := sidebarFixture(t)
	d.Plan.Owner = p.PlanOwner{StaffID: 19, SenderUserID: "owner"}
	d.GroupAssets = []p.GroupAsset{{ID: 1, AssetRef: "owned", DisplayName: "同名群"}, {ID: 2, AssetRef: "second", DisplayName: "同名群"}}
	store.details[d.Plan.ID] = d
	n := &navigationMemory{runs: map[int64]p.NavigationRun{}, keys: map[[32]byte]int64{}}
	now := time.Now().UTC()
	cat := navigationCatalog{groups: map[string]p.CatalogGroup{}}
	for _, g := range d.GroupAssets {
		cat.groups[g.AssetRef] = p.CatalogGroup{ChatID: g.AssetRef, OwnerStaffID: 19, OwnerUserID: "owner", State: "ready", ObservedAt: &now}
	}
	return &NavigationService{Plans: side.Plans, Catalog: cat, Store: n}, store, n, p.SidebarActor{StaffID: 19, WeComUserID: "owner"}, p.NavigationCommand{Action: "start", PlanID: d.Plan.ID, PlanRevision: d.Plan.Revision, Key: "navigation-frozen-create"}
}
func TestNavigationFrozenSequentialReadbackPauseAndReplay(t *testing.T) {
	s, plans, _, actor, start := navigationFixture(t)
	ctx := context.Background()
	r, e := s.MutateNavigation(ctx, actor, start)
	if e != nil {
		t.Fatal(e)
	}
	if r.State != "paused" || len(r.Targets) != 2 || r.Targets[0].ChatID == r.Targets[1].ChatID {
		t.Fatalf("start=%+v", r)
	}
	replay, e := s.MutateNavigation(ctx, actor, start)
	if e != nil || replay.ID != r.ID {
		t.Fatalf("replay=%+v %v", replay, e)
	}
	other := start
	other.Key = "second-navigation-key"
	if _, e = s.MutateNavigation(ctx, actor, other); !errors.Is(e, ErrConflict) {
		t.Fatalf("parallel active=%v", e)
	}
	d := plans.details[start.PlanID]
	d.GroupAssets[0].DisplayName = "改名"
	d.GroupAssets = append(d.GroupAssets, p.GroupAsset{ID: 3, AssetRef: "later"})
	d.Plan.Revision++
	plans.details[start.PlanID] = d
	view, e := s.ReadNavigation(ctx, actor)
	if e != nil || len(view.Run.Targets) != 2 || view.Run.Targets[0].Name != "同名群" {
		t.Fatalf("frozen=%+v %v", view, e)
	}
	run := func(action string) {
		t.Helper()
		var err error
		r, err = s.MutateNavigation(ctx, actor, p.NavigationCommand{Action: action, RunID: r.ID, Revision: r.Revision})
		if err != nil {
			t.Fatal(action, err)
		}
	}
	if _, e = s.MutateNavigation(ctx, actor, p.NavigationCommand{Action: "open", RunID: r.ID, Revision: r.Revision}); !errors.Is(e, ErrStateConflict) {
		t.Fatalf("paused open=%v", e)
	}
	run("resume")
	run("open")
	confirm := p.NavigationCommand{Action: "confirm", RunID: r.ID, Revision: r.Revision, Step: 0, ChatID: r.Targets[0].ChatID}
	wrong := confirm
	wrong.ChatID = "second"
	if _, e = s.MutateNavigation(ctx, actor, wrong); !errors.Is(e, ErrStateConflict) {
		t.Fatalf("wrong group=%v", e)
	}
	run("pause")
	if _, e = s.MutateNavigation(ctx, actor, confirm); e == nil {
		t.Fatal("late confirm during pause accepted")
	}
	run("resume")
	confirm.Revision = r.Revision
	r, e = s.MutateNavigation(ctx, actor, confirm)
	if e != nil || r.Cursor != 1 || r.State != "waiting" {
		t.Fatalf("first=%+v %v", r, e)
	}
	r, e = s.MutateNavigation(ctx, actor, confirm)
	if e != nil || r.Cursor != 1 {
		t.Fatalf("confirm replay skipped=%+v %v", r, e)
	}
	// A new service instance resumes only cloud state, independent of the prior page.
	resumed := &NavigationService{Plans: s.Plans, Catalog: s.Catalog, Store: s.Store}
	v, e := resumed.ReadNavigation(ctx, actor)
	if e != nil || v.Run.Cursor != 1 {
		t.Fatal("reload lost cursor", e)
	}
	run("open")
	r, e = s.MutateNavigation(ctx, actor, p.NavigationCommand{Action: "confirm", RunID: r.ID, Revision: r.Revision, Step: 1, ChatID: "second"})
	if e != nil || r.State != "completed" || len(r.Observations) != 2 {
		t.Fatalf("complete=%+v %v", r, e)
	}
}
func TestNavigationRejectsAccountScopeStalePlanAndLostGroup(t *testing.T) {
	s, plans, _, a, c := navigationFixture(t)
	ctx := context.Background()
	bad := a
	bad.WeComUserID = "another-login"
	if _, e := s.MutateNavigation(ctx, bad, c); !errors.Is(e, ErrSidebarForbidden) {
		t.Fatal("foreign account", e)
	}
	stale := c
	stale.PlanRevision++
	if _, e := s.MutateNavigation(ctx, a, stale); !errors.Is(e, ErrConflict) {
		t.Fatal("stale", e)
	}
	r, e := s.MutateNavigation(ctx, a, c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.MutateNavigation(ctx, bad, p.NavigationCommand{Action: "cancel", RunID: r.ID, Revision: r.Revision}); !errors.Is(e, ErrSidebarForbidden) {
		t.Fatal("foreign mutation", e)
	}
	r, e = s.MutateNavigation(ctx, a, p.NavigationCommand{Action: "resume", RunID: r.ID, Revision: r.Revision})
	if e != nil {
		t.Fatal(e)
	}
	cat := s.Catalog.(navigationCatalog)
	delete(cat.groups, "owned")
	if _, e = s.MutateNavigation(ctx, a, p.NavigationCommand{Action: "open", RunID: r.ID, Revision: r.Revision}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("lost access", e)
	}
	d := plans.details[c.PlanID]
	d.Members = []p.Member{{StaffID: 20}}
	plans.details[c.PlanID] = d
	if _, e = s.MutateNavigation(ctx, a, p.NavigationCommand{Action: "confirm", RunID: r.ID, Revision: r.Revision, ChatID: "owned"}); !errors.Is(e, ErrSidebarForbidden) {
		t.Fatal("owner changed", e)
	}
}

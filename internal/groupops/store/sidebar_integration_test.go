package store

import (
	"context"
	"errors"
	"testing"
	"time"

	app "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/app"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

func TestPostgreSQLSidebarPlansScopeSearchPaginationAndBindingPersistence(t *testing.T) {
	pool, uow := groupOpsListIntegrationPool(t)
	ctx := context.Background()
	repo, err := NewPostgreSQL(pool, uow)
	if err != nil {
		t.Fatal(err)
	}
	var owner, other int64
	for i, target := range []*int64{&owner, &other} {
		if err = pool.QueryRow(ctx, `INSERT INTO admin_users(username,password_hash,display_name) VALUES ($1,'$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$ZGlnaWVzdA','测试员工') RETURNING id`, []string{"owner", "other"}[i]).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	ids := []int64{}
	for i, status := range []p.PlanStatus{p.PlanDraft, p.PlanPaused, p.PlanActive, p.PlanArchived, p.PlanDraft} {
		id := groupOpsListPlan(t, ctx, pool, owner, []string{"体验草稿", "体验暂停", "启用绑定", "归档", "别人计划"}[i], status, base.Add(time.Duration(i)*time.Minute))
		ids = append(ids, id)
		staff := owner
		if i == 4 {
			staff = other
		}
		if _, err = pool.Exec(ctx, `INSERT INTO group_ops_plan_members(plan_id,staff_id) VALUES($1,$2)`, id, staff); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO group_ops_plan_group_assets(plan_id,asset_reference) VALUES($1,'owned'),($2,'different')`, ids[2], ids[0]); err != nil {
		t.Fatal(err)
	}
	read := func(q string, limit, offset int32) p.SidebarPlans {
		t.Helper()
		var out p.SidebarPlans
		if err := uow.Within(ctx, func(tx context.Context) error {
			var e error
			out, e = repo.ReadSidebarPlans(tx, owner, "owned", q, limit, offset)
			return e
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := read("体验", 1, 0)
	if first.Total != 2 || len(first.Candidates) != 1 || first.Candidates[0].ID != ids[1] || len(first.Bindings) != 1 || first.Bindings[0].ID != ids[2] {
		t.Fatalf("first=%+v", first)
	}
	second := read("体验", 1, 1)
	if len(second.Candidates) != 1 || second.Candidates[0].ID != ids[0] {
		t.Fatalf("second=%+v", second)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO group_ops_plan_group_assets(plan_id,asset_reference) VALUES($1,'owned')`, ids[0]); err != nil {
		t.Fatal(err)
	}
	after := read("", 50, 0)
	if len(after.Bindings) != 2 || after.Total != 1 {
		t.Fatalf("readback=%+v", after)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM group_ops_plan_group_assets WHERE plan_id=$1`, ids[0]).Scan(&count); err != nil || count != 2 {
		t.Fatalf("existing bindings lost: %d %v", count, err)
	}
	// The minimal list fixture predates this catalog column. Match the live
	// owner query, without creating a scheduler or an external-effects worker.
	if _, err = pool.Exec(ctx, `ALTER TABLE group_ops_directory_groups ADD COLUMN owner_user_id TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO group_ops_directory_groups(chat_reference,owner_staff_id,display_name,member_count,source_digest,refreshed_at) VALUES('fresh-owned',$1,'验收群',3,'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',$2)`, owner, base); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO group_ops_plan_group_assets(plan_id,asset_reference) VALUES($1,'retained-group')`, ids[1]); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(uow, repo, sidebarActiveStaff{}, repo, repo)
	now := time.Now().UTC()
	bindings := &app.SidebarBindingService{Plans: service, Catalog: sidebarIntegrationCatalog{group: p.CatalogGroup{ChatID: "fresh-owned", OwnerUserID: "fixture-owner", OwnerStaffID: owner, State: "ready", ObservedAt: &now}}, Reader: repo}
	command := p.SidebarBindCommand{ChatID: "fresh-owned", PlanID: ids[1], ExpectedRevision: 1, IdempotencyKey: "sidebar-postgres-binding"}
	actor := p.SidebarActor{StaffID: owner, WeComUserID: "fixture-owner"}
	result, err := bindings.BindSidebarGroup(ctx, actor, command)
	if err != nil || result.State != "bound" || result.Plan.Status != p.PlanPaused {
		t.Fatalf("persisted bind=%+v %v", result, err)
	}
	replayed, err := bindings.BindSidebarGroup(ctx, actor, command)
	if err != nil || replayed.State != "already_bound" || replayed.Plan.Revision != result.Plan.Revision {
		t.Fatalf("persisted replay=%+v %v", replayed, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM group_ops_plan_group_assets WHERE plan_id=$1`, ids[1]).Scan(&count); err != nil || count != 2 {
		t.Fatalf("binding preservation=%d %v", count, err)
	}
	// Two windows add different groups against the same plan revision. Exactly
	// one may commit; neither may replace targets already on the plan.
	if _, err = pool.Exec(ctx, `INSERT INTO group_ops_directory_groups(chat_reference,owner_staff_id,display_name,member_count,source_digest,refreshed_at) VALUES('race-a',$1,'并发A',3,'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',$2),('race-b',$1,'并发B',3,'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',$2)`, owner, base); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, chat := range []string{"race-a", "race-b"} {
		go func(chat string) {
			local := &app.SidebarBindingService{Plans: service, Catalog: sidebarIntegrationCatalog{group: p.CatalogGroup{ChatID: chat, OwnerUserID: "fixture-owner", OwnerStaffID: owner, State: "ready", ObservedAt: &now}}, Reader: repo}
			<-start
			_, e := local.BindSidebarGroup(ctx, actor, p.SidebarBindCommand{ChatID: chat, PlanID: ids[0], ExpectedRevision: 1, IdempotencyKey: "sidebar-concurrent-" + chat})
			results <- e
		}(chat)
	}
	close(start)
	passed, conflicted := 0, 0
	for range 2 {
		e := <-results
		if e == nil {
			passed++
		} else if errors.Is(e, app.ErrConflict) {
			conflicted++
		} else {
			t.Fatalf("concurrent binding: %v", e)
		}
	}
	if passed != 1 || conflicted != 1 {
		t.Fatalf("concurrent results success=%d conflict=%d", passed, conflicted)
	}
	var revision int64
	if err = pool.QueryRow(ctx, `SELECT revision FROM group_ops_plans WHERE id=$1`, ids[0]).Scan(&revision); err != nil || revision != 2 {
		t.Fatalf("concurrent revision=%d %v", revision, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM group_ops_plan_group_assets WHERE plan_id=$1`, ids[0]).Scan(&count); err != nil || count != 3 {
		t.Fatalf("concurrent targets overwritten: %d %v", count, err)
	}
	for _, table := range []string{"group_ops_runs", "group_ops_executions", "external_effect_jobs"} {
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("unexpected execution in %s: %d %v", table, count, err)
		}
	}
}

type sidebarActiveStaff struct{}

func (sidebarActiveStaff) IsActiveStaff(context.Context, int64) (bool, error) { return true, nil }

type sidebarIntegrationCatalog struct {
	p.Catalog
	group p.CatalogGroup
}

func (s sidebarIntegrationCatalog) RefreshCatalogGroup(context.Context, string) (p.CatalogGroup, error) {
	return s.group, nil
}

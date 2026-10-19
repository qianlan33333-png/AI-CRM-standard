package store

import (
	"context"
	"crypto/sha256"
	"errors"
	app "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/app"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	"os"
	"sync"
	"testing"
	"time"
)

func TestPostgreSQLNavigationPersistenceCASUniqueActiveAndRollback(t *testing.T) {
	pool, uow := groupOpsListIntegrationPool(t)
	ctx := context.Background()
	raw, e := os.ReadFile("../../../migrations/0255_group_ops_navigation.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	var staff int64
	if e = pool.QueryRow(ctx, `INSERT INTO admin_users(username,password_hash,display_name)VALUES('nav','$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$ZGlnaWVzdA','导航员工')RETURNING id`).Scan(&staff); e != nil {
		t.Fatal(e)
	}
	id := groupOpsListPlan(t, ctx, pool, staff, "导航计划", p.PlanDraft, time.Now().UTC())
	repo, e := NewPostgreSQL(pool, uow)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	r := p.NavigationRun{StaffID: staff, SenderUserID: "same-account", PlanID: id, PlanName: "导航计划", PlanRevision: 1, Revision: 1, State: "paused", ResumeState: "waiting", Targets: []p.NavigationTarget{{AssetID: 1, ChatID: "a", Name: "同名"}, {AssetID: 2, ChatID: "b", Name: "同名"}}, Observations: []p.NavigationObservation{}, CreatedAt: now, UpdatedAt: now}
	key := sha256.Sum256([]byte("original-key"))
	if e = uow.Within(ctx, func(tx context.Context) error { var err error; r, err = repo.CreateNavigation(tx, r, key); return err }); e != nil {
		t.Fatal(e)
	}
	// Recreate the repository and read through a new transaction, like a page/app restart.
	reopened, _ := NewPostgreSQL(pool, uow)
	if e = uow.Within(ctx, func(tx context.Context) error {
		got, err := reopened.LatestNavigation(tx, staff)
		if err != nil {
			return err
		}
		if got.ID != r.ID || got.SenderUserID != "same-account" || got.ResumeState != "waiting" || len(got.Targets) != 2 {
			t.Fatalf("restart=%+v", got)
		}
		foreign, err := reopened.LatestNavigation(tx, staff+1)
		if err != nil {
			return err
		}
		if foreign != nil {
			t.Fatal("foreign run visible")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e = uow.Within(ctx, func(tx context.Context) error {
		_, err := repo.CreateNavigation(tx, r, sha256.Sum256([]byte("parallel")))
		return err
	}); !errors.Is(e, app.ErrConflict) {
		t.Fatal("two active runs", e)
	}
	// Concurrent writers serialize and CAS permits one update against this revision.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- uow.Within(ctx, func(tx context.Context) error {
				locked, err := repo.LockNavigation(tx, r.ID)
				if err != nil {
					return err
				}
				locked.State = "waiting"
				locked.Revision = 2
				return repo.SaveNavigation(tx, locked, 1)
			})
		}()
	}
	wg.Wait()
	close(results)
	passed, conflicted := 0, 0
	for e := range results {
		if e == nil {
			passed++
		} else if errors.Is(e, app.ErrConflict) {
			conflicted++
		} else {
			t.Fatal(e)
		}
	}
	if passed != 1 || conflicted != 1 {
		t.Fatal(passed, conflicted)
	}
	rollback := errors.New("rollback entire progress")
	if e = uow.Within(ctx, func(tx context.Context) error {
		locked, err := repo.LockNavigation(tx, r.ID)
		if err != nil {
			return err
		}
		locked.Revision++
		locked.Cursor = 1
		locked.Observations = append(locked.Observations, p.NavigationObservation{Step: 0, ChatID: "a", ObservedAt: now})
		if err = repo.SaveNavigation(tx, locked, 2); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(e, rollback) {
		t.Fatal(e)
	}
	if e = uow.Within(ctx, func(tx context.Context) error {
		got, err := repo.NavigationByKey(tx, staff, key)
		if err == nil && (got.Revision != 2 || got.Cursor != 0 || len(got.Observations) != 0) {
			t.Fatalf("partial transaction persisted=%+v", got)
		}
		return err
	}); e != nil {
		t.Fatal(e)
	}
}

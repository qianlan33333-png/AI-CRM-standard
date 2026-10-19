package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

type navigationSnapshot struct {
	Run    p.NavigationRun `json:"run"`
	Sender string          `json:"sender"`
	Resume string          `json:"resume"`
}

func navigationJSON(r p.NavigationRun) ([]byte, error) {
	return json.Marshal(navigationSnapshot{r, r.SenderUserID, r.ResumeState})
}

type navigationRow interface{ Scan(...any) error }

func scanNavigation(row navigationRow) (*p.NavigationRun, error) {
	var r p.NavigationRun
	var raw []byte
	if err := row.Scan(&r.ID, &r.StaffID, &r.Revision, &r.State, &raw); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var saved navigationSnapshot
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	saved.Run.ID = r.ID
	saved.Run.StaffID = r.StaffID
	saved.Run.Revision = r.Revision
	saved.Run.State = r.State
	saved.Run.SenderUserID = saved.Sender
	saved.Run.ResumeState = saved.Resume
	return &saved.Run, nil
}
func (r *Repository) NavigationPlans(ctx context.Context, staff int64) (out []p.SidebarPlan, err error) {
	out = []p.SidebarPlan{}
	tx, err := transaction(ctx)
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT p.id,p.name,p.status,p.revision FROM group_ops_plans p WHERE p.status<>'archived'
 AND (SELECT count(*) FROM group_ops_plan_members m WHERE m.plan_id=p.id)=1
 AND EXISTS(SELECT 1 FROM group_ops_plan_members m WHERE m.plan_id=p.id AND m.staff_id=$1)
 AND EXISTS(SELECT 1 FROM group_ops_plan_group_assets a WHERE a.plan_id=p.id) ORDER BY p.updated_at DESC,p.id DESC`, staff)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item p.SidebarPlan
		if err = rows.Scan(&item.ID, &item.Name, &item.Status, &item.Revision); err != nil {
			return out, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (r *Repository) LatestNavigation(ctx context.Context, staff int64) (*p.NavigationRun, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return nil, err
	}
	return scanNavigation(tx.QueryRow(ctx, `SELECT id,staff_id,revision,state,snapshot FROM group_ops_navigation_runs WHERE staff_id=$1 ORDER BY id DESC LIMIT 1`, staff))
}
func (r *Repository) NavigationByKey(ctx context.Context, staff int64, key [32]byte) (*p.NavigationRun, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return nil, err
	}
	return scanNavigation(tx.QueryRow(ctx, `SELECT id,staff_id,revision,state,snapshot FROM group_ops_navigation_runs WHERE staff_id=$1 AND key_digest=$2`, staff, key[:]))
}
func (r *Repository) LockNavigation(ctx context.Context, id int64) (p.NavigationRun, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return p.NavigationRun{}, err
	}
	run, err := scanNavigation(tx.QueryRow(ctx, `SELECT id,staff_id,revision,state,snapshot FROM group_ops_navigation_runs WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return p.NavigationRun{}, err
	}
	if run == nil {
		return p.NavigationRun{}, ErrNotFound
	}
	return *run, nil
}
func (r *Repository) CreateNavigation(ctx context.Context, run p.NavigationRun, key [32]byte) (p.NavigationRun, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return run, err
	}
	raw, err := navigationJSON(run)
	if err != nil {
		return run, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO group_ops_navigation_runs(staff_id,plan_id,key_digest,plan_revision,revision,state,snapshot,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, run.StaffID, run.PlanID, key[:], run.PlanRevision, run.Revision, run.State, raw, run.CreatedAt, run.UpdatedAt).Scan(&run.ID)
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		return p.NavigationRun{}, ErrConflict
	}
	return run, err
}
func (r *Repository) SaveNavigation(ctx context.Context, run p.NavigationRun, old int64) error {
	tx, err := transaction(ctx)
	if err != nil {
		return err
	}
	raw, err := navigationJSON(run)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE group_ops_navigation_runs SET revision=$1,state=$2,snapshot=$3,updated_at=$4 WHERE id=$5 AND staff_id=$6 AND revision=$7`, run.Revision, run.State, raw, run.UpdatedAt, run.ID, run.StaffID, old)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

func (r *Repository) taskPackage(ctx context.Context, query string, args ...any) (*p.TaskPackage, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return nil, err
	}
	var out p.TaskPackage
	var runID int64
	err = tx.QueryRow(ctx, query, args...).Scan(&out.ID, &out.PlanID, &out.Markdown, &out.SHA256, &runID, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.Run, err = r.LockNavigation(ctx, runID)
	return &out, err
}
func (r *Repository) TaskPackageByKey(ctx context.Context, staff int64, key [32]byte) (*p.TaskPackage, error) {
	return r.taskPackage(ctx, `SELECT id,plan_id,markdown,sha256,run_id,created_at FROM group_ops_rpa_packages WHERE staff_id=$1 AND key_digest=$2`, staff, key[:])
}
func (r *Repository) LatestTaskPackage(ctx context.Context, plan int64) (*p.TaskPackage, error) {
	return r.taskPackage(ctx, `SELECT id,plan_id,markdown,sha256,run_id,created_at FROM group_ops_rpa_packages WHERE plan_id=$1 ORDER BY id DESC LIMIT 1`, plan)
}

// ReadSequencePackageIdentity is called only after run/plan ownership and
// device binding have been checked, inside the same groupops transaction.
func (r *Repository) ReadSequencePackageIdentity(ctx context.Context, run, staff int64) (id int64, digest string, err error) {
	tx, err := transaction(ctx)
	if err != nil {
		return 0, "", err
	}
	err = tx.QueryRow(ctx, `SELECT id,sha256 FROM group_ops_rpa_packages WHERE run_id=$1 AND staff_id=$2`, run, staff).Scan(&id, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil
	} // legacy open/sequence runs
	return
}
func (r *Repository) InsertTaskPackage(ctx context.Context, item p.TaskPackage, staff int64, key [32]byte) (p.TaskPackage, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return item, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO group_ops_rpa_packages(plan_id,staff_id,key_digest,markdown,sha256,run_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, item.PlanID, staff, key[:], item.Markdown, item.SHA256, item.Run.ID, item.CreatedAt).Scan(&item.ID)
	return item, err
}

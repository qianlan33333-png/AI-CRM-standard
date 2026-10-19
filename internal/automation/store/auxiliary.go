package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	automationport "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/port"
	platformpostgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
)

const auxiliaryColumns = `id,request_digest,purpose,customer_id,staff_id,source_version,dispatch,state,COALESCE(effect_id,''),result_text,failure_code,created_at,completed_at`

func scanAuxiliary(row interface{ Scan(...any) error }) (out automationport.AuxiliaryGeneration, found bool, err error) {
	var raw []byte
	err = row.Scan(&out.ID, &out.RequestDigest, &out.Purpose, &out.Scope.CustomerID, &out.Scope.StaffID, &out.SourceVersion, &raw, &out.State, &out.EffectID, &out.ResultText, &out.FailureCode, &out.CreatedAt, &out.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	err = json.Unmarshal(raw, &out.Dispatch)
	return out, err == nil, err
}
func (r *Repository) CreateAuxiliaryWithin(ctx context.Context, v automationport.AuxiliaryGeneration) (out automationport.AuxiliaryGeneration, reused bool, err error) {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return out, false, err
	}
	raw, err := json.Marshal(v.Dispatch)
	if err != nil {
		return out, false, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO automation_auxiliary_generations(request_digest,purpose,customer_id,staff_id,source_version,dispatch,state,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(request_digest) DO NOTHING RETURNING id`, v.RequestDigest, v.Purpose, v.Scope.CustomerID, v.Scope.StaffID, v.SourceVersion, raw, v.State, v.CreatedAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		out, _, err = scanAuxiliary(tx.QueryRow(ctx, `SELECT `+auxiliaryColumns+` FROM automation_auxiliary_generations WHERE request_digest=$1`, v.RequestDigest))
		return out, true, err
	}
	if err != nil {
		return out, false, err
	}
	v.ID = id
	return v, false, nil
}
func (r *Repository) BindAuxiliaryWithin(ctx context.Context, id int64, effect string) error {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE automation_auxiliary_generations SET effect_id=$2,state='queued' WHERE id=$1 AND effect_id IS NULL`, id, effect)
	return err
}
func (r *Repository) ReadAuxiliaryWithin(ctx context.Context, id int64) (automationport.AuxiliaryGeneration, bool, error) {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return automationport.AuxiliaryGeneration{}, false, err
	}
	return scanAuxiliary(tx.QueryRow(ctx, `SELECT `+auxiliaryColumns+` FROM automation_auxiliary_generations WHERE id=$1`, id))
}
func (r *Repository) AuxiliaryByEffectWithin(ctx context.Context, effect string, lock bool) (automationport.AuxiliaryGeneration, bool, error) {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return automationport.AuxiliaryGeneration{}, false, err
	}
	q := `SELECT ` + auxiliaryColumns + ` FROM automation_auxiliary_generations WHERE effect_id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	return scanAuxiliary(tx.QueryRow(ctx, q, effect))
}
func (r *Repository) CompleteAuxiliaryWithin(ctx context.Context, id int64, state, text, code string, at time.Time) error {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE automation_auxiliary_generations SET state=$2,result_text=$3,failure_code=$4,completed_at=$5 WHERE id=$1`, id, state, text, code, at)
	return err
}

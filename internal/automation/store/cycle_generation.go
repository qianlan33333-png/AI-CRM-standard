package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	automationapp "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/app"
	platformpostgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
)

func (r *Repository) FreezeCycleCommandWithin(ctx context.Context, key string, c automationapp.RunConfirmCommand) (out automationapp.RunConfirmCommand, done bool, err error) {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return
	}
	if c.PackageID > 0 {
		raw, _ := json.Marshal(c)
		_, err = tx.Exec(ctx, `INSERT INTO automation_cycle_generations(occurrence,command) VALUES($1,$2) ON CONFLICT DO NOTHING`, key, raw)
		if err != nil {
			return
		}
	}
	var raw []byte
	var id *int64
	err = tx.QueryRow(ctx, `SELECT command,run_id FROM automation_cycle_generations WHERE occurrence=$1`, key).Scan(&raw, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
		return
	}
	if err != nil {
		return
	}
	err = json.Unmarshal(raw, &out)
	done = id != nil
	return
}
func (r *Repository) CompleteCycleWithin(ctx context.Context, key string, id int64) error {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE automation_cycle_generations SET run_id=$2 WHERE occurrence=$1 AND run_id IS NULL`, key, id)
	return err
}

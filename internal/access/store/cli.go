package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	accessport "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
)

func (*PostgreSQL) CLIPolicy(ctx context.Context, userID int64) (accessport.CLIPolicy, error) {
	db, err := tx(ctx)
	if err != nil {
		return accessport.CLIPolicy{}, err
	}
	result := accessport.CLIPolicy{AllowedCIDRs: []string{}, Version: 1}
	err = db.QueryRow(ctx, `SELECT allowed_cidrs,version FROM admin_cli_policies WHERE admin_user_id=$1`, userID).Scan(&result.AllowedCIDRs, &result.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return result, err
}
func (*PostgreSQL) ReplaceCLIPolicy(ctx context.Context, userID int64, cidrs []string) (accessport.CLIPolicy, error) {
	db, err := tx(ctx)
	if err != nil {
		return accessport.CLIPolicy{}, err
	}
	if cidrs == nil {
		cidrs = []string{}
	}
	var result accessport.CLIPolicy
	err = db.QueryRow(ctx, `INSERT INTO admin_cli_policies(admin_user_id,allowed_cidrs,version) VALUES($1,$2,2)
 ON CONFLICT(admin_user_id) DO UPDATE SET allowed_cidrs=EXCLUDED.allowed_cidrs,
 version=admin_cli_policies.version+1 RETURNING allowed_cidrs,version`, userID, cidrs).Scan(&result.AllowedCIDRs, &result.Version)
	return result, err
}

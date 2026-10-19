package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
)

func (*PostgreSQL) CreateRPADevice(ctx context.Context, d p.RPADevice) error {
	db, e := tx(ctx)
	if e != nil {
		return e
	}
	_, e = db.Exec(ctx, `INSERT INTO access_rpa_devices(token_digest,user_code_digest,name,state,pair_expires_at,expires_at,created_at) VALUES($1,$2,$3,'pending',$4,$5,$6)`, d.TokenDigest[:], d.UserCodeDigest[:], d.Name, d.PairExpiresAt, d.ExpiresAt, d.CreatedAt)
	return e
}

type rpaRow interface{ Scan(...any) error }

func scanRPA(row rpaRow) (d p.RPADevice, e error) {
	var token, code []byte
	e = row.Scan(&d.ID, &token, &code, &d.Name, &d.StaffID, &d.SenderUserID, &d.SessionVersion, &d.State, &d.PairExpiresAt, &d.ExpiresAt, &d.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = domain.ErrNotFound
	}
	copy(d.TokenDigest[:], token)
	copy(d.UserCodeDigest[:], code)
	return
}

const rpaColumns = `id,token_digest,user_code_digest,name,COALESCE(staff_id,0),COALESCE(sender_userid,''),COALESCE(session_version,0),state,pair_expires_at,expires_at,created_at`

func (*PostgreSQL) RPADeviceByToken(ctx context.Context, key [32]byte, lock bool) (p.RPADevice, error) {
	db, e := tx(ctx)
	if e != nil {
		return p.RPADevice{}, e
	}
	q := `SELECT ` + rpaColumns + ` FROM access_rpa_devices WHERE token_digest=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	return scanRPA(db.QueryRow(ctx, q, key[:]))
}
func (*PostgreSQL) RPADeviceByUserCode(ctx context.Context, key [32]byte, lock bool) (p.RPADevice, error) {
	db, e := tx(ctx)
	if e != nil {
		return p.RPADevice{}, e
	}
	q := `SELECT ` + rpaColumns + ` FROM access_rpa_devices WHERE user_code_digest=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	return scanRPA(db.QueryRow(ctx, q, key[:]))
}
func (*PostgreSQL) ApproveRPADevice(ctx context.Context, d p.RPADevice) error {
	db, e := tx(ctx)
	if e != nil {
		return e
	}
	tag, e := db.Exec(ctx, `UPDATE access_rpa_devices SET staff_id=$2,sender_userid=$3,session_version=$4,state='active',expires_at=$5 WHERE id=$1 AND state='pending'`, d.ID, d.StaffID, d.SenderUserID, d.SessionVersion, d.ExpiresAt)
	if e == nil && tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return e
}
func (*PostgreSQL) RevokeRPADevice(ctx context.Context, id, staff int64) error {
	db, e := tx(ctx)
	if e != nil {
		return e
	}
	tag, e := db.Exec(ctx, `UPDATE access_rpa_devices SET state='revoked' WHERE id=$1 AND staff_id=$2 AND state='active'`, id, staff)
	if e == nil && tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return e
}

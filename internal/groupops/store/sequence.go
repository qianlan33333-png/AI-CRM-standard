package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	"time"
)

// Use the same PostgreSQL clock as Access session creation, without allowing
// GroupOps to read Access tables or trusting browser-supplied timestamps.
func (r *Repository) SequenceAccountTime(ctx context.Context) (out time.Time, e error) {
	tx, e := transaction(ctx)
	if e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&out)
	return
}

func (r *Repository) ReadSequenceMessages(ctx context.Context, plan int64) (out []p.SequenceMessage, e error) {
	out = []p.SequenceMessage{}
	tx, e := transaction(ctx)
	if e != nil {
		return out, e
	}
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT messages FROM group_ops_rpa_sequences WHERE plan_id=$1`, plan).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(raw, &out)
	return
}
func (r *Repository) SaveSequenceMessages(ctx context.Context, plan int64, messages []p.SequenceMessage) error {
	tx, e := transaction(ctx)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(messages)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO group_ops_rpa_sequences(plan_id,messages,updated_at) VALUES($1,$2,clock_timestamp()) ON CONFLICT(plan_id) DO UPDATE SET messages=EXCLUDED.messages,updated_at=EXCLUDED.updated_at`, plan, raw)
	return e
}

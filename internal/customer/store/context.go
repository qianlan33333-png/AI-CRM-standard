package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	customerport "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/port"
	platformpostgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
)

func (PostgreSQL) ReadContextWithin(ctx context.Context, key customerport.ContextScope) (out customerport.CustomerContext, found bool, err error) {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return out, false, err
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT projection FROM customer_contexts WHERE customer_id=$1 AND staff_id=$2`, key.CustomerID, key.StaffID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err == nil, err
}
func (s PostgreSQL) PrepareContextWithin(ctx context.Context, v customerport.CustomerContext) (customerport.CustomerContext, error) {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return v, err
	}
	// Lock by the canonical owner and scope, including concurrent first prepares.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, stringKey(v.Scope)); err != nil {
		return v, err
	}
	old, found, err := s.ReadContextWithin(ctx, v.Scope)
	if err != nil {
		return v, err
	}
	if found && old.SourceVersion == v.SourceVersion {
		return old, nil
	}
	v.ChunkAnalyses = map[string]customerport.ContextAnalysis{}
	if found {
		v.Analysis = old.Analysis
		v.AnalysisSourceVersion = old.AnalysisSourceVersion
		v.AnalyzedAt = old.AnalyzedAt
		for _, ch := range v.Chunks {
			if a, ok := old.ChunkAnalyses[ch.Hash]; ok {
				v.ChunkAnalyses[ch.Hash] = a
			}
		}
	}
	composeContextAnalysis(&v)
	raw, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO customer_contexts(customer_id,staff_id,source_version,projection,prepared_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(customer_id,staff_id) DO UPDATE SET source_version=excluded.source_version,projection=excluded.projection,prepared_at=excluded.prepared_at`, v.Scope.CustomerID, v.Scope.StaffID, v.SourceVersion, raw, v.PreparedAt)
	return v, err
}
func (s PostgreSQL) ApplyContextWithin(ctx context.Context, key customerport.ContextScope, version string, a customerport.ContextAnalysis, at time.Time) error {
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT 1 FROM customer_contexts WHERE customer_id=$1 AND staff_id=$2 FOR UPDATE`, key.CustomerID, key.StaffID); err != nil {
		return err
	}
	v, found, err := s.ReadContextWithin(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		return customerport.ErrContextStale
	}
	valid := false
	for _, ch := range v.Chunks {
		if ch.Hash == version {
			valid = true
			break
		}
	}
	if !valid {
		return customerport.ErrContextStale
	}
	if v.ChunkAnalyses == nil {
		v.ChunkAnalyses = map[string]customerport.ContextAnalysis{}
	}
	v.ChunkAnalyses[version] = a
	v.AnalyzedAt = &at
	composeContextAnalysis(&v)
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE customer_contexts SET projection=$4 WHERE customer_id=$1 AND staff_id=$2 AND source_version=$3`, key.CustomerID, key.StaffID, v.SourceVersion, raw)
	if err == nil && tag.RowsAffected() != 1 {
		return customerport.ErrContextStale
	}
	return err
}
func stringKey(k customerport.ContextScope) string { raw, _ := json.Marshal(k); return string(raw) }
func composeContextAnalysis(v *customerport.CustomerContext) {
	if len(v.Chunks) == 0 {
		return
	}
	a := customerport.ContextAnalysis{}
	var summaries []string
	for _, ch := range v.Chunks {
		piece, ok := v.ChunkAnalyses[ch.Hash]
		if !ok {
			return
		}
		summaries = append(summaries, piece.Summary)
		a.SummaryEvidenceRefs = append(a.SummaryEvidenceRefs, piece.SummaryEvidenceRefs...)
		a.Needs = append(a.Needs, piece.Needs...)
		a.Blockers = append(a.Blockers, piece.Blockers...)
		a.Uncertainties = append(a.Uncertainties, piece.Uncertainties...)
	}
	a.Summary = strings.Join(summaries, "\n\n")
	v.Analysis = &a
	v.AnalysisSourceVersion = v.SourceVersion
}

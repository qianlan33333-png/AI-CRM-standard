package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	customerport "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/port"
	platformport "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/port"
)

type ContextStore interface {
	ReadContextWithin(context.Context, customerport.ContextScope) (customerport.CustomerContext, bool, error)
	PrepareContextWithin(context.Context, customerport.CustomerContext) (customerport.CustomerContext, error)
	ApplyContextWithin(context.Context, customerport.ContextScope, string, customerport.ContextAnalysis, time.Time) error
}

type ContextService struct {
	UOW   platformport.UnitOfWork
	Store ContextStore
}

// NormalizeContext is deterministic. It never infers a customer's needs from
// staff speech and never turns missing source data into a negative fact.
func NormalizeContext(v customerport.CustomerContext) (customerport.CustomerContext, error) {
	if v.Scope.CustomerID < 1 || v.Scope.StaffID < 0 || v.ThroughAt.IsZero() {
		return v, errors.New("invalid customer context")
	}
	seen := map[string]bool{}
	items := make([]customerport.ContextEvidence, 0, len(v.Evidence))
	for _, e := range v.Evidence {
		if e.Ref == "" || e.OccurredAt.IsZero() || (e.Kind != "chat" && e.Kind != "survey" && e.Kind != "manual") || (e.Role != "customer" && e.Role != "staff" && e.Role != "human_confirmed") {
			return v, errors.New("invalid context evidence")
		}
		if seen[e.Ref] {
			continue
		}
		seen[e.Ref] = true
		e.Text = strings.TrimSpace(e.Text)
		if e.Text == "" {
			continue
		}
		e.OccurredAt = e.OccurredAt.UTC()
		items = append(items, e)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].Ref < items[j].Ref
		}
		return items[i].OccurredAt.Before(items[j].OccurredAt)
	})
	v.Evidence = items
	v.TextCount = len(items)
	v.FromAt = nil
	if len(items) > 0 {
		at := items[0].OccurredAt
		v.FromAt = &at
	}
	raw, _ := json.Marshal(struct {
		Scope                      customerport.ContextScope
		Evidence                   []customerport.ContextEvidence
		SourceCount, UnparsedCount int
	}{v.Scope, items, v.SourceCount, v.UnparsedCount})
	sum := sha256.Sum256(raw)
	v.SourceVersion = "customer-context.v1:" + hex.EncodeToString(sum[:])
	v.Analysis = nil
	v.AnalysisSourceVersion = ""
	v.AnalyzedAt = nil
	v.ChunkAnalyses = nil
	v.Chunks = ContextChunks(v.Scope, items)
	return v, nil
}

// Each model input fits the existing 16000-byte GenerationContext field.
// Large messages are split on UTF-8 boundaries; references still identify the
// original source. All chunks must finish before analysis is called current.
func ContextChunks(scope customerport.ContextScope, items []customerport.ContextEvidence) []customerport.ContextChunk {
	var out []customerport.ContextChunk
	var batch []customerport.ContextEvidence
	flush := func() {
		if len(batch) == 0 {
			return
		}
		raw, _ := json.Marshal(struct {
			Scope    customerport.ContextScope
			Evidence []customerport.ContextEvidence
			Version  string
		}{scope, batch, "context-extractor.v1"})
		sum := sha256.Sum256(raw)
		out = append(out, customerport.ContextChunk{Hash: hex.EncodeToString(sum[:]), Evidence: batch})
		batch = nil
	}
	var add func(customerport.ContextEvidence)
	add = func(e customerport.ContextEvidence) {
		candidate := append(append([]customerport.ContextEvidence{}, batch...), e)
		raw, _ := json.Marshal(candidate)
		if len(raw) <= 16000 {
			batch = candidate
			return
		}
		if len(batch) > 0 {
			flush()
			add(e)
			return
		}
		runes := []rune(e.Text)
		if len(runes) < 2 {
			return
		}
		mid := len(runes) / 2
		left, right := e, e
		left.Text = string(runes[:mid])
		right.Text = string(runes[mid:])
		add(left)
		add(right)
	}
	for _, e := range items {
		add(e)
	}
	flush()
	return out
}

func ValidateContextAnalysis(v customerport.ContextAnalysis, evidence []customerport.ContextEvidence) error {
	if strings.TrimSpace(v.Summary) == "" {
		return errors.New("empty context summary")
	}
	refs := map[string]bool{}
	for _, e := range evidence {
		refs[e.Ref] = e.Role == "customer" || e.Role == "human_confirmed"
	}
	if len(v.SummaryEvidenceRefs) == 0 {
		return errors.New("summary has no evidence")
	}
	for _, r := range v.SummaryEvidenceRefs {
		if _, ok := refs[r]; !ok {
			return errors.New("summary cites missing evidence")
		}
	}
	for _, group := range [][]customerport.ContextStatement{v.Needs, v.Blockers, v.Uncertainties} {
		for _, s := range group {
			if strings.TrimSpace(s.Text) == "" || len(s.EvidenceRefs) == 0 {
				return errors.New("statement has no evidence")
			}
			for _, r := range s.EvidenceRefs {
				if !refs[r] {
					return errors.New("statement cites unavailable customer evidence")
				}
			}
		}
	}
	return nil
}

func (s *ContextService) ReadCustomerContext(ctx context.Context, key customerport.ContextScope) (out customerport.CustomerContext, found bool, err error) {
	if key.CustomerID < 1 || key.StaffID < 0 {
		return out, false, errors.New("invalid context scope")
	}
	err = s.UOW.Within(ctx, func(tx context.Context) error {
		var e error
		out, found, e = s.Store.ReadContextWithin(tx, key)
		return e
	})
	return
}
func (s *ContextService) PrepareCustomerContext(ctx context.Context, v customerport.CustomerContext) (out customerport.CustomerContext, err error) {
	v, err = NormalizeContext(v)
	if err != nil {
		return out, err
	}
	v.PreparedAt = time.Now().UTC()
	err = s.UOW.Within(ctx, func(tx context.Context) error { var e error; out, e = s.Store.PrepareContextWithin(tx, v); return e })
	return
}
func (s *ContextService) ApplyCustomerContextWithin(ctx context.Context, key customerport.ContextScope, version string, a customerport.ContextAnalysis, at time.Time) error {
	v, found, err := s.Store.ReadContextWithin(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		return customerport.ErrContextStale
	}
	var evidence []customerport.ContextEvidence
	for _, chunk := range v.Chunks {
		if chunk.Hash == version {
			evidence = chunk.Evidence
			break
		}
	}
	if len(evidence) == 0 {
		return customerport.ErrContextStale
	}
	if err = ValidateContextAnalysis(a, evidence); err != nil {
		return errors.Join(customerport.ErrContextAnalysisInvalid, err)
	}
	return s.Store.ApplyContextWithin(ctx, key, version, a, at)
}

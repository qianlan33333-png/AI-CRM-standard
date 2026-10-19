package port

import (
	"context"
	"errors"
	"time"

	customerdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
)

var ErrContextStale = errors.New("customer context changed")
var ErrContextAnalysisInvalid = errors.New("customer context analysis invalid")

// ContextScope is part of the projection key. Customer identity does not grant
// permission to another employee's private conversation.
type ContextScope struct {
	CustomerID customerdomain.CustomerID `json:"customer_id"`
	StaffID    int64                     `json:"staff_id"`
}

type ContextEvidence struct {
	Ref        string    `json:"ref"`
	Kind       string    `json:"kind"`
	Role       string    `json:"role"`
	Text       string    `json:"text"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ContextStatement struct {
	Text         string   `json:"text"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type ContextAnalysis struct {
	Summary             string             `json:"summary"`
	SummaryEvidenceRefs []string           `json:"summary_evidence_refs"`
	Needs               []ContextStatement `json:"needs"`
	Blockers            []ContextStatement `json:"blockers"`
	Uncertainties       []ContextStatement `json:"uncertainties"`
}

type ContextChunk struct {
	Hash     string            `json:"hash"`
	Evidence []ContextEvidence `json:"evidence"`
}

type CustomerContext struct {
	Scope                 ContextScope               `json:"scope"`
	SourceVersion         string                     `json:"source_version"`
	Evidence              []ContextEvidence          `json:"evidence,omitempty"`
	SourceCount           int                        `json:"source_count"`
	TextCount             int                        `json:"text_count"`
	UnparsedCount         int                        `json:"unparsed_count"`
	FromAt                *time.Time                 `json:"from_at,omitempty"`
	ThroughAt             time.Time                  `json:"through_at"`
	PreparedAt            time.Time                  `json:"prepared_at"`
	Analysis              *ContextAnalysis           `json:"analysis,omitempty"`
	AnalysisSourceVersion string                     `json:"analysis_source_version,omitempty"`
	AnalyzedAt            *time.Time                 `json:"analyzed_at,omitempty"`
	Chunks                []ContextChunk             `json:"chunks,omitempty"`
	ChunkAnalyses         map[string]ContextAnalysis `json:"chunk_analyses,omitempty"`
}

type ContextReader interface {
	ReadCustomerContext(context.Context, ContextScope) (CustomerContext, bool, error)
}

type ContextWriter interface {
	// The caller already owns the model completion transaction.
	ApplyCustomerContextWithin(context.Context, ContextScope, string, ContextAnalysis, time.Time) error
}

type ContextPreparer interface {
	PrepareCustomerContext(context.Context, CustomerContext) (CustomerContext, error)
}

package port

import (
	"context"
	customerport "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/port"
	"time"
)

type AuxiliaryGeneration struct {
	ID            int64                     `json:"id"`
	RequestDigest string                    `json:"-"`
	Purpose       string                    `json:"purpose"`
	Scope         customerport.ContextScope `json:"-"`
	SourceVersion string                    `json:"source_version"`
	Dispatch      GenerationDispatch        `json:"-"`
	State         string                    `json:"state"`
	EffectID      string                    `json:"-"`
	ResultText    string                    `json:"result,omitempty"`
	FailureCode   string                    `json:"failure_code,omitempty"`
	CreatedAt     time.Time                 `json:"created_at"`
	CompletedAt   *time.Time                `json:"completed_at,omitempty"`
}

type AuxiliaryCommand struct {
	Purpose        string
	Scope          customerport.ContextScope
	SourceVersion  string
	IdempotencyKey string
	RolePrompt     string
	TaskPrompt     string
	Context        GenerationContext
}

type AuxiliaryService interface {
	RequestAuxiliary(context.Context, AuxiliaryCommand) (AuxiliaryGeneration, error)
	ReadAuxiliary(context.Context, int64, customerport.ContextScope) (AuxiliaryGeneration, error)
}

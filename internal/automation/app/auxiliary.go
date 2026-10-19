package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	automationport "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/port"
	customerport "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/port"
	effectport "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
	platformaudit "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/audit"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/platform/idempotency"
	platformport "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/port"
)

type AuxiliaryStore interface {
	CreateAuxiliaryWithin(context.Context, automationport.AuxiliaryGeneration) (automationport.AuxiliaryGeneration, bool, error)
	BindAuxiliaryWithin(context.Context, int64, string) error
	ReadAuxiliaryWithin(context.Context, int64) (automationport.AuxiliaryGeneration, bool, error)
	AuxiliaryByEffectWithin(context.Context, string, bool) (automationport.AuxiliaryGeneration, bool, error)
	CompleteAuxiliaryWithin(context.Context, int64, string, string, string, time.Time) error
}

// AuxiliaryRuntime only accepts existing EER/River work. It owns no worker,
// retry, lease, provider client, or outbound continuation.
type AuxiliaryRuntime struct {
	UOW      platformport.UnitOfWork
	Store    AuxiliaryStore
	Effects  effectport.TransactionalAccepter
	Policy   automationport.GenerationModelPolicyReader
	Contexts customerport.ContextWriter
	Audit    interface {
		Append(context.Context, platformaudit.Event) (platformaudit.Event, error)
	}
}

func (s *AuxiliaryRuntime) RequestAuxiliary(ctx context.Context, c automationport.AuxiliaryCommand) (out automationport.AuxiliaryGeneration, err error) {
	if s == nil || s.Policy == nil || c.Scope.CustomerID < 1 || c.Scope.StaffID < 0 || c.SourceVersion == "" || c.IdempotencyKey == "" || (c.Purpose != "context" && c.Purpose != "sidebar_reply") || c.RolePrompt == "" || c.TaskPrompt == "" || !c.Context.Valid() {
		return out, ErrRuntimeInvalid
	}
	policy, err := s.Policy.GenerationModelPolicy(ctx)
	if err != nil {
		return out, ErrRuntimeNotReady
	}
	raw, _ := json.Marshal(struct {
		Purpose                 string
		Scope                   customerport.ContextScope
		Source, Key, Role, Task string
		Context                 automationport.GenerationContext
	}{c.Purpose, c.Scope, c.SourceVersion, c.IdempotencyKey, c.RolePrompt, c.TaskPrompt, c.Context})
	digest := effectport.Hash("automation.auxiliary.v1", string(raw))
	now := time.Now().UTC()
	dispatch := automationport.GenerationDispatch{AgentCode: "standard_" + c.Purpose, RolePrompt: c.RolePrompt, TaskPrompt: c.TaskPrompt, Context: c.Context, ModelPolicy: policy, AcceptedAt: now, PayloadDigest: effectport.Hash("automation.auxiliary.payload.v1", string(raw))}
	err = s.UOW.Within(ctx, func(tx context.Context) error {
		var reused bool
		var e error
		out, reused, e = s.Store.CreateAuxiliaryWithin(tx, automationport.AuxiliaryGeneration{RequestDigest: string(digest), Purpose: c.Purpose, Scope: c.Scope, SourceVersion: c.SourceVersion, Dispatch: dispatch, State: "accepted", CreatedAt: now})
		if e != nil || reused {
			return e
		}
		env := effectport.Envelope{Owner: effectport.OwnerAutomation, Kind: effectport.KindAIAgentGenerate, SourceRefDigest: effectport.Hash("automation.auxiliary.source", strconv.FormatInt(out.ID, 10)), TargetRefDigest: effectport.Hash("automation.auxiliary.target", strconv.FormatInt(int64(c.Scope.CustomerID), 10), strconv.FormatInt(c.Scope.StaffID, 10)), PayloadDigest: dispatch.PayloadDigest, PolicyVersionHash: effectport.Hash("automation.auxiliary.policy", mustAuxJSON(policy))}
		p, _, e := s.Effects.AcceptAndQueueWithin(tx, effectport.AcceptCommand{ReceiptKey: effectport.Hash("automation.auxiliary.accept", string(digest)), Envelope: env})
		if e != nil {
			return e
		}
		if e = s.Store.BindAuxiliaryWithin(tx, out.ID, p.ID); e != nil {
			return e
		}
		out.State = "queued"
		out.EffectID = p.ID
		if s.Audit != nil {
			_, e = s.Audit.Append(tx, platformaudit.Event{IdempotencyKey: idempotency.Key("auxiliary:" + strconv.FormatInt(out.ID, 10)), Action: "automation.auxiliary.accepted", ActorType: "service", ActorID: "standard-context", ResourceType: "auxiliary_generation", ResourceID: strconv.FormatInt(out.ID, 10), Payload: json.RawMessage(`{}`), OccurredAt: now})
		}
		return e
	})
	return
}
func mustAuxJSON(v any) string { raw, _ := json.Marshal(v); return string(raw) }
func (s *AuxiliaryRuntime) ReadAuxiliary(ctx context.Context, id int64, key customerport.ContextScope) (out automationport.AuxiliaryGeneration, err error) {
	err = s.UOW.Within(ctx, func(tx context.Context) error {
		var found bool
		var e error
		out, found, e = s.Store.ReadAuxiliaryWithin(tx, id)
		if e != nil {
			return e
		}
		if !found || out.Scope != key {
			return ErrRuntimeNotFound
		}
		return nil
	})
	return
}
func (s *AuxiliaryRuntime) GenerationDispatch(ctx context.Context, effectID string) (out automationport.GenerationDispatch, found bool, err error) {
	err = s.UOW.Within(ctx, func(tx context.Context) error {
		v, ok, e := s.Store.AuxiliaryByEffectWithin(tx, effectID, false)
		if e != nil || !ok {
			return e
		}
		found = true
		out = v.Dispatch
		out.ItemID = v.ID
		out.RunID = v.ID
		out.EffectID = effectID
		return nil
	})
	return
}
func (s *AuxiliaryRuntime) CompleteAuxiliary(ctx context.Context, c automationport.GenerationCompletion) (bool, error) {
	v, found, err := s.Store.AuxiliaryByEffectWithin(ctx, c.EffectID, true)
	if err != nil || !found {
		return found, err
	}
	if v.State != "queued" && v.State != "accepted" && v.State != string(effectport.StateUnknown) && v.State != string(effectport.StateRetryable) {
		return true, nil
	}
	state, text, code := string(c.State), string(c.Artifact.Payload), c.FailureCode
	if c.State == effectport.StateExecuted && v.Purpose == "context" {
		var a customerport.ContextAnalysis
		d := json.NewDecoder(bytes.NewReader(c.Artifact.Payload))
		d.DisallowUnknownFields()
		if d.Decode(&a) != nil || !errors.Is(d.Decode(&struct{}{}), io.EOF) {
			state = string(effectport.StateFinalFailed)
			code = "context_analysis_invalid"
			text = ""
		} else {
			err = s.Contexts.ApplyCustomerContextWithin(ctx, v.Scope, v.SourceVersion, a, c.CompletedAt)
			if err != nil {
				if !errors.Is(err, customerport.ErrContextStale) && !errors.Is(err, customerport.ErrContextAnalysisInvalid) {
					return true, err
				}
				state = string(effectport.StateFinalFailed)
				code = "context_analysis_rejected"
				text = ""
			}
		}
	}
	return true, s.Store.CompleteAuxiliaryWithin(ctx, v.ID, state, text, code, c.CompletedAt)
}

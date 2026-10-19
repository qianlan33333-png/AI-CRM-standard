package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	automationdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/domain"
	automationport "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/port"
	operationport "github.com/qianlan33333-png/AI-CRM-standard/internal/operationcycle/port"
	platformport "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/port"
	"time"
)

type CycleGenerationStore interface {
	FreezeCycleCommandWithin(context.Context, string, RunConfirmCommand) (RunConfirmCommand, bool, error)
	CompleteCycleWithin(context.Context, string, int64) error
}

type CycleRunRuntime interface {
	CreateBroadcastPreview(context.Context, int64, int64) (automationdomain.RunPreview, error)
	ConfirmRun(context.Context, RunConfirmCommand) (automationdomain.RuntimeRun, error)
}

// CycleGeneration uses existing audience membership, generation and human
// review. It cannot send, evaluate a second rule language or invent customers.
type CycleGeneration struct {
	UOW         platformport.UnitOfWork
	Strategies  operationport.StrategyPageReader
	Store       CycleGenerationStore
	Runtime     CycleRunRuntime
	Agents      automationport.PublishedGenerationReader
	Now         func() time.Time
	ActorActive func(context.Context, int64) (bool, error)
}

func (s *CycleGeneration) ScanScheduled(ctx context.Context) error {
	if s.Agents == nil {
		return ErrRuntimeNotReady
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	for offset := int32(0); ; offset += 100 {
		page, e := s.Strategies.ListOperationCycleStrategies(ctx, 100, offset)
		if e != nil {
			return e
		}
		for _, strategy := range page.Items {
			if strategy.Status != "active" {
				continue
			}
			var def struct {
				Native *operationport.NativeGeneration `json:"native_generation"`
			}
			if json.Unmarshal(strategy.Definition, &def) != nil || def.Native == nil {
				continue
			}
			n := def.Native
			if n.PackageID < 1 || n.IntervalHours < 1 || n.IntervalHours > 720 || n.ActorID < 1 {
				continue
			}
			active, e := s.ActorActive(ctx, n.ActorID)
			if e != nil {
				return e
			}
			if !active {
				continue
			}
			bucket := now.UTC().Unix() / (n.IntervalHours * 3600)
			occurrence := fmt.Sprintf("cycle:%s:%d:%d", strategy.Key, strategy.Version, bucket)
			// An existing occurrence always reuses its frozen preview/digest, even
			// after restart or a changed audience. Preview rows are local only.
			var command RunConfirmCommand
			done := false
			e = s.UOW.Within(ctx, func(tx context.Context) error {
				var err error
				command, done, err = s.Store.FreezeCycleCommandWithin(tx, occurrence, RunConfirmCommand{})
				return err
			})
			if e != nil {
				return e
			}
			if done {
				continue
			}
			if command.PackageID == 0 {
				preview, e := s.Runtime.CreateBroadcastPreview(ctx, n.PackageID, n.ActorID)
				if e != nil {
					continue
				}
				published, found, e := s.Agents.PublishedGeneration(ctx, automationport.AgentID(preview.AgentID), preview.AgentPublishedVersion)
				if e != nil || !found || !published.Valid() {
					continue
				}
				command = RunConfirmCommand{PackageID: preview.PackageID, PackageVersion: preview.PackageVersion, SnapshotID: preview.SnapshotID, AgentID: preview.AgentID, AgentPublishedVersion: preview.AgentPublishedVersion, PreviewDigest: hex.EncodeToString(preview.PreviewDigest[:]), Actor: n.ActorID, IdempotencyKey: occurrence}
				e = s.UOW.Within(ctx, func(tx context.Context) error {
					var err error
					command, done, err = s.Store.FreezeCycleCommandWithin(tx, occurrence, command)
					return err
				})
				if e != nil {
					return e
				}
				if done {
					continue
				}
			}
			run, e := s.Runtime.ConfirmRun(ctx, command)
			if e != nil {
				continue
			}
			if e = s.UOW.Within(ctx, func(tx context.Context) error { return s.Store.CompleteCycleWithin(tx, occurrence, run.ID) }); e != nil {
				return e
			}
		}
		if int(offset)+len(page.Items) >= page.Total || len(page.Items) == 0 {
			return nil
		}
	}
}

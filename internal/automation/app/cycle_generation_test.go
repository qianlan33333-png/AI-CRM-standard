package app

import (
	"context"
	"encoding/json"
	"errors"
	automationdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/domain"
	automationport "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/port"
	operationport "github.com/qianlan33333-png/AI-CRM-standard/internal/operationcycle/port"
	"reflect"
	"testing"
	"time"
)

type cycleStrategies struct{ items []operationport.Strategy }

func (s *cycleStrategies) ListOperationCycleStrategies(context.Context, int32, int32) (operationport.StrategyPage, error) {
	return operationport.StrategyPage{Items: s.items, Total: len(s.items)}, nil
}

type cycleStore struct {
	commands       map[string]RunConfirmCommand
	done           map[string]bool
	failCompletion bool
}

func (s *cycleStore) FreezeCycleCommandWithin(_ context.Context, key string, c RunConfirmCommand) (RunConfirmCommand, bool, error) {
	old, found := s.commands[key]
	if !found && c.PackageID > 0 {
		s.commands[key] = c
		old = c
	}
	return old, s.done[key], nil
}
func (s *cycleStore) CompleteCycleWithin(_ context.Context, key string, id int64) error {
	if s.failCompletion {
		s.failCompletion = false
		return errors.New("simulated process interruption")
	}
	s.done[key] = true
	return nil
}

type cycleRuntime struct {
	previewCalls, accepted int
	receipts               map[string]RunConfirmCommand
}

func (s *cycleRuntime) CreateBroadcastPreview(context.Context, int64, int64) (automationdomain.RunPreview, error) {
	s.previewCalls++
	return automationdomain.RunPreview{PackageID: 1, PackageVersion: 2, SnapshotID: 3, AgentID: 4, AgentPublishedVersion: 5, PreviewDigest: [32]byte{7}}, nil
}
func (s *cycleRuntime) ConfirmRun(_ context.Context, c RunConfirmCommand) (automationdomain.RuntimeRun, error) {
	if prior, ok := s.receipts[c.IdempotencyKey]; ok {
		if !reflect.DeepEqual(prior, c) {
			return automationdomain.RuntimeRun{}, errors.New("changed frozen input")
		}
	} else {
		s.receipts[c.IdempotencyKey] = c
		s.accepted++
	}
	return automationdomain.RuntimeRun{ID: 11, State: automationport.RunPendingReview}, nil
}
func (*cycleRuntime) PublishedGeneration(context.Context, automationport.AgentID, int64) (automationport.PublishedGeneration, bool, error) {
	return automationport.PublishedGeneration{AgentID: 4, PublishedVersion: 5, AgentCode: "test", RolePrompt: "判断", TaskPrompt: "本次目标"}, true, nil
}
func TestCycleGenerationReusesFrozenOccurrenceAfterInterruptionAndHonorsPauseAndRevocation(t *testing.T) {
	ctx := context.Background()
	raw, _ := json.Marshal(map[string]any{"native_generation": operationport.NativeGeneration{PackageID: 1, IntervalHours: 24, ActorID: 9}})
	strategies := &cycleStrategies{items: []operationport.Strategy{{Key: "daily", Status: "active", Version: 1, Definition: raw}}}
	store := &cycleStore{commands: map[string]RunConfirmCommand{}, done: map[string]bool{}, failCompletion: true}
	runtime := &cycleRuntime{receipts: map[string]RunConfirmCommand{}}
	active := true
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s := &CycleGeneration{UOW: agentTestUOW{}, Strategies: strategies, Store: store, Runtime: runtime, Agents: runtime, ActorActive: func(context.Context, int64) (bool, error) { return active, nil }, Now: func() time.Time { return now }}
	if s.ScanScheduled(ctx) == nil {
		t.Fatal("interruption was hidden")
	}
	if e := s.ScanScheduled(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.ScanScheduled(ctx); e != nil {
		t.Fatal(e)
	}
	if runtime.accepted != 1 || runtime.previewCalls != 1 {
		t.Fatalf("replay duplicated preview/run: %+v", runtime)
	}
	now = now.Add(24 * time.Hour)
	strategies.items[0].Status = "paused"
	_ = s.ScanScheduled(ctx)
	if runtime.accepted != 1 {
		t.Fatal("paused strategy ran")
	}
	strategies.items[0].Status = "active"
	active = false
	_ = s.ScanScheduled(ctx)
	if runtime.accepted != 1 {
		t.Fatal("revoked actor ran")
	}
	active = true
	_ = s.ScanScheduled(ctx)
	if runtime.accepted != 2 {
		t.Fatal("new intentional occurrence missing")
	}
}

package automation

import (
	"context"
	"errors"
	platformjobqueue "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/jobqueue"
	"github.com/riverqueue/river"
	"time"
)

type CycleGenerationArgs struct{}

func (CycleGenerationArgs) Kind() string { return "automation.cycle-generation.v1" }

type CycleGenerationWorker struct {
	river.WorkerDefaults[CycleGenerationArgs]
	Service interface{ ScanScheduled(context.Context) error }
}

func (w *CycleGenerationWorker) Work(ctx context.Context, _ *river.Job[CycleGenerationArgs]) error {
	if w.Service == nil {
		return errors.New("cycle generation unavailable")
	}
	return w.Service.ScanScheduled(ctx)
}
func CycleGenerationPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(time.Minute), func() (river.JobArgs, *river.InsertOpts) {
		return CycleGenerationArgs{}, &river.InsertOpts{Queue: platformjobqueue.OutboundQueue, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Minute}}
	}, &river.PeriodicJobOpts{ID: "standard-cycle-generation-v1", RunOnStart: true})
}

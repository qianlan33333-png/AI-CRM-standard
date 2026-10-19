package app

import (
	"context"
	"github.com/jackc/pgx/v5"
	q "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/jobqueue"
	pg "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/port"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"time"
)

type PurchaseMembershipJobArgs struct {
	CampaignID int64  `json:"campaign_id"`
	CustomerID int64  `json:"customer_id"`
	Cause      string `json:"cause"`
}

func (PurchaseMembershipJobArgs) Kind() string { return "referral.purchase-membership.v1" }

type RiverPurchaseMembershipEnqueuer struct{ Client *river.Client[pgx.Tx] }

func (e *RiverPurchaseMembershipEnqueuer) EnqueuePurchaseMembershipWithin(ctx context.Context, c, customer int64, cause string, at time.Time) error {
	if e == nil || e.Client == nil || c < 0 || customer < 0 || c == 0 && customer == 0 || cause == "" {
		return p.ErrUnavailable
	}
	tx, err := pg.RequireTransaction(ctx)
	if err != nil {
		return err
	}
	_, err = q.InsertTxWithOptions(ctx, e.Client, tx, PurchaseMembershipJobArgs{c, customer, cause}, river.InsertOpts{Queue: ReferralCampaignQueue, MaxAttempts: 24, ScheduledAt: at, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled}}})
	return err
}

type PurchaseMembershipWorker struct {
	river.WorkerDefaults[PurchaseMembershipJobArgs]
	Service *Service
}

func (w *PurchaseMembershipWorker) Work(ctx context.Context, j *river.Job[PurchaseMembershipJobArgs]) error {
	if w.Service == nil || j == nil {
		return p.ErrUnavailable
	}
	result, e := w.Service.ReconcilePurchaseMembers(ctx, j.Args.CampaignID, j.Args.CustomerID, true)
	if e == nil && result.Pending > 0 {
		return river.JobSnooze(time.Minute)
	}
	return e
}

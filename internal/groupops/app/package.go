package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

type taskPackageStore interface {
	TaskPackageByKey(context.Context, int64, [32]byte) (*p.TaskPackage, error)
	LatestTaskPackage(context.Context, int64) (*p.TaskPackage, error)
	InsertTaskPackage(context.Context, p.TaskPackage, int64, [32]byte) (p.TaskPackage, error)
}

func (s *NavigationService) ReadTaskPackage(ctx context.Context, actor p.SidebarActor, plan int64) (view p.TaskPackageView, err error) {
	store, ok := s.Store.(taskPackageStore)
	if !s.ready() || !ok {
		return view, ErrUnavailable
	}
	err = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		d, e := s.Plans.store.Get(tx, plan)
		if e != nil {
			return e
		}
		if e = s.owner(d, actor); e != nil {
			return e
		}
		view.Package, e = store.LatestTaskPackage(tx, plan)
		return e
	})
	return
}

func (s *NavigationService) SaveTaskPackage(ctx context.Context, actor p.SidebarActor, markdown, key string) (out p.TaskPackage, err error) {
	store, ok := s.Store.(taskPackageStore)
	if !s.ready() || !ok || s.Sender == nil {
		return out, ErrUnavailable
	}
	doc, e := p.ParseTaskMarkdown(markdown)
	if e != nil || !validKey(key) {
		return out, ErrInvalid
	}
	if e = validateMessages(doc.Messages); e != nil {
		return out, e
	}
	d, e := s.Plans.Detail(ctx, doc.PlanID)
	if e != nil {
		return out, e
	}
	if e = s.owner(d, actor); e != nil {
		return out, e
	}
	digest := sha256.Sum256([]byte(markdown))
	digestText := hex.EncodeToString(digest[:])
	keyHash := sha256.Sum256([]byte(key))
	// A lost reply may be replayed without another Provider read or a new task.
	var previous *p.TaskPackage
	err = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		var e error
		previous, e = store.TaskPackageByKey(tx, actor.StaffID, keyHash)
		return e
	})
	if err != nil {
		return out, err
	}
	if previous != nil {
		if previous.PlanID != doc.PlanID || previous.SHA256 != digestText {
			return out, ErrConflict
		}
		return *previous, nil
	}
	if d.Plan.Revision != doc.PlanRevision || len(d.GroupAssets) != len(doc.Groups) {
		return out, ErrConflict
	}
	targets := make([]p.NavigationTarget, 0, len(doc.Groups))
	for index, g := range d.GroupAssets {
		if g.AssetRef != doc.Groups[index] {
			return out, ErrConflict
		}
		if e = s.checkGroup(ctx, actor, g.AssetRef); e != nil {
			return out, e
		}
		targets = append(targets, p.NavigationTarget{AssetID: g.ID, ChatID: g.AssetRef, Name: g.DisplayName})
	}
	err = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		locked, e := s.Plans.store.Lock(tx, doc.PlanID)
		if e != nil {
			return e
		}
		if e = s.owner(locked, actor); e != nil {
			return e
		}
		if locked.Plan.Revision != doc.PlanRevision {
			return ErrConflict
		}
		previous, e = store.TaskPackageByKey(tx, actor.StaffID, keyHash)
		if e != nil {
			return e
		}
		if previous != nil {
			if previous.PlanID != doc.PlanID || previous.SHA256 != digestText {
				return ErrConflict
			}
			out = *previous
			return nil
		}
		active, e := s.Store.LatestNavigation(tx, actor.StaffID)
		if e != nil {
			return e
		}
		if active != nil && active.State != "completed" && active.State != "cancelled" {
			current, e := s.Store.LockNavigation(tx, active.ID)
			if e != nil {
				return e
			}
			// Replace only a run that cannot yet have submitted any message.
			if current.SenderUserID != actor.WeComUserID || current.State == "granted" || current.State == "outcome_unknown" || current.State == "final_failed" || (current.Sequence != nil && (current.Sequence.IntentID != 0 || len(current.Sequence.Receipts) > 0)) {
				return ErrStateConflict
			}
			old := current.Revision
			current.State = "cancelled"
			current.ResumeState = ""
			current.Revision++
			current.UpdatedAt = time.Now().UTC()
			if e = s.Store.SaveNavigation(tx, current, old); e != nil {
				return e
			}
		}
		now := time.Now().UTC()
		run, e := s.Store.CreateNavigation(tx, p.NavigationRun{PlanID: doc.PlanID, PlanName: locked.Plan.Name, PlanRevision: doc.PlanRevision, StaffID: actor.StaffID, SenderUserID: actor.WeComUserID, Revision: 1, State: "paused", ResumeState: "waiting", Targets: targets, Observations: []p.NavigationObservation{}, Sequence: &p.SequenceExecution{Messages: doc.Messages, Receipts: []p.SequenceReceipt{}}, CreatedAt: now, UpdatedAt: now}, keyHash)
		if e != nil {
			return e
		}
		out, e = store.InsertTaskPackage(tx, p.TaskPackage{PlanID: doc.PlanID, Markdown: markdown, SHA256: digestText, Run: run, CreatedAt: now}, actor.StaffID, keyHash)
		if e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"plan_id": doc.PlanID, "package_id": out.ID, "run_id": run.ID, "operation": "rpa_package_saved", "actor": actor.StaffID})
		_, e = s.Plans.events.Append(tx, p.Event{Type: p.EvGroupOpsPlanUpdated, Payload: payload, OccurredAt: now, IdempotencyKey: "rpa-package-" + hex.EncodeToString(keyHash[:])})
		return e
	})
	return
}

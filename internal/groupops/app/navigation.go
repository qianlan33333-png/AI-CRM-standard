package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	outbound "github.com/qianlan33333-png/AI-CRM-standard/internal/outbound/port"
)

type NavigationStore interface {
	NavigationPlans(context.Context, int64) ([]p.SidebarPlan, error)
	LatestNavigation(context.Context, int64) (*p.NavigationRun, error)
	NavigationByKey(context.Context, int64, [32]byte) (*p.NavigationRun, error)
	LockNavigation(context.Context, int64) (p.NavigationRun, error)
	CreateNavigation(context.Context, p.NavigationRun, [32]byte) (p.NavigationRun, error)
	SaveNavigation(context.Context, p.NavigationRun, int64) error
}
type NavigationService struct {
	Plans   *Service
	Catalog p.Catalog
	Store   NavigationStore
	Sender  outbound.ConversationSequenceSender
}

func (s *NavigationService) ready() bool {
	return s != nil && s.Plans != nil && s.Catalog != nil && s.Store != nil && ready(s.Plans)
}
func (s *NavigationService) owner(d p.Detail, a p.SidebarActor) error {
	if a.StaffID < 1 || a.WeComUserID == "" || len(d.Members) != 1 || d.Members[0].StaffID != a.StaffID {
		return ErrSidebarForbidden
	}
	if d.Plan.Status == p.PlanArchived {
		return ErrStateConflict
	}
	return nil
}
func (s *NavigationService) ReadNavigation(ctx context.Context, a p.SidebarActor) (v p.NavigationView, err error) {
	if !s.ready() {
		return v, ErrUnavailable
	}
	if a.StaffID < 1 || a.WeComUserID == "" {
		return v, ErrSidebarForbidden
	}
	err = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		var e error
		v.Plans, e = s.Store.NavigationPlans(tx, a.StaffID)
		if e != nil {
			return e
		}
		v.Run, e = s.Store.LatestNavigation(tx, a.StaffID)
		if e != nil {
			return e
		}
		if v.Run != nil {
			if v.Run.SenderUserID != a.WeComUserID {
				return ErrSidebarForbidden
			}
			if v.Run.Sequence != nil {
				if e = s.syncSequenceWithin(tx, v.Run, v.Run.Sequence.DeviceID); e != nil {
					return e
				}
			}
			d, e := s.Plans.store.Get(tx, v.Run.PlanID)
			if e != nil {
				return e
			}
			return s.owner(d, a)
		}
		return nil
	})
	return
}
func (s *NavigationService) checkGroup(ctx context.Context, a p.SidebarActor, id string) error {
	g, e := s.Catalog.RefreshCatalogGroup(ctx, id)
	if e != nil {
		return ErrUnavailable
	}
	if g.ChatID != id || g.State != "ready" || g.ObservedAt == nil {
		return ErrUnavailable
	}
	if g.OwnerStaffID != a.StaffID || g.OwnerUserID != a.WeComUserID {
		return ErrSidebarForbidden
	}
	return nil
}
func (s *NavigationService) MutateNavigation(ctx context.Context, a p.SidebarActor, c p.NavigationCommand) (out p.NavigationRun, err error) {
	if !s.ready() {
		return out, ErrUnavailable
	}
	if a.StaffID < 1 || a.WeComUserID == "" {
		return out, ErrSidebarForbidden
	}
	if c.Action == "start" {
		return s.start(ctx, a, c)
	}
	if c.RunID < 1 || c.Revision < 1 {
		return out, ErrInvalid
	}
	// Read before Provider I/O, then lock and compare the exact revision again.
	if c.Action == "open" {
		var r p.NavigationRun
		err = s.Plans.uow.Within(ctx, func(tx context.Context) error { var e error; r, e = s.Store.LockNavigation(tx, c.RunID); return e })
		if err != nil {
			return out, err
		}
		if r.StaffID != a.StaffID || r.SenderUserID != a.WeComUserID {
			return out, ErrSidebarForbidden
		}
		if r.Revision != c.Revision {
			return out, ErrConflict
		}
		if r.Cursor >= len(r.Targets) || (r.State != "waiting" && r.State != "open_requested" && !(r.Sequence != nil && r.State == "verified" && r.Sequence.IntentID == 0)) {
			return out, ErrStateConflict
		}
		if err = s.checkGroup(ctx, a, r.Targets[r.Cursor].ChatID); err != nil {
			return out, err
		}
	}
	err = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		r, e := s.Store.LockNavigation(tx, c.RunID)
		if e != nil {
			return e
		}
		if r.StaffID != a.StaffID || r.SenderUserID != a.WeComUserID {
			return ErrSidebarForbidden
		}
		d, e := s.Plans.store.Lock(tx, r.PlanID)
		if e != nil {
			return e
		}
		if e = s.owner(d, a); e != nil {
			return e
		}
		// Lost confirmation responses may be read back/replayed, but never skip a step.
		if c.Action == "confirm" && r.Sequence == nil && c.Step >= 0 && c.Step < r.Cursor && c.Step < len(r.Targets) && r.Targets[c.Step].ChatID == c.ChatID {
			out = r
			return nil
		}
		if r.Revision != c.Revision {
			return ErrConflict
		}
		old := r.Revision
		switch c.Action {
		case "open":
			if r.Cursor >= len(r.Targets) || (r.State != "waiting" && r.State != "open_requested" && !(r.Sequence != nil && r.State == "verified" && r.Sequence.IntentID == 0)) {
				return ErrStateConflict
			}
			r.State = "open_requested"
			if r.Sequence != nil {
				r.Sequence.VerifiedAt = time.Time{}
			}
		case "confirm":
			if (r.State != "open_requested" && !(r.Sequence != nil && r.State == "verified")) || c.Step != r.Cursor || r.Cursor >= len(r.Targets) || r.Targets[r.Cursor].ChatID != c.ChatID {
				return ErrStateConflict
			}
			if r.Sequence != nil && (r.Sequence.AccountAfter.IsZero() || c.AccountStaffID != a.StaffID || c.AccountSenderUserID != a.WeComUserID || c.AccountIssuedAt.Before(r.Sequence.AccountAfter)) {
				return ErrSidebarForbidden
			}
			r.Observations = append(r.Observations, p.NavigationObservation{Step: r.Cursor, ChatID: c.ChatID, ObservedAt: time.Now().UTC()})
			if r.Sequence != nil {
				r.Sequence.AccountVerifiedAt = c.AccountIssuedAt
				r.Sequence.VerifiedAt = time.Now().UTC()
				r.State = "verified"
				break
			}
			r.Cursor++
			r.State = "waiting"
			if r.Cursor == len(r.Targets) {
				r.State = "completed"
			}
		case "pause":
			if r.State != "waiting" && r.State != "open_requested" && !(r.Sequence != nil && (r.State == "verified" || r.State == "granted")) {
				return ErrStateConflict
			}
			r.ResumeState = r.State
			r.State = "paused"
		case "resume":
			if r.State != "paused" {
				return ErrStateConflict
			}
			r.State = r.ResumeState
			r.ResumeState = ""
		case "cancel":
			if r.Sequence != nil && (r.State == "granted" || r.State == "outcome_unknown" || (r.State == "paused" && r.ResumeState == "granted")) {
				return ErrStateConflict
			}
			if r.State == "completed" || r.State == "cancelled" {
				return ErrStateConflict
			}
			r.State = "cancelled"
		default:
			return ErrInvalid
		}
		r.Revision++
		r.UpdatedAt = time.Now().UTC()
		if e = s.Store.SaveNavigation(tx, r, old); e != nil {
			return e
		}
		out = r
		return nil
	})
	return
}
func (s *NavigationService) start(ctx context.Context, a p.SidebarActor, c p.NavigationCommand) (out p.NavigationRun, err error) {
	if c.PlanID < 1 || c.PlanRevision < 1 || !validKey(c.Key) || (c.Mode != "" && c.Mode != "rpa") {
		return out, ErrInvalid
	}
	key := sha256.Sum256([]byte(c.Key))
	var previous *p.NavigationRun
	err = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		var e error
		previous, e = s.Store.NavigationByKey(tx, a.StaffID, key)
		return e
	})
	if err != nil {
		return out, err
	}
	if previous != nil {
		if (previous.Sequence != nil) != (c.Mode == "rpa") {
			return out, ErrConflict
		}
		if previous.PlanID != c.PlanID || previous.PlanRevision != c.PlanRevision {
			return out, ErrConflict
		}
		if previous.SenderUserID != a.WeComUserID {
			return out, ErrSidebarForbidden
		}
		d, e := s.Plans.Detail(ctx, previous.PlanID)
		if e != nil {
			return out, e
		}
		if e = s.owner(d, a); e != nil {
			return out, e
		}
		return *previous, nil
	}
	d, err := s.Plans.Detail(ctx, c.PlanID)
	if err != nil {
		return out, err
	}
	if err = s.owner(d, a); err != nil {
		return out, err
	}
	if d.Plan.Revision != c.PlanRevision {
		return out, ErrConflict
	}
	if len(d.GroupAssets) == 0 {
		return out, ErrInvalid
	}
	targets := make([]p.NavigationTarget, 0, len(d.GroupAssets))
	for _, g := range d.GroupAssets {
		if err = s.checkGroup(ctx, a, g.AssetRef); err != nil {
			return out, err
		}
		targets = append(targets, p.NavigationTarget{AssetID: g.ID, ChatID: g.AssetRef, Name: g.DisplayName})
	}
	err = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		latest, e := s.Plans.store.Lock(tx, c.PlanID)
		if e != nil {
			return e
		}
		if e = s.owner(latest, a); e != nil {
			return e
		}
		if latest.Plan.Revision != c.PlanRevision {
			return ErrConflict
		}
		previous, e = s.Store.NavigationByKey(tx, a.StaffID, key)
		if e != nil {
			return e
		}
		if previous != nil {
			if (previous.Sequence != nil) != (c.Mode == "rpa") {
				return ErrConflict
			}
			if previous.PlanID != c.PlanID || previous.PlanRevision != c.PlanRevision {
				return ErrConflict
			}
			out = *previous
			return nil
		}
		now := time.Now().UTC()
		var sequence *p.SequenceExecution
		if c.Mode == "rpa" {
			store, ok := s.Store.(sequenceStore)
			if !ok || s.Sender == nil {
				return ErrUnavailable
			}
			messages, e := store.ReadSequenceMessages(tx, c.PlanID)
			if e != nil {
				return e
			}
			if e = validateMessages(messages); e != nil {
				return e
			}
			sequence = &p.SequenceExecution{Messages: messages, Receipts: []p.SequenceReceipt{}}
		}
		out, e = s.Store.CreateNavigation(tx, p.NavigationRun{PlanID: d.Plan.ID, PlanName: d.Plan.Name, PlanRevision: d.Plan.Revision, StaffID: a.StaffID, SenderUserID: a.WeComUserID, Revision: 1, State: "paused", ResumeState: "waiting", Targets: targets, Observations: []p.NavigationObservation{}, Sequence: sequence, CreatedAt: now, UpdatedAt: now}, key)
		return e
	})
	if errors.Is(err, ErrConflict) {
		return p.NavigationRun{}, ErrConflict
	}
	return
}

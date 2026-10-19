package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	out "github.com/qianlan33333-png/AI-CRM-standard/internal/outbound/port"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

type sequenceStore interface {
	SequenceAccountTime(context.Context) (time.Time, error)
	ReadSequenceMessages(context.Context, int64) ([]p.SequenceMessage, error)
	SaveSequenceMessages(context.Context, int64, []p.SequenceMessage) error
}

func validateMessages(messages []p.SequenceMessage) error {
	if len(messages) == 0 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, m := range messages {
		if m.ID == "" || seen[m.ID] || !utf8.ValidString(m.ID) || strings.ContainsAny(m.ID, "\x00\r\n") {
			return ErrInvalid
		}
		seen[m.ID] = true
		switch m.Kind {
		case "text":
			if strings.TrimSpace(m.Text) == "" || !utf8.ValidString(m.Text) || strings.ContainsRune(m.Text, 0) || m.RelativePath != "" || m.SHA256 != "" || m.ByteCount != 0 {
				return ErrInvalid
			}
		case "image", "file":
			h, e := hex.DecodeString(m.SHA256)
			if e != nil || len(h) != 32 || m.ByteCount < 1 || m.Text != "" || m.RelativePath == "" || path.IsAbs(m.RelativePath) || path.Clean(m.RelativePath) != m.RelativePath || strings.HasPrefix(m.RelativePath, "../") || m.RelativePath == ".." || strings.ContainsAny(m.RelativePath, "\x00\\") {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}
func (s *NavigationService) ReadSequenceConfig(ctx context.Context, a p.SidebarActor, id int64) (out p.SequenceConfig, e error) {
	store, ok := s.Store.(sequenceStore)
	if !s.ready() || !ok {
		return out, ErrUnavailable
	}
	e = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		d, e := s.Plans.store.Get(tx, id)
		if e != nil {
			return e
		}
		if e = s.owner(d, a); e != nil {
			return e
		}
		out = p.SequenceConfig{PlanID: id, Revision: d.Plan.Revision}
		out.Messages, e = store.ReadSequenceMessages(tx, id)
		return e
	})
	return
}
func (s *NavigationService) SaveSequenceConfig(ctx context.Context, a p.SidebarActor, c p.SequenceConfig) (out p.SequenceConfig, e error) {
	store, ok := s.Store.(sequenceStore)
	if !s.ready() || !ok {
		return out, ErrUnavailable
	}
	if c.PlanID < 1 || c.Revision < 1 || !validKey(c.Key) {
		return out, ErrInvalid
	}
	if e = validateMessages(c.Messages); e != nil {
		return out, e
	}
	current, e := s.Plans.Detail(ctx, c.PlanID)
	if e != nil {
		return out, e
	}
	if e = s.owner(current, a); e != nil {
		return out, e
	}
	saved, e := s.Plans.mutate(ctx, "rpa_sequence", c.PlanID, c.Revision, a.StaffID, c.Key, c, func(tx context.Context, d *p.Detail, _ time.Time) error {
		if e := s.owner(*d, a); e != nil {
			return e
		}
		return store.SaveSequenceMessages(tx, c.PlanID, c.Messages)
	})
	if e == nil {
		out = p.SequenceConfig{PlanID: c.PlanID, Revision: saved.Plan.Revision, Messages: c.Messages}
	}
	return
}
func sequenceProjection(r p.NavigationRun) *p.SequenceStep {
	if r.Sequence == nil {
		return nil
	}
	seq := r.Sequence
	step := &p.SequenceStep{RunID: r.ID, Revision: r.Revision, GroupIndex: r.Cursor, MessageIndex: seq.MessageIndex, SenderUserID: r.SenderUserID, State: r.State, IntentID: seq.IntentID}
	step.PlanName, step.GroupCount, step.MessageCount = r.PlanName, len(r.Targets), len(seq.Messages)
	step.Materials = []p.SequenceMessage{}
	for _, message := range seq.Messages {
		if message.Kind == "image" || message.Kind == "file" {
			step.Materials = append(step.Materials, message)
		}
	}
	step.Command = "SendConversationSequence"
	step.CanStart = seq.DeviceID == 0 && r.State == "paused" && r.ResumeState == "waiting"
	if r.Cursor < len(r.Targets) {
		step.ChatID = r.Targets[r.Cursor].ChatID
		step.GroupName = r.Targets[r.Cursor].Name
	}
	if seq.MessageIndex < len(seq.Messages) {
		message := seq.Messages[seq.MessageIndex]
		step.Message = &message
	}
	return step
}

func (s *NavigationService) projectSequence(ctx context.Context, r p.NavigationRun) (view p.SequenceStepView, err error) {
	view.Step = sequenceProjection(r)
	if view.Step == nil {
		return
	}
	reader, ok := s.Store.(interface {
		ReadSequencePackageIdentity(context.Context, int64, int64) (int64, string, error)
	})
	if ok {
		view.Step.PackageID, view.Step.PackageSHA256, err = reader.ReadSequencePackageIdentity(ctx, r.ID, r.StaffID)
	}
	return
}
func (s *NavigationService) syncSequenceWithin(ctx context.Context, r *p.NavigationRun, device int64) error {
	if r.Sequence == nil || s.Sender == nil {
		return nil
	}
	seq := r.Sequence
	if seq.DeviceID != 0 && device != seq.DeviceID {
		return ErrSidebarForbidden
	}
	if seq.IntentID > 0 && (r.State == "granted" || (r.State == "paused" && r.ResumeState == "granted")) {
		intent, e := s.Sender.ReadRPAWithin(ctx, seq.IntentID, seq.DeviceID, r.StaffID)
		if e != nil {
			return e
		}
		if intent.State == "outcome_unknown" || intent.State == "final_failed" {
			old := r.Revision
			r.State = intent.State
			r.ResumeState = ""
			r.Revision++
			r.UpdatedAt = time.Now().UTC()
			return s.Store.SaveNavigation(ctx, *r, old)
		}
	}
	return nil
}
func (s *NavigationService) ReadSequenceStep(ctx context.Context, a p.SidebarActor, device int64) (out p.SequenceStepView, e error) {
	if !s.ready() || s.Sender == nil || device < 1 {
		return out, ErrUnavailable
	}
	e = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		latest, e := s.Store.LatestNavigation(tx, a.StaffID)
		if e != nil || latest == nil {
			return e
		}
		r, e := s.Store.LockNavigation(tx, latest.ID)
		if e != nil {
			return e
		}
		if r.StaffID != a.StaffID || r.SenderUserID != a.WeComUserID {
			return ErrSidebarForbidden
		}
		d, e := s.Plans.store.Get(tx, r.PlanID)
		if e != nil {
			return e
		}
		if e = s.owner(d, a); e != nil {
			return e
		}
		if e = s.syncSequenceWithin(tx, &r, device); e != nil {
			return e
		}
		out, e = s.projectSequence(tx, r)
		return e
	})
	return
}
func (s *NavigationService) MutateSequence(ctx context.Context, a p.SidebarActor, c p.SequenceCommand) (outcome p.SequenceStepView, e error) {
	defer func() {
		if errors.Is(e, out.ErrConversationSequenceConflict) {
			e = ErrConflict
		}
	}()
	if !s.ready() || s.Sender == nil || c.DeviceID < 1 || c.RunID < 1 || c.Revision < 1 {
		return outcome, ErrInvalid
	}
	if c.Action == "prepare" {
		view, e := s.ReadSequenceStep(ctx, a, c.DeviceID)
		if e != nil {
			return outcome, e
		}
		if view.Step == nil || view.Step.RunID != c.RunID || view.Step.Revision != c.Revision {
			return outcome, ErrConflict
		}
		if e = s.checkGroup(ctx, a, view.Step.ChatID); e != nil {
			return outcome, e
		}
	}
	e = s.Plans.uow.Within(ctx, func(tx context.Context) error {
		r, e := s.Store.LockNavigation(tx, c.RunID)
		if e != nil {
			return e
		}
		if r.StaffID != a.StaffID || r.SenderUserID != a.WeComUserID || r.Sequence == nil {
			return ErrSidebarForbidden
		}
		d, e := s.Plans.store.Get(tx, r.PlanID)
		if e != nil {
			return e
		}
		if e = s.owner(d, a); e != nil {
			return e
		}
		seq := r.Sequence
		if seq.DeviceID != 0 && seq.DeviceID != c.DeviceID {
			return ErrSidebarForbidden
		}
		if c.Action == "complete" || c.Action == "reconcile" {
			// Replaying an acknowledged receipt never skips the next message/group.
			for _, receipt := range seq.Receipts {
				if receipt.GroupIndex == c.GroupIndex && receipt.MessageIndex == c.MessageIndex {
					command := out.RPAOutcomeCommand{IntentID: receipt.IntentID, DeviceID: c.DeviceID, StaffID: a.StaffID, Grant: c.Grant, Outcome: c.Outcome, EvidenceDigest: c.EvidenceDigest}
					if c.Action == "reconcile" {
						if receipt.State == "outcome_unknown" {
							break
						}
						_, e = s.Sender.ReconcileRPAWithin(tx, command)
					} else {
						_, e = s.Sender.CompleteRPAWithin(tx, command)
					}
					if e != nil {
						return e
					}
					outcome, e = s.projectSequence(tx, r)
					return e
				}
			}
		}
		if r.Revision != c.Revision || r.Cursor != c.GroupIndex || seq.MessageIndex != c.MessageIndex {
			return ErrConflict
		}
		old := r.Revision
		switch c.Action {
		case "verify":
			if r.State != "waiting" && r.State != "open_requested" && r.State != "verified" {
				return ErrStateConflict
			}
			clock, ok := s.Store.(sequenceStore)
			if !ok {
				return ErrUnavailable
			}
			after, e := clock.SequenceAccountTime(tx)
			if e != nil {
				return e
			}
			seq.AccountAfter = after.UTC()
			seq.AccountVerifiedAt = time.Time{}
			seq.VerifiedAt = time.Time{}
		case "resume":
			if r.State != "paused" || r.ResumeState == "outcome_unknown" {
				return ErrStateConflict
			}
			r.State = r.ResumeState
			seq.DeviceID = c.DeviceID
			r.ResumeState = ""
		case "pause":
			if r.State != "waiting" && r.State != "open_requested" && r.State != "verified" && r.State != "granted" {
				return ErrStateConflict
			}
			r.ResumeState = r.State
			r.State = "paused"
		case "prepare":
			if r.State != "verified" || r.Cursor >= len(r.Targets) || seq.MessageIndex >= len(seq.Messages) || seq.VerifiedAt.IsZero() || seq.AccountAfter.IsZero() || seq.AccountVerifiedAt.Before(seq.AccountAfter) {
				return ErrStateConflict
			}
			payload, _ := json.Marshal(seq.Messages[seq.MessageIndex])
			intent, e := s.Sender.AcceptRPAWithin(tx, out.RPAIntentCommand{RunID: r.ID, GroupIndex: r.Cursor, MessageIndex: seq.MessageIndex, DeviceID: c.DeviceID, StaffID: a.StaffID, SenderUserID: a.WeComUserID, ChatID: r.Targets[r.Cursor].ChatID, Payload: payload})
			if e != nil {
				return e
			}
			if intent.State != "queued" || intent.Grant == "" {
				return ErrStateConflict
			}
			seq.IntentID = intent.ID
			seq.DeviceID = c.DeviceID
			r.State = "granted"
			r.Revision++
			r.UpdatedAt = time.Now().UTC()
			if e = s.Store.SaveNavigation(tx, r, old); e != nil {
				return e
			}
			outcome, e = s.projectSequence(tx, r)
			if e != nil {
				return e
			}
			outcome.Step.Grant = intent.Grant
			return nil
		case "complete", "reconcile":
			paused := r.State == "paused" && r.ResumeState == "granted"
			reconcile := c.Action == "reconcile"
			if (!reconcile && r.State != "granted" && !paused) || (reconcile && r.State != "outcome_unknown" && !(r.State == "paused" && r.ResumeState == "outcome_unknown")) || seq.IntentID < 1 {
				return ErrStateConflict
			}
			command := out.RPAOutcomeCommand{IntentID: seq.IntentID, DeviceID: c.DeviceID, StaffID: a.StaffID, Grant: c.Grant, Outcome: c.Outcome, EvidenceDigest: c.EvidenceDigest}
			var result out.RPAIntent
			if reconcile {
				result, e = s.Sender.ReconcileRPAWithin(tx, command)
			} else {
				result, e = s.Sender.CompleteRPAWithin(tx, command)
			}
			if e != nil {
				return e
			}
			receipt := p.SequenceReceipt{GroupIndex: r.Cursor, MessageIndex: seq.MessageIndex, IntentID: seq.IntentID, State: result.State}
			replaced := false
			for i := range seq.Receipts {
				if seq.Receipts[i].IntentID == seq.IntentID {
					seq.Receipts[i] = receipt
					replaced = true
					break
				}
			}
			if !replaced {
				seq.Receipts = append(seq.Receipts, receipt)
			}
			r.State = result.State
			if result.State == "client_executed" {
				seq.MessageIndex++
				seq.IntentID = 0
				seq.VerifiedAt = time.Time{}
				r.State = "verified"
				if seq.MessageIndex == len(seq.Messages) {
					seq.AccountAfter = time.Time{}
					seq.AccountVerifiedAt = time.Time{}
					seq.MessageIndex = 0
					r.Cursor++
					r.State = "waiting"
					if r.Cursor == len(r.Targets) {
						r.State = "completed"
					}
				}
			}
			if (paused || reconcile) && r.State != "completed" && r.State != "outcome_unknown" && r.State != "final_failed" {
				r.ResumeState = r.State
				r.State = "paused"
			}
		default:
			return ErrInvalid
		}
		r.Revision++
		r.UpdatedAt = time.Now().UTC()
		if e = s.Store.SaveNavigation(tx, r, old); e != nil {
			return e
		}
		outcome, e = s.projectSequence(tx, r)
		return e
	})
	return
}

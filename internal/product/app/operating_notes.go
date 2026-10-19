package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"strings"
	"time"
)

type OperatingNotesStore interface {
	ReadOperatingNotesWithin(context.Context, productport.ID) (productport.OperatingNotes, error)
	SaveOperatingNotesWithin(context.Context, productport.SaveOperatingNotes, time.Time) (productport.OperatingNotes, error)
}

func (s *Service) ReadOperatingNotes(ctx context.Context, id productport.ID) (out productport.OperatingNotes, err error) {
	store, ok := s.store.(OperatingNotesStore)
	if !ok || id < 1 {
		return out, ErrInvalidProduct
	}
	err = s.uow.Within(ctx, func(tx context.Context) error {
		if _, e := s.store.Get(tx, id); e != nil {
			return e
		}
		var e error
		out, e = store.ReadOperatingNotesWithin(tx, id)
		return e
	})
	return out, classify(err)
}
func (s *Service) SaveOperatingNotes(ctx context.Context, c productport.SaveOperatingNotes) (out productport.OperatingNotes, err error) {
	c.Notes = strings.TrimSpace(c.Notes)
	store, ok := s.store.(OperatingNotesStore)
	if !ok || c.ProductID < 1 || c.ExpectedVersion < 0 || c.Actor < 1 || len(c.Notes) > 12000 || !validIdempotencyKey(c.IdempotencyKey) {
		return out, ErrInvalidProduct
	}
	raw, _ := json.Marshal(struct {
		ID      productport.ID
		Notes   string
		Version int64
	}{c.ProductID, c.Notes, c.ExpectedVersion})
	digest := sha256.Sum256(raw)
	key := sha256.Sum256([]byte("operating-notes:" + c.IdempotencyKey))
	now := s.now().UTC()
	reservation := Reservation{Operation: "update", ActorScope: fmt.Sprintf("admin:%d", c.Actor), KeyDigest: key, PayloadDigest: digest, CreatedAt: now}
	err = s.uow.Within(ctx, func(tx context.Context) error {
		receipt, owned, e := s.store.Reserve(tx, reservation)
		if e != nil {
			return e
		}
		if receipt.PayloadDigest != digest {
			return ErrConflict
		}
		if !owned {
			if receipt.State != "completed" {
				return ErrUnavailable
			}
			return json.Unmarshal(receipt.ResultSnapshot, &out)
		}
		if _, e = s.store.GetForUpdate(tx, c.ProductID); e != nil {
			return e
		}
		out, e = store.SaveOperatingNotesWithin(tx, c, now)
		if e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"product_id": c.ProductID, "notes_version": out.Version, "actor": c.Actor})
		if _, e = s.events.Append(tx, productport.Event{Type: productport.EventProductOperatingNotesUpdated, Payload: payload, OccurredAt: now, IdempotencyKey: "product.notes:" + hex.EncodeToString(key[:])}); e != nil {
			return e
		}
		snapshot, _ := json.Marshal(out)
		_, e = s.store.Complete(tx, receipt.ID, snapshot, now)
		return e
	})
	return out, classify(err)
}

package outbound

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	effects "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/outbound/port"
	postgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
	"strconv"
	"time"
)

var ErrRPAConflict = p.ErrConversationSequenceConflict

type ConversationSequenceService struct {
	Effects interface {
		effects.TransactionalAccepter
		effects.ClientCompleter
		ReconcileEffectWithin(context.Context, effects.ReconcileCommand) (effects.Projection, error)
	}
}

func (s *ConversationSequenceService) AcceptRPAWithin(ctx context.Context, c p.RPAIntentCommand) (out p.RPAIntent, err error) {
	if s.Effects == nil || c.RunID < 1 || c.DeviceID < 1 || c.StaffID < 1 || c.GroupIndex < 0 || c.MessageIndex < 0 || c.SenderUserID == "" || c.ChatID == "" || !json.Valid(c.Payload) {
		return out, ErrRPAConflict
	}
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return out, err
	}
	raw, _ := json.Marshal(c)
	digest := sha256.Sum256(raw)
	var prior []byte
	err = tx.QueryRow(ctx, `SELECT id,state,intent_digest,expires_at FROM outbound_rpa_intents WHERE run_ref=$1 AND group_index=$2 AND message_index=$3 FOR UPDATE`, c.RunID, c.GroupIndex, c.MessageIndex).Scan(&out.ID, &out.State, &prior, &out.ExpiresAt)
	if err == nil {
		if subtle.ConstantTimeCompare(prior, digest[:]) != 1 {
			return p.RPAIntent{}, ErrRPAConflict
		}
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return out, err
	}
	grant := base64.RawURLEncoding.EncodeToString(random[:])
	hash := sha256.Sum256([]byte(grant))
	now := time.Now().UTC()
	expires := now.Add(5 * time.Minute)
	err = tx.QueryRow(ctx, `INSERT INTO outbound_rpa_intents(run_ref,group_index,message_index,device_id,staff_id,intent_digest,payload,state,grant_digest,expires_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'accepted',$8,$9,$10,$10) RETURNING id`, c.RunID, c.GroupIndex, c.MessageIndex, c.DeviceID, c.StaffID, digest[:], c.Payload, hash[:], expires, now).Scan(&out.ID)
	if err != nil {
		return out, err
	}
	envelope := effects.Envelope{Owner: effects.OwnerOutbound, Kind: effects.KindRPAClientSend, SourceRefDigest: effects.Hash("rpa.intent", strconv.FormatInt(out.ID, 10)), TargetRefDigest: effects.Hash("rpa.target", c.ChatID, c.SenderUserID), PayloadDigest: effects.Hash("rpa.payload", string(c.Payload)), PolicyVersionHash: effects.Hash("rpa.policy.v1")}
	effect, _, err := s.Effects.AcceptAndQueueWithin(ctx, effects.AcceptCommand{ReceiptKey: effects.Hash("rpa.accept", strconv.FormatInt(out.ID, 10)), Envelope: envelope, ScheduledAt: expires.Add(time.Minute)})
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `UPDATE outbound_rpa_intents SET effect_id=$2,state='queued' WHERE id=$1`, out.ID, effect.ID)
	if err != nil {
		return out, err
	}
	if err = rpaEvent(ctx, out.ID, "accept", digest[:]); err != nil {
		return out, err
	}
	out.State = "queued"
	out.Grant = grant
	out.ExpiresAt = expires
	return
}
func (s *ConversationSequenceService) ReadRPAWithin(ctx context.Context, id, device, staff int64) (out p.RPAIntent, err error) {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT id,state,expires_at FROM outbound_rpa_intents WHERE id=$1 AND device_id=$2 AND staff_id=$3`, id, device, staff).Scan(&out.ID, &out.State, &out.ExpiresAt)
	// A lost grant/receipt can never become executable again, even if the
	// shared effects worker has not yet consumed its timeout job.
	if err == nil && out.State == "queued" && !time.Now().Before(out.ExpiresAt) {
		out.State = "outcome_unknown"
	}
	return
}
func (s *ConversationSequenceService) CompleteRPAWithin(ctx context.Context, c p.RPAOutcomeCommand) (out p.RPAIntent, err error) {
	if c.IntentID < 1 || c.DeviceID < 1 || c.StaffID < 1 || c.Grant == "" || c.EvidenceDigest == ([32]byte{}) || (c.Outcome != "client_executed" && c.Outcome != "outcome_unknown" && c.Outcome != "final_failed") {
		return out, ErrRPAConflict
	}
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return out, err
	}
	var grant, prior []byte
	var effect string
	err = tx.QueryRow(ctx, `SELECT id,state,expires_at,grant_digest,COALESCE(evidence_digest,''::bytea),effect_id FROM outbound_rpa_intents WHERE id=$1 AND device_id=$2 AND staff_id=$3 FOR UPDATE`, c.IntentID, c.DeviceID, c.StaffID).Scan(&out.ID, &out.State, &out.ExpiresAt, &grant, &prior, &effect)
	if err != nil {
		return out, err
	}
	hashed := sha256.Sum256([]byte(c.Grant))
	if subtle.ConstantTimeCompare(grant, hashed[:]) != 1 {
		return out, ErrRPAConflict
	}
	if out.State != "queued" {
		if out.State == c.Outcome && subtle.ConstantTimeCompare(prior, c.EvidenceDigest[:]) == 1 {
			return out, nil
		}
		return out, ErrRPAConflict
	}
	if !time.Now().Before(out.ExpiresAt) && c.Outcome != "outcome_unknown" {
		return out, ErrRPAConflict
	}
	state := effects.StateExecuted
	if c.Outcome == "outcome_unknown" {
		state = effects.StateUnknown
	}
	if c.Outcome == "final_failed" {
		state = effects.StateFinalFailed
	}
	if _, err = s.Effects.CompleteClientEffectWithin(ctx, effects.ClientCompletionCommand{EffectID: effect, ReceiptKey: effects.Hash("rpa.complete", string(hashed[:])), EvidenceDigest: effects.Digest("sha256:" + hex.EncodeToString(c.EvidenceDigest[:])), State: state}); err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `UPDATE outbound_rpa_intents SET state=$2,evidence_digest=$3,updated_at=clock_timestamp() WHERE id=$1`, out.ID, c.Outcome, c.EvidenceDigest[:])
	if err != nil {
		return out, err
	}
	err = rpaEvent(ctx, out.ID, "complete", c.EvidenceDigest[:])
	out.State = c.Outcome
	return
}
func (s *ConversationSequenceService) ReconcileRPAWithin(ctx context.Context, c p.RPAOutcomeCommand) (out p.RPAIntent, err error) {
	if s.Effects == nil || c.IntentID < 1 || c.DeviceID < 1 || c.StaffID < 1 || c.Grant == "" || c.Outcome != "client_executed" || c.EvidenceDigest == ([32]byte{}) {
		return out, ErrRPAConflict
	}
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return out, err
	}
	var grant, prior []byte
	var effect string
	var reconciled bool
	err = tx.QueryRow(ctx, `SELECT id,state,expires_at,grant_digest,COALESCE(evidence_digest,''::bytea),effect_id,EXISTS(SELECT 1 FROM outbound_rpa_events WHERE intent_id=$1 AND operation='reconcile') FROM outbound_rpa_intents WHERE id=$1 AND device_id=$2 AND staff_id=$3 FOR UPDATE`, c.IntentID, c.DeviceID, c.StaffID).Scan(&out.ID, &out.State, &out.ExpiresAt, &grant, &prior, &effect, &reconciled)
	if err != nil {
		return out, err
	}
	hashed := sha256.Sum256([]byte(c.Grant))
	if subtle.ConstantTimeCompare(grant, hashed[:]) != 1 {
		return out, ErrRPAConflict
	}
	if out.State == "client_executed" && reconciled && subtle.ConstantTimeCompare(prior, c.EvidenceDigest[:]) == 1 {
		return out, nil
	}
	if out.State != "outcome_unknown" {
		return out, ErrRPAConflict
	}
	// Client submissions use their original one-shot grant, not a Provider
	// lease. The existing EER manual reconciliation locks the original unknown
	// attempt (including fence=0 client completions); it schedules no retry.
	if _, err = s.Effects.ReconcileEffectWithin(ctx, effects.ReconcileCommand{EffectID: effect, ReceiptKey: effects.Hash("rpa.reconcile", string(hashed[:])), EvidenceDigest: effects.Digest("sha256:" + hex.EncodeToString(c.EvidenceDigest[:])), ActorAdminUserID: c.StaffID}); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE outbound_rpa_intents SET state='client_executed',evidence_digest=$2,updated_at=clock_timestamp() WHERE id=$1`, out.ID, c.EvidenceDigest[:]); err != nil {
		return out, err
	}
	if err = rpaEvent(ctx, out.ID, "reconcile", c.EvidenceDigest[:]); err != nil {
		return out, err
	}
	out.State = "client_executed"
	return out, nil
}

func rpaEvent(ctx context.Context, id int64, operation string, digest []byte) error {
	tx, e := postgres.RequireTransaction(ctx)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO outbound_rpa_events(intent_id,operation,payload_digest,occurred_at) VALUES($1,$2,$3,clock_timestamp())`, id, operation, digest)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO outbound_rpa_outbox(intent_id,event_type,payload,occurred_at) VALUES($1,$2,jsonb_build_object('intent_id',$1::bigint),clock_timestamp())`, id, "outbound.rpa."+operation+".v1")
	return e
}
func expireRPAWithin(ctx context.Context, effect string, digest effects.Digest) error {
	tx, e := postgres.RequireTransaction(ctx)
	if e != nil {
		return e
	}
	var id int64
	e = tx.QueryRow(ctx, `UPDATE outbound_rpa_intents SET state='outcome_unknown',updated_at=clock_timestamp() WHERE effect_id=$1 AND state='queued' RETURNING id`, effect).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	hash := sha256.Sum256([]byte(digest))
	return rpaEvent(ctx, id, "expire", hash[:])
}

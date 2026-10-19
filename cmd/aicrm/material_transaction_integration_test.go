package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	channel "github.com/qianlan33333-png/AI-CRM-standard/internal/channel"
	channelport "github.com/qianlan33333-png/AI-CRM-standard/internal/channel/port"
	externaleffects "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects"
	effectport "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
	groupopsport "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/media/groupopsmaterial"
	mediaport "github.com/qianlan33333-png/AI-CRM-standard/internal/media/port"
	mediastore "github.com/qianlan33333-png/AI-CRM-standard/internal/media/store"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/outbound"
	outboundport "github.com/qianlan33333-png/AI-CRM-standard/internal/outbound/port"
	platformjobqueue "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/jobqueue"
	platformpostgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/platform/webhook"
	wecom "github.com/qianlan33333-png/AI-CRM-standard/internal/wecom"
	"github.com/riverqueue/river"
)

type materialTransactionFixture struct {
	ctx         context.Context
	native      *pgxpool.Pool
	unit        *platformpostgres.UnitOfWork
	media       *mediastore.Repository
	effects     *externaleffects.Repository
	preparation *outbound.MaterialPreparationService
	resolver    groupopsport.MaterialSnapshotResolver
	actor       int64
	scope       string
}

func newMaterialTransactionFixture(t *testing.T) *materialTransactionFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	dsn, cleanup := adminAccessCompositionDatabase(t, ctx)
	t.Cleanup(cleanup)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Every port must finish on the one connection already owned by the UoW.
	cfg.MaxConns, cfg.MinConns = 1, 0
	native, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.Close)
	pool, err := platformpostgres.Wrap(native, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := platformpostgres.NewUnitOfWork(pool)
	if err != nil {
		t.Fatal(err)
	}
	actor := surveyJourneyGovernedActor(t, ctx, native, "material-transaction", "Material transaction")
	return bindMaterialTransactionPorts(t, ctx, native, unit, actor, string(effectport.Hash("material-transaction-fixture")))
}

func bindMaterialTransactionPorts(t *testing.T, ctx context.Context, native *pgxpool.Pool, unit *platformpostgres.UnitOfWork, actor int64, scope string) *materialTransactionFixture {
	t.Helper()
	media, err := mediastore.NewPostgreSQL(native, unit)
	if err != nil {
		t.Fatal(err)
	}
	workers := river.NewWorkers()
	if err = river.AddWorkerSafely[externaleffects.EffectJobArgs](workers, externaleffects.NewWorker(nil, nil)); err != nil {
		t.Fatal(err)
	}
	insert, err := platformjobqueue.NewInsertClient(native, workers)
	if err != nil {
		t.Fatal(err)
	}
	effects, err := externaleffects.NewRepository(native, insert)
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := outbound.NewMaterialPreparationService(unit, effects, native, media)
	if err != nil {
		t.Fatal(err)
	}
	freezer, err := groupopsmaterial.NewFreezer(mediaPreparedPlanReader{sources: media, preparer: preparation, scopeDigest: scope})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := newUnifiedGroupOpsMaterialAdapter(media, freezer, media, preparation, scope)
	if err != nil {
		t.Fatal(err)
	}
	return &materialTransactionFixture{ctx, native, unit, media, effects, preparation, resolver, actor, scope}
}

func materialTransactionPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func (f *materialTransactionFixture) image(t *testing.T, name string) int64 {
	t.Helper()
	item, err := f.media.CreateImage(f.ctx, f.actor, "material-transaction-image-"+name, mediastore.ImageInput{
		FileName: name + ".png", MIME: "image/png", Name: name, Content: materialTransactionPNG(t), Width: 2, Height: 2, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item["id"].(int64)
}

// This uploader is the only synthetic Provider boundary. All source capture,
// task acceptance, envelope validation and completion use production ports.
type materialTransactionUploader struct{ now time.Time }

func (u materialTransactionUploader) UploadMaterial(ctx context.Context, source outboundport.MaterialSourceSnapshot, content outboundport.MaterialSourceContent, scope string) (outboundport.MaterialUploadReceipt, bool, error) {
	if _, err := platformpostgres.RequireTransaction(ctx); !errors.Is(err, platformpostgres.ErrTransactionNeeded) {
		return outboundport.MaterialUploadReceipt{}, false, errors.New("upload retained a database transaction")
	}
	if len(content.Bytes) == 0 {
		return outboundport.MaterialUploadReceipt{}, false, errors.New("missing captured bytes")
	}
	return outboundport.MaterialUploadReceipt{MediaID: "synthetic-media-" + source.SourceRef, ProviderCreatedAt: u.now}, true, nil
}

func (f *materialTransactionFixture) prepare(t *testing.T, ref string, now time.Time) outboundport.MaterialSourceSnapshot {
	t.Helper()
	source, err := f.media.GetSourceSnapshot(f.ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := f.preparation.Prepare(f.ctx, outboundport.MaterialRequest{MaterialSourceSnapshot: source, CorpScopeDigest: f.scope})
	if err != nil {
		t.Fatal(err)
	}
	var envelope effectport.Envelope
	if err = f.native.QueryRow(f.ctx, `SELECT owner,kind,source_ref_digest,target_ref_digest,payload_digest,policy_version_hash FROM external_effects WHERE id=$1`, effectIDNumber(t, accepted.EffectID)).Scan(&envelope.Owner, &envelope.Kind, &envelope.SourceRefDigest, &envelope.TargetRefDigest, &envelope.PayloadDigest, &envelope.PolicyVersionHash); err != nil {
		t.Fatal(err)
	}
	provider, err := outbound.NewMaterialPreparationProvider(f.preparation, materialTransactionUploader{now})
	if err != nil {
		t.Fatal(err)
	}
	attempt := effectport.Attempt{EffectID: accepted.EffectID, Number: 1, Generation: 1, Fence: 1}
	result, err := provider.Execute(f.ctx, envelope, attempt)
	if err != nil || result.Completion != effectport.StateExecuted {
		t.Fatalf("upload completion=%s err=%v", result.Completion, err)
	}
	if err = f.unit.Within(f.ctx, func(tx context.Context) error {
		return f.preparation.CompleteEffect(tx, accepted.EffectID, envelope, attempt, result)
	}); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestMaterialSourceSnapshotJoinsTransactionAndRollsBackPostgreSQL(t *testing.T) {
	f := newMaterialTransactionFixture(t)
	ref := "image:" + strconv.FormatInt(f.image(t, "source"), 10)
	rollback := errors.New("injected caller rollback")
	err := f.unit.Within(f.ctx, func(tx context.Context) error {
		if _, err := f.media.GetSourceSnapshot(tx, ref); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("caller transaction did not reach rollback: %v", err)
	}
	var count int
	if err = f.native.QueryRow(f.ctx, `SELECT count(*) FROM media_material_source_snapshots WHERE source_ref=$1`, ref).Scan(&count); err != nil || count != 0 {
		t.Fatalf("snapshot escaped rollback count=%d err=%v", count, err)
	}
	if _, err = f.media.GetSourceSnapshot(f.ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err = f.unit.Within(f.ctx, func(tx context.Context) error { return f.unit.Within(tx, func(context.Context) error { return nil }) }); !errors.Is(err, platformpostgres.ErrNestedTransaction) {
		t.Fatalf("global nested guard weakened: %v", err)
	}
}

func TestMaterialStatusUsesCallerConnectionPostgreSQL(t *testing.T) {
	f := newMaterialTransactionFixture(t)
	source := f.prepare(t, "image:"+strconv.FormatInt(f.image(t, "status"), 10), time.Now().UTC())
	rollback := errors.New("rollback uncommitted marker")
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	err := f.unit.Within(ctx, func(txctx context.Context) error {
		tx, _ := platformpostgres.RequireTransaction(txctx)
		if _, err := tx.Exec(txctx, `UPDATE outbound_material_preparations SET failure_code='caller_transaction_marker' WHERE source_ref=$1`, source.SourceRef); err != nil {
			return err
		}
		status, err := f.preparation.GetMaterialStatus(txctx, source, f.scope)
		if err != nil {
			return err
		}
		if !status.CredentialUsable || status.FailureCode != "caller_transaction_marker" {
			return fmt.Errorf("did not observe caller writes: %+v", status)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("single-connection status blocked or missed caller writes: %v", err)
	}
	status, err := f.preparation.GetMaterialStatus(f.ctx, source, f.scope)
	if err != nil || status.FailureCode != "uploaded" {
		t.Fatalf("status did not roll back: %+v err=%v", status, err)
	}
}

func TestCapturedMaterialGraphUsesRealSourcesPostgreSQL(t *testing.T) {
	f := newMaterialTransactionFixture(t)
	imageID := f.image(t, "cover")
	f.prepare(t, "image:"+strconv.FormatInt(imageID, 10), time.Now().UTC())
	mini, err := f.media.CreateMiniProgram(f.ctx, f.actor, "material-transaction-mini", map[string]any{"name": "card", "app_id": "wx-synthetic", "page_path": "pages/card", "title": "Card", "thumb_image_id": float64(imageID)})
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := f.media.CreateAttachment(f.ctx, f.actor, "material-transaction-file", mediastore.AttachmentInput{FileName: "fixture.pdf", Name: "file", Content: []byte("%PDF-1.4\nsynthetic document"), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	f.prepare(t, "attachment:"+strconv.FormatInt(attachment["id"].(int64), 10), time.Now().UTC())
	invite, err := f.media.CreateGroupInvite(f.ctx, f.actor, "material-transaction-invite", map[string]any{"name": "invite", "title": "Invite", "join_url": "https://work.weixin.qq.com/gm/0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind string
		id   int64
		want string
	}{
		{"image", imageID, "image"}, {"attachment", attachment["id"].(int64), "file"}, {"miniprogram", mini["id"].(int64), "miniprogram"}, {"group_invite", invite["id"].(int64), "link"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			err := f.unit.Within(f.ctx, func(tx context.Context) error {
				raw, _, err := f.resolver.ResolveMaterialSnapshot(tx, groupopsport.MaterialPlan{References: []groupopsport.MaterialReference{{Kind: tc.kind, ID: tc.id}}}, time.Now().UTC().Add(20*time.Second))
				if err != nil {
					return err
				}
				var snap mediaport.GroupOpsMaterialSnapshot
				if json.Unmarshal(raw, &snap) != nil || len(snap.Attachments) != 1 || snap.Attachments[0].MsgType != tc.want {
					return fmt.Errorf("wrong captured snapshot: %s", raw)
				}
				if tc.kind != "group_invite" && snap.Attachments[0].MediaID == "" {
					return errors.New("missing Provider credential")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMaterialSnapshotRejectsUnreadySourcesPostgreSQL(t *testing.T) {
	for _, state := range []string{"missing", "expired", "disabled", "deleted", "draft_without_cover"} {
		t.Run(state, func(t *testing.T) {
			f := newMaterialTransactionFixture(t)
			id := f.image(t, state)
			ref := "image:" + strconv.FormatInt(id, 10)
			if state == "expired" {
				f.prepare(t, ref, time.Now().UTC().Add(-73*time.Hour))
			}
			if state == "disabled" || state == "deleted" {
				f.prepare(t, ref, time.Now().UTC())
				if state == "disabled" {
					if _, err := f.media.UpdateImage(f.ctx, id, f.actor, "disable-material-transaction-image", map[string]any{"enabled": false}); err != nil {
						t.Fatal(err)
					}
				} else if _, err := f.media.Delete(f.ctx, "image", id, f.actor, "delete-material-transaction-image"); err != nil {
					t.Fatal(err)
				}
			}
			kind := "image"
			if state == "draft_without_cover" {
				item, err := f.media.CreateMiniProgram(f.ctx, f.actor, "material-transaction-draft", map[string]any{"name": "draft", "app_id": "wx-synthetic", "page_path": "pages/draft", "title": "Draft"})
				if err != nil {
					t.Fatalf("valid local draft was prohibited: %v", err)
				}
				kind, id = "miniprogram", item["id"].(int64)
			}
			var before, after int
			if err := f.native.QueryRow(f.ctx, `SELECT count(*) FROM external_effects`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			err := f.unit.Within(f.ctx, func(tx context.Context) error {
				_, _, err := f.resolver.ResolveMaterialSnapshot(tx, groupopsport.MaterialPlan{References: []groupopsport.MaterialReference{{Kind: kind, ID: id}}}, time.Now().UTC().Add(20*time.Second))
				return err
			})
			if err == nil {
				t.Fatal("unready source accepted as sendable")
			}
			if err = f.native.QueryRow(f.ctx, `SELECT count(*) FROM external_effects`).Scan(&after); err != nil || before != after {
				t.Fatalf("query implicitly created an upload/send: %d -> %d err=%v", before, after, err)
			}
		})
	}
}

type materialWelcomeFailure struct {
	delegate channelport.CallbackWelcomeAccepter
}

func (a materialWelcomeFailure) AcceptCallbackWelcome(ctx context.Context, command channelport.CallbackWelcomeCommand) error {
	if err := a.delegate.AcceptCallbackWelcome(ctx, command); err != nil {
		return err
	}
	return errors.New("injected failure after material, intent and job acceptance")
}

func TestMaterialWelcomeAtomicAcceptanceAndConcurrentReplayPostgreSQL(t *testing.T) {
	f := newMaterialTransactionFixture(t)
	if id := f.image(t, "atomic"); id != 1 {
		t.Fatalf("isolated image=%d want=1", id)
	}
	f.prepare(t, "image:1", time.Now().UTC())
	states := channel.NewPostgreSQLStore()
	digester, err := wecom.NewHMACStateDigester([]byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatal(err)
	}
	ready := seedChannelWelcomeFixture(t, f.ctx, f.unit, states, digester, f.actor, "atomic", "material-atomic", true, 1, 0)
	cipher, err := wecom.NewWelcomeGrantCipher("material-transaction-grant-key-32-bytes")
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := webhook.NewService(webhook.NewPostgreSQLStore())
	if err != nil {
		t.Fatal(err)
	}
	actions := channel.NewEntrantActionStore(f.effects, channelWelcomeMaterialAdapter{resolver: f.resolver})
	dispatch := wecom.ExternalContactCallbackDispatcher{StateDigester: digester, Inbox: inbox, UOW: f.unit, WelcomeGrants: wecom.NewPostgreSQLWelcomeGrantStore(cipher), WelcomeActions: materialWelcomeFailure{actions}, States: states}
	input := wecom.DecryptedCallbackEvent{CorpID: "wx-corp", CallbackKey: mustChannelWelcomeKey(t, "wecom:external-contact:material-atomic"), Plaintext: channelWelcomePlaintext(ready.rawState), ReceivedAt: time.Now().UTC().Truncate(time.Microsecond)}
	counts := func() string {
		t.Helper()
		var result string
		if err := f.native.QueryRow(f.ctx, `SELECT json_build_array((SELECT count(*) FROM webhook_inbox),(SELECT count(*) FROM wecom_welcome_grants),(SELECT count(*) FROM channel_welcome_intents),(SELECT count(*) FROM external_effects),(SELECT count(*) FROM external_effect_operation_receipts),(SELECT count(*) FROM external_effect_jobs),(SELECT count(*) FROM river_job),(SELECT count(*) FROM media_material_source_snapshots),(SELECT count(*) FROM media_audit_events),(SELECT count(*) FROM media_outbox))::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := counts()
	if err = dispatch.DispatchDecryptedEvent(f.ctx, input); err == nil {
		t.Fatal("injected post-acceptance failure ignored")
	}
	if after := counts(); after != before {
		t.Fatalf("partial transaction escaped rollback: before=%s after=%s", before, after)
	}
	dispatch.WelcomeActions = actions
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- dispatch.DispatchDecryptedEvent(f.ctx, input) }()
	}
	wg.Wait()
	close(errs)
	for err = range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var intents, effects, jobs int
	var state string
	var deadline time.Time
	if err = f.native.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM channel_welcome_intents),(SELECT count(*) FROM external_effects WHERE kind='channel_welcome_message'),(SELECT count(*) FROM external_effect_jobs WHERE queue='outbound_welcome')`).Scan(&intents, &effects, &jobs); err != nil || intents != 1 || effects != 1 || jobs != 1 {
		t.Fatalf("replay duplicated acceptance intents=%d effects=%d jobs=%d err=%v", intents, effects, jobs, err)
	}
	if err = f.native.QueryRow(f.ctx, `SELECT state,send_deadline_at FROM channel_welcome_intents`).Scan(&state, &deadline); err != nil || state != "queued" || !deadline.Equal(input.ReceivedAt.Add(20*time.Second)) {
		t.Fatalf("state=%s deadline=%s err=%v", state, deadline, err)
	}
	// The frozen task keeps its accepted image even when a later config is
	// unavailable. Replaying the callback must not reread or extend it.
	if _, err = f.media.UpdateImage(f.ctx, 1, f.actor, "material-atomic-disable", map[string]any{"enabled": false}); err != nil {
		t.Fatal(err)
	}
	late := input
	late.ReceivedAt = late.ReceivedAt.Add(time.Minute)
	if err = dispatch.DispatchDecryptedEvent(f.ctx, late); err != nil {
		t.Fatal(err)
	}
	if err = f.native.QueryRow(f.ctx, `SELECT state,send_deadline_at FROM channel_welcome_intents`).Scan(&state, &deadline); err != nil || state != "queued" || !deadline.Equal(input.ReceivedAt.Add(20*time.Second)) {
		t.Fatal("replay mutated the frozen decision")
	}
}

func TestFrozenMiniProgramCoverIsNotReplacedPostgreSQL(t *testing.T) {
	f := newMaterialTransactionFixture(t)
	originalID := f.image(t, "original-cover")
	f.prepare(t, "image:"+strconv.FormatInt(originalID, 10), time.Now().UTC())
	changed := image.NewRGBA(image.Rect(0, 0, 2, 2))
	changed.Set(1, 1, color.RGBA{B: 255, A: 255})
	var changedPNG bytes.Buffer
	if err := png.Encode(&changedPNG, changed); err != nil {
		t.Fatal(err)
	}
	other, err := f.media.CreateImage(f.ctx, f.actor, "material-replacement-cover", mediastore.ImageInput{FileName: "changed.png", MIME: "image/png", Name: "Changed", Content: changedPNG.Bytes(), Width: 2, Height: 2, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	otherID := other["id"].(int64)
	f.prepare(t, "image:"+strconv.FormatInt(otherID, 10), time.Now().UTC())
	mini, err := f.media.CreateMiniProgram(f.ctx, f.actor, "material-frozen-mini", map[string]any{"name": "Frozen", "app_id": "wx-synthetic", "page_path": "pages/old", "title": "Old title", "thumb_image_id": float64(originalID)})
	if err != nil {
		t.Fatal(err)
	}
	miniID := mini["id"].(int64)
	var captured mediaport.GroupOpsMaterialSourceSnapshot
	if err = f.unit.Within(f.ctx, func(tx context.Context) error {
		var err error
		captured, err = f.media.CaptureGroupOpsMaterialSources(tx, mediaport.GroupOpsMaterialPlan{References: []mediaport.GroupOpsMaterialReference{{Kind: "miniprogram", ID: miniID}}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.media.UpdateMiniProgram(f.ctx, miniID, f.actor, "material-change-mini-cover", map[string]any{"thumb_image_id": float64(otherID), "title": "New title", "page_path": "pages/new"}); err != nil {
		t.Fatal(err)
	}
	adapter := f.resolver.(groupOpsMaterialAdapter)
	if err = f.unit.Within(f.ctx, func(tx context.Context) error {
		snapshot, err := adapter.freezer.FreezeGroupOpsMaterial(tx, captured, time.Now().UTC().Add(20*time.Second))
		if err != nil {
			return err
		}
		item := snapshot.Attachments[0]
		if item.MediaID != "synthetic-media-image:"+strconv.FormatInt(originalID, 10) || item.Title != "Old title" || item.PagePath != "pages/old" {
			return fmt.Errorf("frozen card silently used replacement: %+v", item)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.media.Delete(f.ctx, "image", originalID, f.actor, "material-delete-old-cover"); err != nil {
		t.Fatal(err)
	}
	if err = f.unit.Within(f.ctx, func(tx context.Context) error {
		_, err := adapter.freezer.FreezeGroupOpsMaterial(tx, captured, time.Now().UTC().Add(20*time.Second))
		return err
	}); err == nil {
		t.Fatal("deleted old source silently fell forward to the new cover")
	}
}

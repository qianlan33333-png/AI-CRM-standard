package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/qianlan33333-png/AI-CRM-standard/internal/messagearchive/app"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/messagearchive/domain"
	platformpostgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
	wecomport "github.com/qianlan33333-png/AI-CRM-standard/internal/wecom/port"
)

func TestPostgreSQLArchiveEmptyRecipientEvidenceAndCursorAtomicReplay(t *testing.T) {
	pool, cleanup := archiveIntegrationPool(t)
	defer cleanup()
	ctx := context.Background()
	wrapped, err := platformpostgres.Wrap(pool, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	uow, err := platformpostgres.NewUnitOfWork(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewPostgreSQL()
	now := time.Now().UTC()
	const scope = "wecom-corp:empty-recipient-fixture"
	var run int64
	if err = uow.Within(ctx, func(tx context.Context) error {
		var e error
		run, e = repo.StartRun(tx, app.SyncRun{CorpScope: scope, Trigger: "notify", StartedAt: now})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"msgid":"empty-fixture","action":"send","from":"employee","tolist":[""],"msgtype":"text","msgtime":1792060446730,"text":{"content":"原消息"}}`)
	message, err := app.NormalizeArchiveRecord(scope, wecomport.PlainArchiveRecord{Seq: 96313, MsgID: "empty-fixture", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	evidence, digest := domain.NewIssuePayload(raw)
	batch := app.Batch{CorpScope: scope, ExpectedCursor: 0, EndSeq: 96313, RunID: run, NotifyReceivedAt: now, Messages: []domain.Message{message}, Issues: []app.IngestIssue{{Seq: 96313, MsgID: "empty-fixture", Stage: "normalize", Reason: "empty_recipient_preserved", Payload: evidence, Digest: digest}}}
	abort := errors.New("fixture rollback")
	if err = uow.Within(ctx, func(tx context.Context) error {
		if _, e := repo.CommitBatch(tx, batch); e != nil {
			return e
		}
		return abort
	}); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	var messages, issues, cursor int64
	read := func() {
		t.Helper()
		if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM message_archive_messages),(SELECT count(*) FROM message_archive_ingest_issues),(SELECT last_seq FROM message_archive_sync_state WHERE corp_scope=$1)`, scope).Scan(&messages, &issues, &cursor); err != nil {
			t.Fatal(err)
		}
	}
	read()
	if messages != 0 || issues != 0 || cursor != 0 {
		t.Fatal("partial commit")
	}
	if err = uow.Within(ctx, func(tx context.Context) error { _, e := repo.CommitBatch(tx, batch); return e }); err != nil {
		t.Fatal(err)
	}
	read()
	if messages != 1 || issues != 1 || cursor != 96313 {
		t.Fatal("message evidence or cursor missing")
	}
	var payload json.RawMessage
	var protected []byte
	var emptyActors int
	if err = pool.QueryRow(ctx, `SELECT (SELECT provider_payload FROM message_archive_messages),(SELECT protected_payload FROM message_archive_ingest_issues),(SELECT count(*) FROM message_archive_participants WHERE provider_value='')`).Scan(&payload, &protected, &emptyActors); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(protected, raw) || sha256.Sum256(protected) != digest || emptyActors != 0 {
		t.Fatal("evidence lost or empty identity persisted")
	}
	var retained map[string]any
	if json.Unmarshal(payload, &retained) != nil || len(retained["tolist"].([]any)) != 1 || retained["tolist"].([]any)[0] != "" {
		t.Fatal("raw recipient fact changed")
	}
	if err = uow.Within(ctx, func(tx context.Context) error { _, e := repo.CommitBatch(tx, batch); return e }); !errors.Is(err, app.ErrCursorAdvanced) {
		t.Fatal("replay not fenced")
	}
	read()
	if messages != 1 || issues != 1 || cursor != 96313 {
		t.Fatal("replay duplicated facts")
	}
}

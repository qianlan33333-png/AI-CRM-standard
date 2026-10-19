package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"

	wecomport "github.com/qianlan33333-png/AI-CRM-standard/internal/wecom/port"
)

func TestArchiveEmptyRecipientRetainsMessageEvidenceWithoutInventingIdentity(t *testing.T) {
	raw := []byte(`{"msgid":"empty-recipient","action":"send","from":"employee","tolist":["","wm_customer",""],"roomid":"","msgtype":"text","msgtime":1792060446730,"text":{"content":"原消息"}}`)
	service := Service{CorpScope: "wecom-corp:fixture"}
	messages, issues, trusted, next, err := service.normalizePage([]wecomport.EncryptedArchiveRecord{{Seq: 96313, MsgID: "empty-recipient"}}, []wecomport.PlainArchiveRecord{{Seq: 96313, MsgID: "empty-recipient", Payload: raw}})
	if err != nil || len(messages) != 1 || next != 96313 || len(issues) != 1 || len(trusted) != 0 {
		t.Fatalf("err=%v messages=%d issues=%d cursor=%d", err, len(messages), len(issues), next)
	}
	message := messages[0]
	if message.ContentText != "原消息" || !bytes.Equal(message.ProviderPayload, raw) || len(message.Participants) != 2 {
		t.Fatal("message or participant evidence lost")
	}
	for _, participant := range message.Participants {
		if participant.ProviderValue == "" || participant.CustomerID != 0 || participant.IdentityID != 0 {
			t.Fatal("invented recipient identity")
		}
	}
	if issues[0].Reason != "empty_recipient_preserved" || !bytes.Equal(issues[0].Payload, raw) || issues[0].Digest != sha256.Sum256(raw) {
		t.Fatal("protected evidence missing")
	}
	raw[0] = 'x'
	if issues[0].Payload[0] != '{' || message.ProviderPayload[0] != '{' {
		t.Fatal("retained payload aliases caller")
	}
}

func TestArchiveEmptyRecipientCommitsThenContinuesFromSameCursor(t *testing.T) {
	encrypted, plain := validPage()
	plain[0].Payload = []byte(`{"msgid":"m1","from":"employee","tolist":[""],"msgtype":"text","msgtime":1,"text":{"content":"kept"}}`)
	store := &storeStub{}
	reader := &readerStub{encrypted: encrypted, plain: plain, emptyAfter: 1}
	service := serviceFor(reader, store)
	service.PageBudget = 2
	resolver := &resolverStub{}
	service.Identity = resolver
	if err := service.ProcessArchiveDelivery(context.Background(), delivery()); err != nil {
		t.Fatal(err)
	}
	if store.cursor != 1 || store.commits != 1 || store.blocked != 0 || resolver.calls != 0 || reader.calls != 2 {
		t.Fatal("empty recipient blocked ingestion or resolved identity")
	}
}

func TestArchiveNonemptyMalformedRecipientsStillBlockWholePage(t *testing.T) {
	for _, recipient := range []string{" ", " wm_customer", "wm_customer ", string(bytes.Repeat([]byte("x"), 1025))} {
		payload, _ := json.Marshal(map[string]any{"msgid": "m1", "from": "employee", "tolist": []string{recipient}, "msgtype": "text", "msgtime": 1})
		encrypted, plain := validPage()
		plain[0].Payload = payload
		store := &storeStub{}
		err := serviceFor(&readerStub{encrypted: encrypted, plain: plain}, store).ProcessArchiveDelivery(context.Background(), delivery())
		if !errors.Is(err, ErrProviderPage) || store.cursor != 0 || store.commits != 0 || store.blocked != 1 {
			t.Fatal("malformed nonempty identity no longer fails closed")
		}
	}
}

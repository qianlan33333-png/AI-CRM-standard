package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	effectport "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
	wecom "github.com/qianlan33333-png/AI-CRM-standard/internal/wecom"
	wecomport "github.com/qianlan33333-png/AI-CRM-standard/internal/wecom/port"
)

func consentWelcomeInput(t *testing.T, f *channelWelcomeRuntimeFixture, suffix, corp, staff, external string) wecom.DecryptedCallbackEvent {
	t.Helper()
	return wecom.DecryptedCallbackEvent{CorpID: corp, CallbackKey: mustChannelWelcomeKey(t, "wecom:external-contact:"+suffix),
		ReceivedAt: time.Now().UTC().Truncate(time.Microsecond),
		Plaintext:  []byte(fmt.Sprintf(`<xml><ToUserName>%s</ToUserName><CreateTime>1788336003</CreateTime><MsgType>event</MsgType><Event>change_external_contact</Event><ChangeType>msg_audit_approved</ChangeType><ExternalUserID>%s</ExternalUserID><UserID>%s</UserID><WelcomeCode>new-consent-code</WelcomeCode></xml>`, corp, external, staff))}
}

func TestChannelConsentWelcomePostgreSQLJourney(t *testing.T) {
	f := newChannelWelcomeRuntimeFixtureWithWelcomeMessage(t, "原渠道欢迎语")
	defer f.close()
	f.dispatch.ConsentWelcomes = wecom.PostgreSQLConsentWelcomeCorrelator{}
	original, _ := f.acceptWelcome(t, "consent-original", f.now)
	firstWriter := &runtimeWelcomeWriter{called: make(chan runtimeWelcomeCall, 2), err: wecomport.WrapProviderWriteDispositionWithCode(fmt.Errorf("synthetic rejection"), true, false, false, 41051)}
	_, stop := f.startRuntime(t, firstWriter, nil, false)
	f.waitEffect(t, original, effectport.StateFinalFailed)
	stop()
	f.assertWelcomeOutcome(t, original, effectport.StateFinalFailed, "provider_rejected", true)
	// A later configuration change must not redirect the consent event to a
	// new welcome body; the original immutable channel version remains binding.
	if _, err := f.native.Exec(f.ctx, `INSERT INTO channel_config_versions SELECT (jsonb_populate_record(NULL::channel_config_versions,to_jsonb(v)||'{"config_version":2,"welcome_message":"later changed text"}'::jsonb)).* FROM channel_config_versions v WHERE channel_id=$1 AND config_version=1`, f.ready.resolution.Asset.ChannelID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.native.Exec(f.ctx, `UPDATE channels SET current_config_version=2 WHERE id=$1`, f.ready.resolution.Asset.ChannelID); err != nil {
		t.Fatal(err)
	}
	approval := consentWelcomeInput(t, f, "consent-approved", "wx-corp", "employee-1", "external-1")
	// Concurrent callback replays share the same Inbox/grant and deadline.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.dispatch.DispatchDecryptedEvent(f.ctx, approval) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// Distinct consent events still share the original add's one welcome source.
	duplicate := approval
	duplicate.CallbackKey = mustChannelWelcomeKey(t, "wecom:external-contact:consent-second-event")
	duplicate.Plaintext = []byte(strings.ReplaceAll(string(approval.Plaintext), "1788336003", "1788336004"))
	if err := f.dispatch.DispatchDecryptedEvent(f.ctx, duplicate); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.native.QueryRow(f.ctx, `SELECT count(*) FROM channel_welcome_intents WHERE effect_ref IS NOT NULL`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("welcome effects=%d err=%v", count, err)
	}
	var version int64
	var deadline time.Time
	if err := f.native.QueryRow(f.ctx, `SELECT config_version,send_deadline_at FROM channel_welcome_intents WHERE callback_id=$1`, approval.CallbackKey).Scan(&version, &deadline); err != nil || version != 1 || !deadline.Equal(approval.ReceivedAt.Add(20*time.Second)) {
		t.Fatalf("frozen version=%d deadline=%s err=%v", version, deadline, err)
	}
	secondWriter := &runtimeWelcomeWriter{called: make(chan runtimeWelcomeCall, 2)}
	_, stop = f.startRuntime(t, secondWriter, nil, true)
	defer stop()
	f.waitEffect(t, approval, effectport.StateExecuted)
	f.assertWelcomeOutcome(t, approval, effectport.StateExecuted, "sent", true)
	select {
	case call := <-secondWriter.called:
		if call.code != "new-consent-code" || call.message != "原渠道欢迎语" || !call.deadline.Equal(deadline) {
			t.Fatalf("new credential/original body/deadline not preserved")
		}
	case <-time.After(time.Second):
		t.Fatal("welcome writer not called")
	}
	if secondWriter.calls() != 1 {
		t.Fatalf("consent writes=%d", secondWriter.calls())
	}
	f.assertWelcomeOutcome(t, original, effectport.StateFinalFailed, "provider_rejected", true)
}

func TestChannelConsentWelcomePostgreSQLSafety(t *testing.T) {
	for _, state := range []string{"queued", "executed", "outcome_unknown", "final_failed"} {
		t.Run(state, func(t *testing.T) {
			f := newChannelWelcomeRuntimeFixture(t)
			defer f.close()
			f.dispatch.ConsentWelcomes = wecom.PostgreSQLConsentWelcomeCorrelator{}
			original, _ := f.acceptWelcome(t, "consent-safety-original", f.now)
			reason := ""
			var code any
			if state == "executed" {
				reason = "sent"
			}
			if state == "outcome_unknown" {
				reason = "outcome_unknown"
			}
			// Any definite failure is safe to consider with a NEW credential; there
			// is no added numeric Provider error whitelist.
			if state == "final_failed" {
				reason = "provider_rejected"
				code = int64(40003)
			}
			if _, err := f.native.Exec(f.ctx, `UPDATE channel_welcome_intents SET state=$2,result_reason=NULLIF($3,''),provider_error_code=$4,attempt_count=1 WHERE callback_id=$1`, original.CallbackKey, state, reason, code); err != nil {
				t.Fatal(err)
			}
			approval := consentWelcomeInput(t, f, "consent-safety-approved", "wx-corp", "employee-1", "external-1")
			if err := f.dispatch.DispatchDecryptedEvent(f.ctx, approval); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := f.native.QueryRow(f.ctx, `SELECT count(*) FROM channel_welcome_intents WHERE effect_ref IS NOT NULL`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 1
			if state == "final_failed" {
				want = 2
			}
			if count != want {
				t.Fatalf("state=%s effects=%d want=%d", state, count, want)
			}
		})
	}
}

func TestChannelConsentWelcomePostgreSQLCorrelation(t *testing.T) {
	for _, scenario := range []string{"other-corp", "other-staff", "other-customer", "deleted", "newer-add-no-state", "ambiguous-time", "disabled", "rollback"} {
		t.Run(scenario, func(t *testing.T) {
			f := newChannelWelcomeRuntimeFixture(t)
			defer f.close()
			f.dispatch.ConsentWelcomes = wecom.PostgreSQLConsentWelcomeCorrelator{}
			original, _ := f.acceptWelcome(t, "correlation-original", f.now)
			if _, err := f.native.Exec(f.ctx, `UPDATE channel_welcome_intents SET state='final_failed',result_reason='provider_rejected',provider_error_code=41051,attempt_count=1 WHERE callback_id=$1`, original.CallbackKey); err != nil {
				t.Fatal(err)
			}
			corp, staff, external := "wx-corp", "employee-1", "external-1"
			switch scenario {
			case "other-corp":
				corp = "other-corp"
			case "other-staff":
				staff = "other-employee"
			case "other-customer":
				external = "other-external"
			case "deleted", "newer-add-no-state", "ambiguous-time":
				change, timestamp := "del_follow_user", "1788336001"
				if scenario == "newer-add-no-state" {
					change = "add_external_contact"
				}
				if scenario == "ambiguous-time" {
					change = "add_external_contact"
					timestamp = "1788336000"
				}
				event := original
				event.CallbackKey = mustChannelWelcomeKey(t, "wecom:external-contact:correlation-later-event")
				event.Plaintext = []byte(fmt.Sprintf(`<xml><ToUserName>wx-corp</ToUserName><CreateTime>%s</CreateTime><MsgType>event</MsgType><Event>change_external_contact</Event><ChangeType>%s</ChangeType><ExternalUserID>external-1</ExternalUserID><UserID>employee-1</UserID></xml>`, timestamp, change))
				if err := f.dispatch.DispatchDecryptedEvent(f.ctx, event); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if _, err := f.native.Exec(f.ctx, `UPDATE channels SET status='inactive' WHERE id=$1`, f.ready.resolution.Asset.ChannelID); err != nil {
					t.Fatal(err)
				}
			case "rollback":
				// Force the last step (Inbox insert) to fail after intent/effect/grant
				// acceptance; the surrounding UOW must leave none of them committed.
				if _, err := f.native.Exec(f.ctx, `CREATE FUNCTION reject_consent_inbox() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.payload->>'change_type'='msg_audit_approved' THEN RAISE EXCEPTION 'synthetic inbox failure'; END IF; RETURN NEW; END;$$; CREATE TRIGGER reject_consent BEFORE INSERT ON webhook_inbox FOR EACH ROW EXECUTE FUNCTION reject_consent_inbox()`); err != nil {
					t.Fatal(err)
				}
			}
			approval := consentWelcomeInput(t, f, "correlation-approved", corp, staff, external)
			err := f.dispatch.DispatchDecryptedEvent(f.ctx, approval)
			if scenario == "rollback" {
				if err == nil {
					t.Fatal("expected rollback")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var effects int
			if err = f.native.QueryRow(f.ctx, `SELECT count(*) FROM external_effects`).Scan(&effects); err != nil || effects != 1 {
				t.Fatalf("effects=%d err=%v", effects, err)
			}
			if scenario == "rollback" {
				var grants, inboxes, intents int
				if err = f.native.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM wecom_welcome_grants),(SELECT count(*) FROM webhook_inbox),(SELECT count(*) FROM channel_welcome_intents)`).Scan(&grants, &inboxes, &intents); err != nil || grants != 1 || inboxes != 1 || intents != 1 {
					t.Fatalf("rollback grants=%d inboxes=%d intents=%d err=%v", grants, inboxes, intents, err)
				}
			}
		})
	}
}

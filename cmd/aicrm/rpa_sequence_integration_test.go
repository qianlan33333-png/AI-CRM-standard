package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	accessapp "github.com/qianlan33333-png/AI-CRM-standard/internal/access/app"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	cfg "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
)

// Uses real composition, isolated PostgreSQL, Access credentials, Group Ops,
// Outbound and the shared EER/River tables. Only Provider reads are synthetic;
// no client-side UI execution or recipient delivery is claimed by this test.
func TestPostgreSQLRPASequencePairingOrderedSendReplayPauseAndUnknown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	databaseURL, cleanup := adminAccessCompositionDatabase(t, ctx)
	defer cleanup()
	owner := "rpa-fixture-owner"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "access_token": "synthetic-token", "expires_in": 7200})
		case "/cgi-bin/externalcontact/groupchat/get":
			var body struct {
				ChatID string `json:"chat_id"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "group_chat": map[string]any{"chat_id": body.ChatID, "name": "同名群", "owner": owner, "member_list": []any{}}})
		default:
			t.Errorf("unexpected Provider write or read endpoint %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer provider.Close()
	secret := make([]byte, 32)
	if _, e := rand.Read(secret); e != nil {
		t.Fatal(e)
	}
	runtime := cfg.Runtime{Role: cfg.RoleAPI, DatabaseURL: databaseURL, PublicOrigin: "https://rpa.fixture", ReleaseSHA: "rpa-sequence-contract", WorkerOwner: "rpa-sequence-contract", WorkerLimit: 1,
		GroupOps:  cfg.GroupOps{WebhookSecret: "rpa-sequence-contract-secret", ProviderReadEnabled: true},
		Survey:    cfg.Survey{DataKey: base64.RawStdEncoding.EncodeToString(secret), IdentityPhoneDataKey: base64.RawStdEncoding.EncodeToString(secret)},
		Bootstrap: cfg.Bootstrap{Enabled: true, Username: "rpa-admin", Password: "rpa-contract-password", DisplayName: "RPA 验收"},
		WeCom:     cfg.WeCom{Enabled: true, CorpID: "fixture-corp", AgentID: "fixture-agent", Secret: "fixture-secret", ContactSecret: "fixture-contact", ContextSigningKey: strings.Repeat("k", 32), APIBase: provider.URL, HTTPClient: provider.Client()}}
	application, e := compose(ctx, runtime)
	if e != nil {
		t.Fatal(e)
	}
	defer application.Close()
	if e = application.bootstrap(ctx, runtime.Bootstrap); e != nil {
		t.Fatal(e)
	}
	pool := application.pool.Native()
	var actor, plan int64
	if e = pool.QueryRow(ctx, `UPDATE admin_users SET wecom_userid=$1 WHERE username='rpa-admin' RETURNING id`, owner).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `INSERT INTO group_ops_plans(name,status,revision,created_by,updated_by,created_at,updated_at,plan_type)VALUES('双群本地序列','draft',1,$1,$1,now(),now(),'standard')RETURNING id`, actor).Scan(&plan); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO group_ops_plan_members(plan_id,staff_id)VALUES($1,$2);`, plan, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO group_ops_plan_group_assets(plan_id,asset_reference)VALUES($1,'rpa-a'),($1,'rpa-b')`, plan); e != nil {
		t.Fatal(e)
	}
	session, e := application.authentication.LoginWithWeComUserID(ctx, accessapp.WeComLoginCommand{WeComUserID: owner, Remote: "rpa-contract"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path string, body any, key, mode, token string, csrf bool, want int) []byte {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "https://rpa.fixture"+path, bytes.NewReader(raw))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		if mode == "admin" || mode == "sidebar" {
			prefix := "aicrm_admin_"
			if mode == "sidebar" {
				prefix = "aicrm_sidebar_"
			}
			req.AddCookie(&http.Cookie{Name: prefix + "session", Value: session.SessionToken})
			req.AddCookie(&http.Cookie{Name: prefix + "csrf", Value: session.CSRFToken})
			if csrf {
				req.Header.Set("X-CSRF-Token", session.CSRFToken)
			}
		}
		if mode == "native" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		application.handler.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, w.Code, want, w.Body.String())
		}
		return w.Body.Bytes()
	}
	runRPASequenceHTTPContract(t, ctx, pool, plan, owner, call, func() {
		var e error
		session, e = application.authentication.LoginWithWeComUserID(ctx, accessapp.WeComLoginCommand{WeComUserID: owner, Remote: "fresh-rpa-fixture"})
		if e != nil {
			t.Fatal(e)
		}
	}, application.effectsRuntime.Run)
}

type rpaHTTPCall func(method, path string, body any, key, mode, token string, csrf bool, want int) []byte

func runRPASequenceHTTPContract(t *testing.T, ctx context.Context, pool *pgxpool.Pool, plan int64, owner string, call rpaHTTPCall, freshAccount func(), runWorker func(context.Context) error) {
	t.Helper()
	var e error
	pair := func(name string) accessapp.RPAPairStart {
		t.Helper()
		var pair accessapp.RPAPairStart
		if e = json.Unmarshal(call("POST", "/api/rpa/v1/pair/begin", map[string]string{"device_name": name}, "", "public", "", false, 200), &pair); e != nil {
			t.Fatal(e)
		}
		confirmation, parseErr := url.Parse(pair.VerificationURI)
		if parseErr != nil || confirmation.Path != "/admin/automation-conversion/group-ops/ui" || confirmation.Scheme != "https" || confirmation.Query().Get("rpa_pair") != pair.UserCode || len(confirmation.Query()) != 1 {
			t.Fatal("pair confirmation must retain its code on the canonical HTTPS group operations page")
		}
		if _, allowed := allowedOAuthRedirects()[confirmation.Path]; !allowed {
			t.Fatal("a signed-out pairing confirmation cannot return through the existing OAuth allowlist")
		}
		var poll accessapp.RPAPairPoll
		json.Unmarshal(call("POST", "/api/rpa/v1/pair/poll", map[string]string{"device_code": pair.DeviceCode}, "", "public", "", false, 200), &poll)
		if poll.State != "pending" || poll.Credential != nil {
			t.Fatal("credential before approval")
		}
		call("POST", "/api/admin/access/rpa-pair/approve", map[string]string{"user_code": pair.UserCode}, "", "admin", "", false, 403)
		call("POST", "/api/admin/access/rpa-pair/approve", map[string]string{"user_code": pair.UserCode}, "", "admin", "", true, 200)
		call("POST", "/api/admin/access/rpa-pair/approve", map[string]string{"user_code": pair.UserCode}, "", "admin", "", true, 200)
		json.Unmarshal(call("POST", "/api/rpa/v1/pair/poll", map[string]string{"device_code": pair.DeviceCode}, "", "public", "", false, 200), &poll)
		if poll.State != "approved" || poll.Credential == nil || poll.Credential.Token != pair.DeviceCode || poll.Credential.SenderUserID != owner {
			t.Fatal("pair approval binding")
		}
		return pair
	}
	device, foreignDevice := pair("内部测试 Mac"), pair("另一个 Mac")
	call("GET", "/api/rpa/v1/state", nil, "", "public", "", false, 401)
	call("GET", "/api/rpa/v1/state", nil, "", "admin", "", false, 401)
	call("GET", "/api/admin/access/users", nil, "", "native", device.DeviceCode, false, 401)
	messages := []p.SequenceMessage{{ID: "m1", Kind: "text", Text: "第一条"}, {ID: "m2", Kind: "image", RelativePath: "1.jpg", SHA256: strings.Repeat("a", 64), ByteCount: 997213}, {ID: "m3", Kind: "file", RelativePath: "验收.docx", SHA256: strings.Repeat("b", 64), ByteCount: 30769}}
	configBody := map[string]any{"plan_id": fmt.Sprint(plan), "revision": 1, "messages": messages}
	const configPath = "/api/admin/automation-conversion/group-ops/rpa-config"
	const startPath = "/api/admin/automation-conversion/group-ops/rpa-start"
	call("POST", configPath, configBody, "rpa-config-original", "admin", "", false, 403)
	call("POST", configPath, configBody, "rpa-config-original", "admin", "", true, 200)
	call("POST", configPath, configBody, "rpa-config-original", "admin", "", true, 200)
	var config p.SequenceConfig
	json.Unmarshal(call("GET", configPath+"?plan_id="+fmt.Sprint(plan), nil, "", "admin", "", false, 200), &config)
	if config.Revision != 2 || len(config.Messages) != 3 {
		t.Fatal("configuration did not persist")
	}
	var run p.NavigationRun
	json.Unmarshal(call("POST", startPath, map[string]any{"plan_id": fmt.Sprint(plan), "revision": 2}, "rpa-start-original", "admin", "", true, 200), &run)
	if run.State != "paused" || run.Sequence == nil || len(run.Targets) != 2 {
		t.Fatalf("frozen run=%+v", run)
	}
	call("POST", startPath, map[string]any{"plan_id": fmt.Sprint(plan), "revision": 2}, "rpa-start-original", "admin", "", true, 200)
	// Change the plan after freezing; the task must retain the original sequence.
	call("POST", configPath, map[string]any{"plan_id": fmt.Sprint(plan), "revision": 2, "messages": []p.SequenceMessage{{ID: "changed", Kind: "text", Text: "修改后的配置"}}}, "rpa-config-new-version", "admin", "", true, 200)
	read := func(token string) p.SequenceStep {
		t.Helper()
		var view p.SequenceStepView
		json.Unmarshal(call("GET", "/api/rpa/v1/state", nil, "", "native", token, false, 200), &view)
		if view.Step == nil {
			t.Fatal("missing native step")
		}
		return *view.Step
	}
	step := read(device.DeviceCode)
	if step.Command != "SendConversationSequence" || step.State != "paused" || step.Message.Text != "第一条" {
		t.Fatal("frozen content replaced")
	}
	mutate := func(action string, step p.SequenceStep, extra map[string]any, want int) p.SequenceStep {
		t.Helper()
		body := map[string]any{"run_id": fmt.Sprint(step.RunID), "revision": step.Revision, "group_index": step.GroupIndex, "message_index": step.MessageIndex}
		for k, v := range extra {
			body[k] = v
		}
		var view p.SequenceStepView
		raw := call("POST", "/api/rpa/v1/"+action, body, "", "native", device.DeviceCode, false, want)
		if want == 200 {
			if e := json.Unmarshal(raw, &view); e != nil || view.Step == nil {
				t.Fatal("native step response", e)
			}
			return *view.Step
		}
		return step
	}
	mutate("prepare", step, map[string]any{"chat_id": "arbitrary-target"}, 400)
	mutate("prepare", step, nil, 409)
	step = mutate("resume", step, nil, 200)
	call("GET", "/api/rpa/v1/state", nil, "", "native", foreignDevice.DeviceCode, false, 403)
	confirm := func(step p.SequenceStep) p.SequenceStep {
		t.Helper()
		needsAccount := step.MessageIndex == 0 || (step.GroupIndex == 0 && step.MessageIndex == 1)
		if needsAccount {
			step = mutate("verify", step, nil, 200)
		}
		var current p.NavigationView
		json.Unmarshal(call("GET", "/api/sidebar/v2/group-navigation", nil, "", "sidebar", "", false, 200), &current)
		if current.Run == nil {
			t.Fatal("missing workbench projection")
		}
		r := current.Run
		if r.State == "waiting" || (needsAccount && r.State == "verified") {
			json.Unmarshal(call("POST", "/api/sidebar/v2/group-navigation/open", map[string]any{"run_id": fmt.Sprint(r.ID), "revision": r.Revision}, "", "sidebar", "", true, 200), r)
		}
		call("POST", "/api/sidebar/v2/group-navigation/confirm", map[string]any{"run_id": fmt.Sprint(r.ID), "revision": r.Revision, "step": r.Cursor, "chat_id": "wrong-id"}, "", "sidebar", "", true, 409)
		if needsAccount {
			call("POST", "/api/sidebar/v2/group-navigation/confirm", map[string]any{"run_id": fmt.Sprint(r.ID), "revision": r.Revision, "step": r.Cursor, "chat_id": step.ChatID}, "", "sidebar", "", true, 403)
			freshAccount()
		}
		call("POST", "/api/sidebar/v2/group-navigation/confirm", map[string]any{"run_id": fmt.Sprint(r.ID), "revision": r.Revision, "step": r.Cursor, "chat_id": step.ChatID}, "", "sidebar", "", true, 200)
		return read(device.DeviceCode)
	}
	for group := 0; group < 2; group++ {
		for message := 0; message < 3; message++ {
			step = read(device.DeviceCode)
			if step.GroupIndex != group || step.MessageIndex != message {
				t.Fatal("out of order step")
			}
			step = confirm(step)
			if group == 0 && message == 0 {
				// Prove acceptance/effect/queue and progress roll back as one transaction.
				if _, e = pool.Exec(ctx, `CREATE FUNCTION qa_rpa_fail()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic';END$$;CREATE TRIGGER qa_rpa_fail BEFORE INSERT ON outbound_rpa_outbox FOR EACH ROW EXECUTE FUNCTION qa_rpa_fail()`); e != nil {
					t.Fatal(e)
				}
				mutate("prepare", step, nil, 503)
				var count int
				pool.QueryRow(ctx, `SELECT count(*) FROM outbound_rpa_intents`).Scan(&count)
				if count != 0 {
					t.Fatal("failed acceptance leaked intent")
				}
				if restored := read(device.DeviceCode); restored.Revision != step.Revision {
					t.Fatal("failed acceptance advanced progress")
				}
				if _, e = pool.Exec(ctx, `DROP TRIGGER qa_rpa_fail ON outbound_rpa_outbox;DROP FUNCTION qa_rpa_fail()`); e != nil {
					t.Fatal(e)
				}
			}
			prepared := mutate("prepare", step, nil, 200)
			if prepared.Grant == "" || prepared.State != "granted" {
				t.Fatal("missing one-time grant")
			}
			mutate("prepare", step, nil, 409)
			originalGrant := prepared.Grant
			if group == 0 && message == 0 {
				prepared = mutate("pause", prepared, nil, 200)
				if prepared.State != "paused" {
					t.Fatal("pause lost")
				}
			}
			receipt := map[string]any{"grant": originalGrant, "outcome": "client_executed", "evidence_digest": strings.Repeat("c", 64)}
			// Pause projection never contains the grant; retain the immutable original.
			if receipt["grant"] == "" {
				t.Fatal("test lost original grant")
			}
			completed := mutate("complete", prepared, receipt, 200)
			mutate("complete", prepared, receipt, 200)
			if group == 0 && message == 0 {
				if completed.State != "paused" || completed.MessageIndex != 1 {
					t.Fatal("paused receipt lost progress")
				}
				completed = mutate("resume", completed, nil, 200)
			}
			if group == 1 && message == 2 && completed.State != "completed" {
				t.Fatal("last group not complete")
			}
		}
	}
	var intents, effects, attempts int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_rpa_intents),(SELECT count(*) FROM external_effects WHERE kind='rpa_client_send'),(SELECT count(*) FROM external_effect_attempts a JOIN external_effects e ON e.id=a.effect_id WHERE e.kind='rpa_client_send')`).Scan(&intents, &effects, &attempts); e != nil || intents != 6 || effects != 6 || attempts != 6 {
		t.Fatal("intent/effect replay counts", intents, effects, attempts, e)
	}
	var second p.NavigationRun
	if _, e = pool.Exec(ctx, `UPDATE group_ops_plans SET status='paused' WHERE id=$1`, plan); e != nil {
		t.Fatal(e)
	}
	call("POST", configPath, map[string]any{"plan_id": fmt.Sprint(plan), "revision": 3, "messages": []p.SequenceMessage{{ID: "paused-new", Kind: "text", Text: "暂停计划的本地配置"}}}, "rpa-paused-config-original", "admin", "", true, 200)
	json.Unmarshal(call("POST", startPath, map[string]any{"plan_id": fmt.Sprint(plan), "revision": 4}, "rpa-start-unknown", "admin", "", true, 200), &second)
	step = read(device.DeviceCode)
	step = mutate("resume", step, nil, 200)
	step = confirm(step)
	prepared := mutate("prepare", step, nil, 200)
	unknown := mutate("complete", prepared, map[string]any{"grant": prepared.Grant, "outcome": "outcome_unknown", "evidence_digest": strings.Repeat("d", 64)}, 200)
	if unknown.State != "outcome_unknown" {
		t.Fatal("unknown result erased")
	}
	mutate("prepare", unknown, nil, 409)
	mutate("resume", unknown, nil, 409)
	call("POST", startPath, map[string]any{"plan_id": fmt.Sprint(plan), "revision": 4}, "rpa-unknown-replacement", "admin", "", true, 409)
	// Recovery closes the original unknown effect, without another grant,
	// attempt, intent or job. Original unknown completion remains auditable.
	reconciliation := map[string]any{"grant": prepared.Grant, "outcome": "client_executed", "evidence_digest": strings.Repeat("e", 64)}
	mutate("reconcile", prepared, reconciliation, 409) // stale revision
	mutate("reconcile", unknown, map[string]any{"grant": "wrong", "outcome": "client_executed", "evidence_digest": strings.Repeat("e", 64)}, 409)
	mutate("reconcile", unknown, map[string]any{"grant": prepared.Grant, "outcome": "outcome_unknown", "evidence_digest": strings.Repeat("e", 64)}, 409)
	foreignBody := map[string]any{"run_id": fmt.Sprint(unknown.RunID), "revision": unknown.Revision, "group_index": unknown.GroupIndex, "message_index": unknown.MessageIndex, "grant": prepared.Grant, "outcome": "client_executed", "evidence_digest": strings.Repeat("e", 64)}
	call("POST", "/api/rpa/v1/reconcile", foreignBody, "", "native", foreignDevice.DeviceCode, false, 403)
	var countBefore, jobsBefore int
	pool.QueryRow(ctx, `SELECT count(*) FROM outbound_rpa_intents`).Scan(&countBefore)
	pool.QueryRow(ctx, `SELECT count(*) FROM external_effect_jobs`).Scan(&jobsBefore)
	if _, e = pool.Exec(ctx, `CREATE FUNCTION qa_rpa_reconcile_fail()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='reconcile' THEN RAISE EXCEPTION 'synthetic reconcile rollback';END IF;RETURN NEW;END$$;CREATE TRIGGER qa_rpa_reconcile_fail BEFORE INSERT ON outbound_rpa_events FOR EACH ROW EXECUTE FUNCTION qa_rpa_reconcile_fail()`); e != nil {
		t.Fatal(e)
	}
	mutate("reconcile", unknown, reconciliation, 503)
	if current := read(device.DeviceCode); current.State != "outcome_unknown" || current.Revision != unknown.Revision {
		t.Fatal("reconcile failure advanced run")
	}
	var effectState string
	if e = pool.QueryRow(ctx, `SELECT e.state FROM external_effects e JOIN outbound_rpa_intents i ON i.effect_id='eer_'||e.id::text WHERE i.id=$1`, prepared.IntentID).Scan(&effectState); e != nil || effectState != "outcome_unknown" {
		t.Fatal("reconcile failure leaked EER", effectState, e)
	}
	if _, e = pool.Exec(ctx, `DROP TRIGGER qa_rpa_reconcile_fail ON outbound_rpa_events;DROP FUNCTION qa_rpa_reconcile_fail()`); e != nil {
		t.Fatal(e)
	}
	var replay sync.WaitGroup
	for i := 0; i < 2; i++ {
		replay.Add(1)
		go func() { defer replay.Done(); mutate("reconcile", unknown, reconciliation, 200) }()
	}
	replay.Wait()
	recovered := read(device.DeviceCode)
	if recovered.State != "paused" || recovered.GroupIndex != 1 || recovered.MessageIndex != 0 {
		t.Fatal("reconcile must advance once and remain paused", recovered)
	}
	if recovered.Revision != unknown.Revision+1 {
		t.Fatal("concurrent reconciliation advanced twice")
	}
	mutate("reconcile", unknown, reconciliation, 200)
	mutate("reconcile", unknown, map[string]any{"grant": prepared.Grant, "outcome": "client_executed", "evidence_digest": strings.Repeat("f", 64)}, 409)
	var after, jobsAfter, reconcileEvents, completeEvents, attemptCount int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_rpa_intents),(SELECT count(*) FROM external_effect_jobs),(SELECT count(*) FROM outbound_rpa_events WHERE intent_id=$1 AND operation='reconcile'),(SELECT count(*) FROM outbound_rpa_events WHERE intent_id=$1 AND operation='complete'),e.state,e.attempt_count FROM external_effects e JOIN outbound_rpa_intents i ON i.effect_id='eer_'||e.id::text WHERE i.id=$1`, prepared.IntentID).Scan(&after, &jobsAfter, &reconcileEvents, &completeEvents, &effectState, &attemptCount); e != nil || after != countBefore || jobsAfter != jobsBefore || reconcileEvents != 1 || completeEvents != 1 || effectState != "reconciled" || attemptCount != 1 {
		t.Fatal("reconcile replay or audit mismatch", after, jobsAfter, reconcileEvents, completeEvents, effectState, attemptCount, e)
	}
	step = mutate("resume", recovered, nil, 200)
	step = confirm(step)
	prepared = mutate("prepare", step, nil, 200)
	// Advance only the isolated fixture's deadline; the actual shared worker
	// must expire it through its original sink, without a Provider call.
	if _, e = pool.Exec(ctx, `UPDATE outbound_rpa_intents SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1;`, prepared.IntentID); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE river_job SET scheduled_at=clock_timestamp()-interval '1 second' WHERE id IN (SELECT j.river_job_id FROM external_effect_jobs j JOIN outbound_rpa_intents i ON i.effect_id='eer_'||j.effect_id::text WHERE i.id=$1)`, prepared.IntentID); e != nil {
		t.Fatal(e)
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() { workerDone <- runWorker(workerCtx) }()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		pool.QueryRow(ctx, `SELECT state FROM outbound_rpa_intents WHERE id=$1`, prepared.IntentID).Scan(&effectState)
		if effectState == "outcome_unknown" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopWorker()
	if e = <-workerDone; e != nil {
		t.Fatal(e)
	}
	if effectState != "outcome_unknown" {
		t.Fatal("shared worker did not expire original client intent", effectState)
	}
	unknown = read(device.DeviceCode)
	reconciliation = map[string]any{"grant": prepared.Grant, "outcome": "client_executed", "evidence_digest": strings.Repeat("a", 64)}
	mutate("complete", unknown, reconciliation, 409)
	recovered = mutate("reconcile", unknown, reconciliation, 200)
	if recovered.State != "completed" {
		t.Fatal("expired original intent did not reconcile final group", recovered)
	}
	mutate("reconcile", unknown, reconciliation, 200)
	if e = pool.QueryRow(ctx, `SELECT e.state,e.attempt_count,(SELECT count(*) FROM outbound_rpa_events WHERE intent_id=$1 AND operation='expire') FROM external_effects e JOIN outbound_rpa_intents i ON i.effect_id='eer_'||e.id::text WHERE i.id=$1`, prepared.IntentID).Scan(&effectState, &attemptCount, &after); e != nil || effectState != "reconciled" || attemptCount != 1 || after != 1 {
		t.Fatal("expiry reconciliation duplicated or erased original audit", effectState, attemptCount, after, e)
	}
	call("POST", "/api/rpa/v1/device/revoke", map[string]any{}, "", "native", foreignDevice.DeviceCode, false, 200)
	call("GET", "/api/rpa/v1/state", nil, "", "native", foreignDevice.DeviceCode, false, 401)
	_ = read(device.DeviceCode)
	// Revoking/changing the human account session version invalidates the device.
	if _, e = pool.Exec(ctx, `UPDATE admin_users SET session_version=session_version+1 WHERE wecom_userid=$1`, owner); e != nil {
		t.Fatal(e)
	}
	call("GET", "/api/rpa/v1/state", nil, "", "native", device.DeviceCode, false, 401)
}

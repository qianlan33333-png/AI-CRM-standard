package http

import (
	"context"
	"encoding/json"
	access "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
	"net/http/httptest"
	"strings"
	"testing"
)

type nativeSequenceAuth struct{ calls int }

func (a *nativeSequenceAuth) AuthenticateRPA(_ context.Context, token string) (access.RPAPrincipal, error) {
	a.calls++
	return access.RPAPrincipal{DeviceID: 4, StaffID: 19, SenderUserID: "verified-staff"}, nil
}

type nativeSequenceApp struct {
	p.Sequences
	commands []p.SequenceCommand
}

func (a *nativeSequenceApp) ReadSequenceStep(context.Context, p.SidebarActor, int64) (p.SequenceStepView, error) {
	return p.SequenceStepView{}, nil
}
func (a *nativeSequenceApp) MutateSequence(_ context.Context, actor p.SidebarActor, c p.SequenceCommand) (p.SequenceStepView, error) {
	if actor.StaffID != 19 || actor.WeComUserID != "verified-staff" {
		panic("untrusted actor")
	}
	a.commands = append(a.commands, c)
	return p.SequenceStepView{}, nil
}
func TestNativeSequenceTransportRejectsAmbientCredentialsAndArbitraryTargets(t *testing.T) {
	auth := &nativeSequenceAuth{}
	app := &nativeSequenceApp{}
	h := NewNativeSequenceHandler(app, auth)
	for _, tc := range []struct {
		name, body, cookie, origin string
		bearer                     bool
		want                       int
	}{
		{name: "unauthenticated", body: `{}`, want: 401},
		{name: "ambient cookie", body: `{}`, cookie: "aicrm_admin_session=ambient", bearer: true, want: 401},
		{name: "browser origin", body: `{}`, origin: "https://crm.example", bearer: true, want: 401},
		{name: "arbitrary target", body: `{"run_id":"2","revision":1,"chat_id":"another-group"}`, bearer: true, want: 400},
		{name: "arbitrary message", body: `{"run_id":"2","revision":1,"msg":"send this"}`, bearer: true, want: 400},
		{name: "spoof device", body: `{"run_id":"2","revision":1,"device_id":"9"}`, bearer: true, want: 400},
		{name: "server owned target", body: `{"run_id":"2","revision":1,"group_index":0,"message_index":0}`, bearer: true, want: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/rpa/v1/prepare", strings.NewReader(tc.body))
			if tc.bearer {
				r.Header.Set("Authorization", "Bearer arp_synthetic")
			}
			if tc.cookie != "" {
				r.Header.Set("Cookie", tc.cookie)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
			var body json.RawMessage
			if json.Unmarshal(w.Body.Bytes(), &body) != nil {
				t.Fatal("not JSON")
			}
		})
	}
	if len(app.commands) != 1 || app.commands[0].DeviceID != 4 || app.commands[0].RunID != 2 {
		t.Fatal("unexpected native commands", app.commands)
	}
}

func TestNativeReconciliationUsesAuthenticatedDeviceAndEvidenceOnly(t *testing.T) {
	application := &nativeSequenceApp{}
	h := NewNativeSequenceHandler(application, &nativeSequenceAuth{})
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"run_id":"2","revision":1,"grant":"original","outcome":"client_executed"}`, 400},
		{`{"run_id":"2","revision":1,"staff_id":"99","grant":"original","outcome":"client_executed","evidence_digest":"` + strings.Repeat("a", 64) + `"}`, 400},
		{`{"run_id":"2","revision":1,"grant":"original","outcome":"client_executed","evidence_digest":"` + strings.Repeat("a", 64) + `"}`, 200},
	} {
		r := httptest.NewRequest("POST", "/api/rpa/v1/reconcile", strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer arp_synthetic")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(application.commands) != 1 || application.commands[0].Action != "reconcile" || application.commands[0].DeviceID != 4 {
		t.Fatal("reconciliation accepted untrusted identity")
	}
}

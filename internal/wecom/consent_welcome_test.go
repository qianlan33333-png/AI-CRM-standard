package wecom

import (
	"strings"
	"testing"
)

func TestConsentWelcomeRequiresEmployee(t *testing.T) {
	body := `<xml><ToUserName>wx-corp</ToUserName><CreateTime>1788336003</CreateTime><MsgType>event</MsgType><Event>change_external_contact</Event><ChangeType>msg_audit_approved</ChangeType><ExternalUserID>external-1</ExternalUserID><UserID>staff-1</UserID><WelcomeCode>synthetic-code</WelcomeCode></xml>`
	if _, err := parseCallbackEventDraft([]byte(body), "wx-corp"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseCallbackEventDraft([]byte(strings.ReplaceAll(body, "<UserID>staff-1</UserID>", "")), "wx-corp"); err == nil {
		t.Fatal("consent without employee accepted")
	}
}

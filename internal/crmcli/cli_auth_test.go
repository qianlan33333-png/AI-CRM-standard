package crmcli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIHeadlessTokenFileAndNoAutomaticRenewal(t *testing.T) {
	t.Setenv("AICRM_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv("AICRM_CLI_TOKEN", "")
	t.Setenv("AICRM_CLI_PASSWORD", "")
	t.Setenv("AICRM_CLI_CREDENTIAL_FILE", "")
	logins, reads := 0, 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/admin/access/cli/login" {
			logins++
			var v map[string]string
			json.NewDecoder(r.Body).Decode(&v)
			if v["password"] != "fixture-password" {
				w.WriteHeader(401)
				return
			}
			io.WriteString(w, `{"access_token":"acl_private_token","csrf_token":"private_csrf","expires_at":null}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer acl_private_token" || r.Header.Get("Cookie") != "" {
			t.Error("incorrect token transport")
		}
		reads++
		w.WriteHeader(401)
	}))
	defer s.Close()
	old := http.DefaultTransport
	http.DefaultTransport = s.Client().Transport
	defer func() { http.DefaultTransport = old }()
	var out, stderr bytes.Buffer
	run := func(args []string, input string) int {
		out.Reset()
		return Run(args, strings.NewReader(input), &out, &stderr)
	}
	if run([]string{"profile", "add", "headless", s.URL}, "") != 0 {
		t.Fatal(out.String())
	}
	file := filepath.Join(t.TempDir(), "credential.json")
	if run([]string{"auth", "login", "--username", "operator", "--secret-stdin", "--secret-output", file}, "fixture-password") != 0 {
		t.Fatal(out.String())
	}
	raw, e := os.ReadFile(file)
	if e != nil || strings.Contains(string(raw), "fixture-password") || strings.Contains(out.String(), "acl_private_token") {
		t.Fatal("password stored or token leaked")
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal("credential mode")
	}
	t.Setenv("AICRM_CLI_CREDENTIAL_FILE", file)
	if run([]string{"auth", "whoami"}, "") != 3 || logins != 1 || reads != 1 {
		t.Fatalf("expired token auto-logged in: logins=%d reads=%d", logins, reads)
	}
	if e = os.Chmod(file, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadCredential(s.URL); e == nil {
		t.Fatal("public credential file accepted")
	}
	if e = os.Chmod(file, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadCredential("https://different.example.invalid"); e == nil {
		t.Fatal("cross-origin credential accepted")
	}
}

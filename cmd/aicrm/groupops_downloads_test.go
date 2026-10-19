package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGroupOpsDownloadsOnlyPublishedRegularArtifacts(t *testing.T) {
	dir := t.TempDir()
	name := "groupops-helper-0.2.0-abcdef123456.dmg"
	os.WriteFile(filepath.Join(dir, name), []byte("installer"), 0600)
	os.WriteFile(filepath.Join(dir, "release.json"), []byte(`{"version":"0.2.0"}`), 0600)
	os.WriteFile(filepath.Join(dir, "secret.pem"), []byte("private"), 0600)
	os.Symlink(filepath.Join(dir, "secret.pem"), filepath.Join(dir, "groupops-helper-0.2.0-111111111111.dmg"))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(418) })
	handler := mountGroupOpsDownloads(next, dir)
	for _, tc := range []struct {
		method, path string
		code         int
	}{{"GET", "/downloads/groupops/" + name, 200}, {"HEAD", "/downloads/groupops/" + name, 200}, {"GET", "/downloads/groupops/release.json", 200}, {"GET", "/downloads/groupops/secret.pem", 404}, {"GET", "/downloads/groupops/../secret.pem", 404}, {"GET", "/downloads/groupops/groupops-helper-0.2.0-111111111111.dmg", 404}, {"POST", "/downloads/groupops/release.json", 405}, {"GET", "/other", 418}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.code {
			t.Fatalf("%s %s = %d", tc.method, tc.path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mountGroupOpsDownloads(next, "").ServeHTTP(w, httptest.NewRequest("GET", "/downloads/groupops/release.json", nil))
	if w.Code != 404 {
		t.Fatal("unpublished release appeared available")
	}
}

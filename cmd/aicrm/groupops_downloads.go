package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var groupOpsInstallerName = regexp.MustCompile(`^groupops-helper-[0-9]+\.[0-9]+\.[0-9]+-[a-f0-9]{12}\.dmg$`)

// Static release attachments, populated by the existing release workstation.
// No task state, device credentials, or dynamic download service lives here.
func mountGroupOpsDownloads(next http.Handler, directory string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/downloads/groupops/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, prefix)
		if directory == "" || (name != "release.json" && !groupOpsInstallerName.MatchString(name)) {
			http.NotFound(w, r)
			return
		}
		file := filepath.Join(directory, name)
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		if name == "release.json" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, file)
	})
}

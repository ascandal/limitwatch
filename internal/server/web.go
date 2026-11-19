package server

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// registerWeb serves an optional Next.js export without an SPA fallback for
// missing files or API routes. Existing API-only startup needs no Node or assets.
func registerWeb(mux *http.ServeMux, directory string) error {
	info, err := os.Stat(filepath.Join(directory, "index.html"))
	if err != nil || info.IsDir() {
		return fmt.Errorf("WEB_DIR must contain an exported index.html: %s", directory)
	}
	files := http.FileServer(http.Dir(directory))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "v1" || strings.HasPrefix(name, "v1/") ||
			name == "debug" || strings.HasPrefix(name, "debug/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if name == "" || name == "." {
			name = "index.html"
		}
		info, err := os.Stat(filepath.Join(directory, filepath.FromSlash(name)))
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		if strings.HasPrefix(name, "_next/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
	return nil
}

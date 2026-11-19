package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptionalWebExportPreservesRoutes(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{"index.html": "<html>LimitWatch console</html>", "_next/static/main.js": "console.log('console')", "v1/missing": "must not serve this"} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, withAssets := range []bool{true, false} {
		t.Run(map[bool]string{true: "with assets", false: "API only"}[withAssets], func(t *testing.T) {
			cfg := Config{DSN: "file:" + filepath.Join(t.TempDir(), "test.db"), Workers: 1}
			if withAssets {
				cfg.WebDir = dir
			}
			app, err := NewApp(context.Background(), cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close(context.Background())
			rootStatus := http.StatusOK
			for _, tc := range []struct {
				method, path string
				want         int
			}{
				{"GET", "/", rootStatus}, {"GET", "/healthz", 200}, {"GET", "/readyz", 200},
				{"GET", "/version", 200}, {"GET", "/v1/rules", 200}, {"GET", "/v1/alarms", 200},
				{"GET", "/v1/missing", 404}, {"GET", "/v1/alarms/1/events/extra", 404},
				{"GET", "/debug/pprof/", 404}, {"GET", "/not-a-page", 404},
				{"GET", "/_next/static/", 404},
			} {
				rec := httptest.NewRecorder()
				app.Handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
				if rec.Code != tc.want {
					t.Errorf("%s %s: got %d want %d", tc.method, tc.path, rec.Code, tc.want)
				}
				if strings.HasPrefix(tc.path, "/v1/") && strings.Contains(rec.Body.String(), "<html>") {
					t.Errorf("API returned UI: %s", tc.path)
				}
			}
			if withAssets {
				for _, method := range []string{"GET", "HEAD"} {
					rec := httptest.NewRecorder()
					app.Handler.ServeHTTP(rec, httptest.NewRequest(method, "/_next/static/main.js", nil))
					if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
						t.Fatalf("asset response: %d %v", rec.Code, rec.Header())
					}
				}
				rec := httptest.NewRecorder()
				app.Handler.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
				if rec.Code != 405 {
					t.Fatalf("POST / = %d", rec.Code)
				}
			}
		})
	}
}

package server

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestAPIRoutesWithoutWebAssets(t *testing.T) {
	app, err := NewApp(context.Background(), Config{
		DSN: "file:" + filepath.Join(t.TempDir(), "api.db"), Workers: 1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 200}, {"GET", "/healthz", 200}, {"GET", "/readyz", 200},
		{"GET", "/version", 200}, {"GET", "/v1/rules", 200}, {"GET", "/v1/alarms", 200},
		{"GET", "/v1/missing", 404}, {"GET", "/debug/pprof/", 404},
		{"GET", "/_next/static/", 404}, {"GET", "/not-a-page", 404},
		{"POST", "/", 405},
	} {
		response := httptest.NewRecorder()
		app.Handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
		if response.Code != tc.status {
			t.Errorf("%s %s: got %d want %d", tc.method, tc.path, response.Code, tc.status)
		}
	}
}

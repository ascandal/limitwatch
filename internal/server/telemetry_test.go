package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ascandal/limitwatch/internal/engine"
	"github.com/ascandal/limitwatch/internal/storage"
	"github.com/ascandal/limitwatch/internal/telemetry"
)

func TestTelemetryArchiveOnlyContainsAcceptedPrefix(t *testing.T) {
	db, err := storage.Open("file:" + filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng, err := engine.New(nil, 1, 1, false, logger)
	if err != nil {
		t.Fatal(err)
	}

	store := telemetry.New(db)
	mux := http.NewServeMux()
	registerIngest(mux, eng, store, logger)

	// No worker: the queue fills deterministically on the second point.
	request := httptest.NewRequest("POST", "/v1/ingest", bytes.NewBufferString(`[{"name":"Temp","value":1},{"name":"Temp","value":2}]`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != 503 {
		t.Fatalf("expected queue rejection, got %d", response.Code)
	}

	streams, _, err := store.Streams(context.Background())
	if err != nil || len(streams) != 1 || streams[0].Value != 1 {
		t.Fatalf("archived rejected input: %+v %v", streams, err)
	}
}

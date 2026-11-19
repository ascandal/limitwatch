package e2e

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/ascandal/limitwatch/internal/server"
	"github.com/ascandal/limitwatch/internal/telemetry"
)

func TestTelemetryAPIHistoryPermissionsAndRestart(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "telemetry.db") + "?_pragma=busy_timeout(5000)"
	secret := "telemetry-test-only"
	viewer := mustJWT(secret, "reader", "viewer", time.Hour)
	denied := mustJWT(secret, "nobody", "unknown", time.Hour)
	app, err := server.NewApp(context.Background(), server.Config{DSN: dsn, JWTSecret: []byte(secret), Workers: 1}, nil)

	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = app.Close(context.Background()) }()
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)

	points := []map[string]any{
		{"name": "Temp", "entity": "STAND-1", "value": 12, "ts": start.Format(time.RFC3339Nano)},
		{"name": "Temp", "entity": "STAND-1", "value": 18, "ts": start.Add(time.Minute).Format(time.RFC3339Nano)},
		{"name": "Pressure", "value": 2},
	}

	consoleCall(t, app, "POST", "/v1/ingest", "", points, 202, nil)
	consoleCall(t, app, "GET", "/v1/telemetry/streams", "", nil, 401, nil)
	consoleCall(t, app, "GET", "/v1/telemetry/streams", denied, nil, 403, nil)
	consoleCall(t, app, "POST", "/v1/telemetry/streams", viewer, nil, 405, nil)

	var streams struct {
		Streams   []telemetry.Sample `json:"streams"`
		Retention int                `json:"retention_samples"`
	}

	consoleCall(t, app, "GET", "/v1/telemetry/streams", viewer, nil, 200, &streams)
	if len(streams.Streams) != 2 || streams.Retention != telemetry.MaxSamples {
		t.Fatalf("wrong catalog: %+v", streams)
	}

	query := url.Values{"name": {"Temp"}, "entity": {"STAND-1"}, "from": {start.Format(time.RFC3339Nano)}, "to": {start.Add(2 * time.Minute).Format(time.RFC3339Nano)}}
	path := "/v1/telemetry/history?" + query.Encode()
	var history struct {
		Buckets []telemetry.Bucket `json:"buckets"`
	}

	consoleCall(t, app, "GET", path, viewer, nil, 200, &history)
	if len(history.Buckets) != 2 || history.Buckets[0].Mean != 12 || history.Buckets[1].Mean != 18 {
		t.Fatalf("wrong history: %+v", history)
	}

	for _, bad := range []string{"", "?name=Temp", "?name=Temp&entity=A&from=no", "?name=Temp&entity=A&to=no", "?name=Temp&entity=A&from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z", "?name=Temp&entity=A&from=2026-01-01T00:00:00Z&to=2026-03-01T00:00:00Z"} {
		consoleCall(t, app, "GET", "/v1/telemetry/history"+bad, viewer, nil, 400, nil)
	}

	if err := app.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	app, err = server.NewApp(context.Background(), server.Config{DSN: dsn, JWTSecret: []byte(secret), Workers: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	consoleCall(t, app, "GET", path, viewer, nil, 200, &history)
	if len(history.Buckets) != 2 {
		t.Fatalf("restart lost telemetry: %+v", history)
	}
}

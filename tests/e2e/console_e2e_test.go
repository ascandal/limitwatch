package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/repository/sqlite"
	"github.com/ascandal/limitwatch/internal/server"
	"github.com/ascandal/limitwatch/internal/storage"
)

type eventPage struct {
	Events []domain.AlarmEvent `json:"events"`
	Next   int64               `json:"next_after_id"`
	More   bool                `json:"has_more"`
}

func consoleFixture(t *testing.T) (string, int64) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", filepath.Join(t.TempDir(), "console.db"))
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repos := sqlite.New(db)
	rule := &domain.Rule{Name: "TestStandHot", Signal: "TestStand.TempC", Type: domain.RuleThreshold, Severity: domain.SevWarn, Latch: true, Enabled: true, Config: []byte(`{"hi":80,"hysteresis":3,"duration_ms":500}`)}
	if err := repos.Rules.Create(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	return dsn, rule.ID
}

func consoleCall(t *testing.T, app *server.App, method, path, token string, body any, want int, output any) {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	app.Handler.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("%s %s = %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
	}
	if output != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), output); err != nil {
			t.Fatalf("decode %s: %v (%s)", path, err, rec.Body.String())
		}
	}
}

func TestConsoleReplayAndRestart(t *testing.T) {
	dsn, _ := consoleFixture(t)
	app, err := server.NewApp(context.Background(), server.Config{DSN: dsn, Workers: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// This closure follows app through the restart below.
	defer func() { _ = app.Close(context.Background()) }()
	start := time.Now().UTC().Add(-time.Minute)
	send := func(values []float64, offsets []time.Duration) {
		points := make([]map[string]any, 0, len(values))
		for i, value := range values {
			points = append(points, map[string]any{"name": "TestStand.TempC", "entity": "STAND-1", "value": value, "ts": start.Add(offsets[i]).Format(time.RFC3339Nano)})
		}
		consoleCall(t, app, "POST", "/v1/ingest", "", points, 202, nil)
	}
	send([]float64{78, 82, 82, 82}, []time.Duration{0, 300 * time.Millisecond, 600 * time.Millisecond, 900 * time.Millisecond})
	var alarm domain.Alarm
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var alarms []domain.Alarm
		consoleCall(t, app, "GET", "/v1/alarms", "", nil, 200, &alarms)
		if len(alarms) > 0 {
			alarm = alarms[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if alarm.ID == 0 || alarm.State != domain.Active {
		t.Fatalf("expected active alarm: %+v", alarm)
	}
	base := fmt.Sprintf("/v1/alarms/%d", alarm.ID)
	consoleCall(t, app, "POST", base+"/ack", "", nil, 200, nil)
	// Same action is idempotent and must not add a second acknowledgment event.
	consoleCall(t, app, "POST", base+"/ack", "", nil, 200, nil)
	send([]float64{79, 76}, []time.Duration{1200 * time.Millisecond, 1500 * time.Millisecond})
	// Let the in-memory input drain before testing record persistence.
	time.Sleep(100 * time.Millisecond)
	if err := app.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	app, err = server.NewApp(context.Background(), server.Config{DSN: dsn, Workers: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var alarms []domain.Alarm
	consoleCall(t, app, "GET", "/v1/alarms", "", nil, 200, &alarms)
	if len(alarms) != 1 || alarms[0].State != domain.Acked {
		t.Fatalf("restart lost acknowledgment: %+v", alarms)
	}
	consoleCall(t, app, "POST", base+"/clear", "", nil, 200, nil)
	consoleCall(t, app, "POST", base+"/clear", "", nil, 200, nil)
	consoleCall(t, app, "GET", "/v1/alarms", "", nil, 200, &alarms)
	if alarms[0].State != domain.Inactive {
		t.Fatalf("clear failed: %+v", alarms[0])
	}
	cursor := int64(0)
	for _, kind := range []domain.AlarmEventType{domain.EventActivated, domain.EventAcked, domain.EventCleared} {
		var page eventPage
		consoleCall(t, app, "GET", fmt.Sprintf("%s/events?after_id=%d&limit=1", base, cursor), "", nil, 200, &page)
		if len(page.Events) != 1 || page.Events[0].Type != kind || page.Next <= cursor {
			t.Fatalf("bad history page: %+v", page)
		}
		cursor = page.Next
	}
	var page eventPage
	consoleCall(t, app, "GET", fmt.Sprintf("%s/events?after_id=%d&limit=1", base, cursor), "", nil, 200, &page)
	if len(page.Events) != 0 || page.More || page.Next != cursor {
		t.Fatalf("bad terminal page: %+v", page)
	}
}

func TestHistoryPermissionsBoundsAndIDOrder(t *testing.T) {
	dsn, ruleID := consoleFixture(t)
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.New(db)
	alarm := &domain.Alarm{RuleID: ruleID, Entity: "STAND-1", State: domain.Active}
	if err := repos.Alarms.Create(context.Background(), alarm); err != nil {
		t.Fatal(err)
	}
	// Reverse timestamps prove that cursor ordering follows IDs, not clocks.
	for i := 0; i < 3; i++ {
		ev := &domain.AlarmEvent{AlarmID: alarm.ID, Type: domain.EventNote, Actor: "engine", At: time.Now().Add(-time.Duration(i) * time.Hour)}
		if err := repos.Alarms.CreateEvent(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	secret := "console-test-only"
	viewer := mustJWT(secret, "viewer", "viewer", time.Hour)
	app, err := server.NewApp(context.Background(), server.Config{DSN: dsn, Workers: 1, JWTSecret: []byte(secret)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	base := fmt.Sprintf("/v1/alarms/%d", alarm.ID)
	consoleCall(t, app, "GET", base+"/events", "", nil, 401, nil)
	consoleCall(t, app, "GET", base+"/events", "invalid", nil, 401, nil)
	consoleCall(t, app, "POST", base+"/ack", viewer, nil, 403, nil)
	consoleCall(t, app, "POST", base+"/clear", viewer, nil, 403, nil)
	consoleCall(t, app, "POST", base+"/events", viewer, nil, 405, nil)
	for _, query := range []string{"limit=0", "limit=101", "limit=no", "after_id=-1", "after_id=no"} {
		consoleCall(t, app, "GET", base+"/events?"+query, viewer, nil, 400, nil)
	}
	consoleCall(t, app, "GET", "/v1/alarms/99999/events", viewer, nil, 404, nil)
	consoleCall(t, app, "GET", base+"/events/extra", viewer, nil, 404, nil)
	consoleCall(t, app, "GET", "/v1/alarms/-1/events", viewer, nil, 404, nil)
	var page eventPage
	consoleCall(t, app, "GET", base+"/events?limit=2", viewer, nil, 200, &page)
	if len(page.Events) != 2 || page.Events[0].ID >= page.Events[1].ID || !page.Events[0].At.After(page.Events[1].At) || !page.More {
		t.Fatalf("wrong ordering: %+v", page)
	}
	var tail eventPage
	consoleCall(t, app, "GET", fmt.Sprintf("%s/events?after_id=%d&limit=2", base, page.Next), viewer, nil, 200, &tail)
	if len(tail.Events) != 1 || tail.Events[0].ID <= page.Next || tail.More {
		t.Fatalf("wrong cursor: %+v", tail)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, base+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+viewer)
	app.Handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("history must not be cached")
	}
}

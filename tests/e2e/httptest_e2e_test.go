package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/repository/sqlite"
	"github.com/ascandal/limitwatch/internal/server"
	"github.com/ascandal/limitwatch/internal/storage"
)

// End-to-end: seed rule -> start in-process server -> ingest -> see ACTIVE alarm -> ack/silence/clear.
func TestE2E_Httptest_M3(t *testing.T) {
	t.Parallel()

	// --- Arrange: temp DB & seed rule BEFORE server starts (engine compiles on boot) ---
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "e2e.db")
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)",
		dbPath,
	)
	secret := "devsecret-httptest"

	// Seed DB: open -> migrate -> create rule -> close
	{
		db, err := storage.Open(dsn)

		if err != nil {
			t.Fatalf("open db: %v", err)
		}

		defer db.Close()

		if err := storage.Migrate(context.Background(), db); err != nil {
			t.Fatalf("migrate: %v", err)
		}

		repo := sqlite.New(db)
		cfg := json.RawMessage(`{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}`)

		r := &domain.Rule{
			Name:     "BatteryLow",
			Signal:   "Battery.Voltage",
			Type:     domain.RuleThreshold,
			Severity: domain.Severity("warn"),
			Latch:    true,
			Enabled:  true,
			Config:   cfg,
		}

		if err := repo.Rules.Create(context.Background(), r); err != nil {
			t.Fatalf("create rule: %v", err)
		}
	}

	// --- Start in-process server (no network ports needed) ---
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	app, err := server.NewApp(context.Background(), server.Config{
		DSN:       dsn,
		JWTSecret: []byte(secret),
		QueueSize: 4096,
		Workers:   1,
		Trace:     false,
	}, logger)

	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	defer app.Close(context.Background())

	ts := httptest.NewServer(app.Handler)
	defer ts.Close()

	// --- Tokens ---
	admin := mustJWT(secret, "admin@e2e", "admin", time.Hour)
	operator := mustJWT(secret, "ops@e2e", "operator", time.Hour)

	// --- Rules endpoint (auth sanity) ---
	getJSON(t, ts.URL+"/v1/rules", admin, http.StatusOK, nil)

	// --- Ingest points to trip low threshold for >= 500ms ---
	entity := "SC-1"
	now := time.Now().UTC()
	points := []map[string]any{
		{
			"name":   "Battery.Voltage",
			"value":  23.4,
			"entity": entity,
			"ts":     now.Add(0 * time.Millisecond).Format(time.RFC3339Nano),
		},
		{
			"name":   "Battery.Voltage",
			"value":  23.4,
			"entity": entity,
			"ts":     now.Add(300 * time.Millisecond).Format(time.RFC3339Nano),
		},
		{
			"name":   "Battery.Voltage",
			"value":  23.4,
			"entity": entity,
			"ts":     now.Add(600 * time.Millisecond).Format(time.RFC3339Nano),
		},
	}

	postJSON(t, ts.URL+"/v1/ingest", "", points, http.StatusAccepted, nil)

	// --- Poll alarms until ACTIVE appears ---
	var alarmID int64
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		var alarms []struct {
			ID     int64  `json:"id"`
			Entity string `json:"entity"`
			State  string `json:"state"`
		}

		getJSON(t, ts.URL+"/v1/alarms", operator, http.StatusOK, &alarms)

		for _, a := range alarms {
			if a.Entity == entity && strings.EqualFold(a.State, "active") {
				alarmID = a.ID
				break
			}
		}

		if alarmID != 0 {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if alarmID == 0 {
		t.Fatalf("expected ACTIVE alarm, found none")
	}

	// --- Ack / Silence / Clear sequence (latched rule requires explicit clear) ---
	postJSON(
		t,
		fmt.Sprintf("%s/v1/alarms/%d/ack", ts.URL, alarmID),
		operator, nil, http.StatusOK, nil,
	)

	postJSON(
		t,
		fmt.Sprintf("%s/v1/alarms/%d/silence", ts.URL, alarmID),
		operator, map[string]any{"ttl_sec": 60}, http.StatusOK, nil,
	)

	postJSON(
		t,
		fmt.Sprintf("%s/v1/alarms/%d/clear", ts.URL, alarmID),
		operator, nil, http.StatusOK, nil,
	)

	// --- Verify alarm goes INACTIVE ---
	deadline = time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		var alarms []struct {
			ID    int64  `json:"id"`
			State string `json:"state"`
		}

		getJSON(t, ts.URL+"/v1/alarms", operator, http.StatusOK, &alarms)

		for _, a := range alarms {
			if a.ID == alarmID && strings.EqualFold(a.State, "inactive") {
				return
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("alarm %d did not become INACTIVE", alarmID)
}

// --- tiny HTTP helpers ---

func getJSON(t *testing.T, url, bearer string, want int, out any) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)

	if err != nil {
		t.Fatalf("request: %v", err)
	}

	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != want {
		b, _ := ioReadAll(resp)
		t.Fatalf("GET %s: status=%d want=%d body=%s", url, resp.StatusCode, want, string(b))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
}

func postJSON(t *testing.T, url, bearer string, in any, want int, out any) {
	t.Helper()
	var body []byte
	var err error

	if in != nil {
		body, err = json.Marshal(in)

		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))

	if err != nil {
		t.Fatalf("request: %v", err)
	}

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != want {
		b, _ := ioReadAll(resp)
		t.Fatalf("POST %s: status=%d want=%d body=%s", url, resp.StatusCode, want, string(b))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
}

func ioReadAll(r *http.Response) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)

	return buf.Bytes(), err
}

// minimal HS256 generator (mirrors cmd/jwtgen)
func mustJWT(secret, sub, role string, d time.Duration) string {
	enc := base64.RawURLEncoding
	hdr := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	exp := time.Now().Add(d).Unix()

	pl := fmt.Sprintf(`{"sub":%q,"role":%q,"exp":%d}`, sub, role, exp)

	plB := enc.EncodeToString([]byte(pl))
	mac := hmac.New(sha256.New, []byte(secret))

	mac.Write([]byte(hdr))
	mac.Write([]byte("."))
	mac.Write([]byte(plB))

	sig := enc.EncodeToString(mac.Sum(nil))

	return hdr + "." + plB + "." + sig
}

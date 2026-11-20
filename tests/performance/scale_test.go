// Package performance exercises the real HTTP handler, evaluator, manager, and
// on-disk SQLite store together. It intentionally makes no production changes.
package performance

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/ingest"
	"github.com/ascandal/limitwatch/internal/repository/sqlite"
	"github.com/ascandal/limitwatch/internal/server"
	"github.com/ascandal/limitwatch/internal/storage"
	"github.com/ascandal/limitwatch/internal/telemetry"
)

const flushInterval = 10 * time.Millisecond

type rateGroup struct {
	Channels int `json:"channels"`
	Hz       int `json:"hz_per_channel"`
}

type scenario struct {
	Name   string      `json:"name"`
	Groups []rateGroup `json:"groups"`
	Rules  bool        `json:"alarm_rules"`
}

type options struct {
	CheckTiming bool          `json:"check_timing"`
	Duration    time.Duration `json:"duration_ns"`
	Drain       time.Duration `json:"drain_timeout_ns"`
	MaxDelay    time.Duration `json:"max_completion_p99_ns"`
	Batch       int           `json:"max_batch_points"`
	Clients     int           `json:"max_http_clients"`
	Workers     int           `json:"engine_workers"`
	Queue       int           `json:"ingest_queue_size"`
}

func defaults() options {
	return options{CheckTiming: true, Duration: 10 * time.Second, Drain: 5 * time.Second, MaxDelay: 250 * time.Millisecond,
		Batch: 100, Clients: 4, Workers: 1, Queue: 4096}
}

type result struct {
	SenderQueueBatches int         `json:"max_sender_queue_batches"`
	Scenario           scenario    `json:"scenario"`
	Passed             bool        `json:"passed"`
	Problems           []string    `json:"problems"`
	Channels           int         `json:"channels"`
	TargetPointsPS     int         `json:"target_points_per_second"`
	ConfirmedPointsPS  float64     `json:"confirmed_points_per_second"`
	Planned            int         `json:"planned_points"`
	Attempted          int         `json:"attempted_points"`
	Confirmed          int         `json:"confirmed_points"`
	GeneratorDropped   int         `json:"generator_dropped_points"`
	HTTPFailures       int         `json:"http_failures"`
	Statuses           map[int]int `json:"http_status_counts"`
	QueuePeak          int         `json:"observed_ingest_queue_peak"`
	HTTPP50Ms          float64     `json:"http_p50_ms"`
	HTTPP95Ms          float64     `json:"http_p95_ms"`
	HTTPP99Ms          float64     `json:"http_p99_ms"`
	CompletionP99Ms    float64     `json:"completion_p99_ms"`
	WallSeconds        float64     `json:"load_wall_seconds"`
	DrainSeconds       float64     `json:"alarm_drain_seconds"`
	ArchiveWritten     int         `json:"archive_written_points"`
	ArchiveRetained    int         `json:"archive_retained_points"`
	ArchiveMismatches  int         `json:"archive_mismatches"`
	ExpectedEvents     int         `json:"expected_alarm_events"`
	ObservedEvents     int         `json:"observed_alarm_events"`
	EventMismatches    int         `json:"event_mismatches"`
	StateMismatches    int         `json:"final_alarm_state_mismatches"`
	ServerErrors       int         `json:"server_errors"`
	ErrorExamples      []string    `json:"error_examples,omitempty"`
}

type stream struct {
	Name   string
	Hz     int
	RuleID int64
	Next   int
	Count  int
}

func offset(sequence, hz int) time.Duration {
	return time.Duration(sequence) * time.Second / time.Duration(hz)
}

// Every point changes state. This makes a dropped evaluation observable as a
// missing audit event, even if the HTTP request and archive both succeeded.
func value(sequence int) float64 {
	if sequence%2 == 0 {
		return float64(sequence + 1)
	}
	return -float64(sequence + 1)
}

type errorLog struct {
	mu       sync.Mutex
	count    int
	examples []string
}

func (l *errorLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.count++
	if len(l.examples) < 3 {
		l.examples = append(l.examples, strings.TrimSpace(string(data)))
	}
	return len(data), nil
}

// A small, paced functional check runs in the normal test suite. Timing gates
// are generous here; performance conclusions come from the opt-in scale test.
func TestMixedRateTelemetry(t *testing.T) {
	cfg := defaults()
	cfg.Duration = 2 * time.Second
	cfg.CheckTiming = false
	cfg.MaxDelay = 5 * time.Second
	r := run(t, scenario{"mixed_rate_smoke", []rateGroup{{1, 1}, {1, 10}, {1, 100}}, true}, cfg)
	if !r.Passed {
		t.Fatalf("mixed-rate correctness: %+v", r)
	}
}

// TestScale deliberately crosses capacity boundaries. Failed subtests remain in
// the JSON report and cause a nonzero exit. Never use -race for capacity numbers.
func TestScale(t *testing.T) {
	if os.Getenv("LIMITWATCH_PERF") != "1" {
		t.Skip("set LIMITWATCH_PERF=1 or run make perf")
	}
	cfg := defaults()
	for key, dest := range map[string]*time.Duration{
		"LIMITWATCH_PERF_DURATION": &cfg.Duration, "LIMITWATCH_PERF_DRAIN": &cfg.Drain,
		"LIMITWATCH_PERF_MAX_DELAY": &cfg.MaxDelay,
	} {
		if raw := os.Getenv(key); raw != "" {
			n, err := time.ParseDuration(raw)
			if err != nil || n <= 0 {
				t.Fatalf("%s must be a positive duration", key)
			}
			*dest = n
		}
	}
	if cfg.Duration < time.Second || cfg.Duration > 5*time.Minute {
		t.Fatal("LIMITWATCH_PERF_DURATION must be between 1s and 5m")
	}
	channelHz := 10
	for key, dest := range map[string]*int{
		"LIMITWATCH_PERF_BATCH": &cfg.Batch, "LIMITWATCH_PERF_CLIENTS": &cfg.Clients,
		"LIMITWATCH_PERF_QUEUE": &cfg.Queue, "LIMITWATCH_PERF_CHANNEL_HZ": &channelHz,
	} {
		if raw := os.Getenv(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 4096 {
				t.Fatalf("%s must be between 1 and 4096", key)
			}
			*dest = n
		}
	}
	var cases []scenario
	for _, n := range integerList(t, "LIMITWATCH_PERF_CHANNELS", []int{10, 100, 250, 500, 1000}, 10000) {
		cases = append(cases, scenario{fmt.Sprintf("channels_%d_at_%dhz", n, channelHz), []rateGroup{{n, channelHz}}, true})
	}
	cases = append(cases, scenario{"mixed_1_10_100hz", []rateGroup{{10, 1}, {10, 10}, {10, 100}}, true})
	for _, hz := range integerList(t, "LIMITWATCH_PERF_HZ", []int{100, 500, 1000, 2000, 5000, 10000, 20000}, 1000000) {
		cases = append(cases, scenario{fmt.Sprintf("frequency_%dhz", hz), []rateGroup{{1, hz}}, true})
	}
	for _, hz := range integerList(t, "LIMITWATCH_PERF_ARCHIVE_HZ", []int{5000, 10000, 20000, 50000, 100000, 200000}, 1000000) {
		cases = append(cases, scenario{fmt.Sprintf("archive_only_%dhz", hz), []rateGroup{{1, hz}}, false})
	}
	commit, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	dirty, _ := exec.Command("git", "status", "--porcelain").Output()
	report := struct {
		Started string   `json:"started_utc"`
		Go      string   `json:"go_version"`
		OS      string   `json:"os"`
		Arch    string   `json:"arch"`
		CPUs    int      `json:"logical_cpus"`
		Procs   int      `json:"gomaxprocs"`
		Commit  string   `json:"commit"`
		Dirty   bool     `json:"working_tree_dirty"`
		Config  options  `json:"config"`
		Results []result `json:"results"`
	}{time.Now().UTC().Format(time.RFC3339), runtime.Version(), runtime.GOOS, runtime.GOARCH,
		runtime.NumCPU(), runtime.GOMAXPROCS(0), strings.TrimSpace(string(commit)), len(dirty) > 0, cfg, nil}
	for _, s := range cases {
		t.Run(s.Name, func(t *testing.T) {
			r := run(t, s, cfg)
			report.Results = append(report.Results, r)
			t.Logf("channels=%d target=%d/s confirmed=%.0f/s HTTP p95=%.1fms completion p99=%.1fms archive=%d/%d events=%d/%d",
				r.Channels, r.TargetPointsPS, r.ConfirmedPointsPS, r.HTTPP95Ms, r.CompletionP99Ms,
				r.ArchiveWritten, r.Planned, r.ObservedEvents, r.ExpectedEvents)
			if !r.Passed {
				t.Errorf("capacity/correctness gate: %s", strings.Join(r.Problems, "; "))
			}
		})
	}
	if path := os.Getenv("LIMITWATCH_PERF_OUTPUT"); path != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		t.Logf("report: %s", path)
	}
}

func integerList(t *testing.T, key string, fallback []int, maximum int) []int {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	var values []int
	for _, part := range strings.Split(raw, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > maximum {
			t.Fatalf("%s needs comma-separated integers between 1 and %d", key, maximum)
		}
		values = append(values, n)
	}
	return values
}

type batch struct {
	Points []ingest.Point
	Due    time.Time
}

type clientStats struct {
	Attempted, Confirmed, Failures, QueuePeak int
	Statuses                                  map[int]int
	Latency, Completion                       []time.Duration
	Errors                                    []string
}

func send(client *http.Client, url string, jobs <-chan batch) clientStats {
	s := clientStats{Statuses: map[int]int{}}
	for job := range jobs {
		s.Attempted += len(job.Points)
		started := time.Now()
		data, err := json.Marshal(job.Points)
		if err != nil {
			panic(err) // generated, finite values and valid timestamps only
		}
		response, err := client.Post(url+"/v1/ingest", "application/json", bytes.NewReader(data))
		var body []byte
		if err == nil {
			s.Statuses[response.StatusCode]++
			q, _ := strconv.Atoi(response.Header.Get("X-Queue-Len"))
			s.QueuePeak = max(s.QueuePeak, q)
			body, err = io.ReadAll(response.Body)
			_ = response.Body.Close()
		}
		s.Latency = append(s.Latency, time.Since(started))
		s.Completion = append(s.Completion, time.Since(job.Due))
		var ack struct {
			Ingested int `json:"ingested"`
		}
		if err == nil && response.StatusCode == http.StatusAccepted && json.Unmarshal(body, &ack) == nil && ack.Ingested == len(job.Points) {
			s.Confirmed += ack.Ingested
		} else {
			s.Failures++
			if len(s.Errors) < 3 {
				s.Errors = append(s.Errors, fmt.Sprintf("request error=%v body=%.160s", err, body))
			}
		}
	}
	return s
}

func run(t *testing.T, sc scenario, cfg options) result {
	t.Helper()
	r := result{Scenario: sc, Statuses: map[int]int{}}
	dsn := "file:" + filepath.Join(t.TempDir(), "scale.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.New(db)
	var streams []stream
	for _, group := range sc.Groups {
		for range group.Channels {
			s := stream{Name: fmt.Sprintf("Scale.%05d", len(streams)), Hz: group.Hz,
				Count: int((cfg.Duration-1)*time.Duration(group.Hz)/time.Second) + 1}
			if sc.Rules {
				rule := &domain.Rule{Name: s.Name, Signal: s.Name, Type: domain.RuleThreshold,
					Severity: domain.SevWarn, Enabled: true, Config: []byte(`{"hi":0}`)}
				if err := repo.Rules.Create(context.Background(), rule); err != nil {
					t.Fatal(err)
				}
				s.RuleID = rule.ID
			}
			streams = append(streams, s)
			r.Planned += s.Count
			r.TargetPointsPS += s.Hz
		}
	}
	r.Channels = len(streams)
	errors := &errorLog{}
	logger := slog.New(slog.NewJSONHandler(errors, &slog.HandlerOptions{Level: slog.LevelError}))
	app, err := server.NewApp(context.Background(), server.Config{DSN: dsn, Workers: cfg.Workers, QueueSize: cfg.Queue}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	httpServer := httptest.NewServer(app.Handler)
	defer httpServer.Close()
	transport := &http.Transport{MaxIdleConnsPerHost: cfg.Clients, MaxConnsPerHost: cfg.Clients}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	clients := min(cfg.Clients, len(streams))
	queues := make([]chan batch, clients)
	stats := make(chan clientStats, clients)
	for i := range queues {
		// Fit at least two scheduled flushes, even for one-point requests.
		// Otherwise the harness itself would cap the rate at 16 batches/10 ms.
		windowPoints := 0
		for j := i; j < len(streams); j += clients {
			windowPoints += int((time.Duration(streams[j].Hz)*flushInterval + time.Second - 1) / time.Second)
		}
		capacity := max(16, 2*((windowPoints+cfg.Batch-1)/cfg.Batch))
		r.SenderQueueBatches = max(r.SenderQueueBatches, capacity)
		queues[i] = make(chan batch, capacity)
		go func(jobs <-chan batch) { stats <- send(client, httpServer.URL, jobs) }(queues[i])
	}
	start := time.Now().UTC().Add(20 * time.Millisecond)
	// Fixed source schedules, independent of response speed. A bounded sender
	// queue records missed load instead of quietly reducing the offered rate.
	for window := time.Duration(0); window < cfg.Duration; window += flushInterval {
		end := min(window+flushInterval, cfg.Duration)
		time.Sleep(time.Until(start.Add(end)))
		pending := make([][]ingest.Point, clients)
		for i := range streams {
			s := &streams[i]
			for s.Next < s.Count && offset(s.Next, s.Hz) < end {
				pending[i%clients] = append(pending[i%clients], ingest.Point{Name: s.Name, Entity: "PERF",
					Value: value(s.Next), TS: start.Add(offset(s.Next, s.Hz))})
				s.Next++
			}
		}
		for i, points := range pending {
			for len(points) > 0 {
				n := min(cfg.Batch, len(points))
				job := batch{points[:n], start.Add(window)}
				if !cfg.CheckTiming {
					// The normal functional test may wait on a slow CI host.
					queues[i] <- job
				} else {
					select {
					case queues[i] <- job:
					default:
						r.GeneratorDropped += n
					}
				}
				points = points[n:]
			}
		}
	}
	for _, q := range queues {
		close(q)
	}
	var latency, completion []time.Duration
	for range queues {
		s := <-stats
		r.Attempted += s.Attempted
		r.Confirmed += s.Confirmed
		r.HTTPFailures += s.Failures
		r.QueuePeak = max(r.QueuePeak, s.QueuePeak)
		for code, count := range s.Statuses {
			r.Statuses[code] += count
		}
		latency = append(latency, s.Latency...)
		completion = append(completion, s.Completion...)
		r.ErrorExamples = append(r.ErrorExamples, s.Errors...)
	}
	r.WallSeconds = time.Since(start).Seconds()
	r.ConfirmedPointsPS = float64(r.Confirmed) / r.WallSeconds
	slices.Sort(latency)
	slices.Sort(completion)
	r.HTTPP50Ms, r.HTTPP95Ms, r.HTTPP99Ms = percentile(latency, .5), percentile(latency, .95), percentile(latency, .99)
	r.CompletionP99Ms = percentile(completion, .99)
	if sc.Rules {
		r.ExpectedEvents = r.Planned
		drainStart := time.Now()
		for {
			if err := db.QueryRow("SELECT COUNT(*) FROM alarm_events").Scan(&r.ObservedEvents); err != nil {
				t.Fatal(err)
			}
			if r.ObservedEvents >= r.ExpectedEvents || time.Since(drainStart) >= cfg.Drain {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		r.DrainSeconds = time.Since(drainStart).Seconds()
	}
	verify(t, db, streams, start, &r)
	errors.mu.Lock()
	r.ServerErrors = errors.count
	r.ErrorExamples = append(r.ErrorExamples, errors.examples...)
	errors.mu.Unlock()
	check := func(ok bool, message string) {
		if !ok {
			r.Problems = append(r.Problems, message)
		}
	}
	check(r.GeneratorDropped == 0, fmt.Sprintf("generator dropped %d scheduled points", r.GeneratorDropped))
	check(r.Confirmed == r.Planned && r.HTTPFailures == 0, fmt.Sprintf("HTTP confirmed %d/%d points; failures=%d", r.Confirmed, r.Planned, r.HTTPFailures))
	if cfg.CheckTiming {
		check(r.ConfirmedPointsPS >= float64(r.TargetPointsPS)*.98, "achieved rate below 98% of target")
		check(r.CompletionP99Ms <= float64(cfg.MaxDelay)/float64(time.Millisecond), "scheduled-to-HTTP-completion p99 exceeded limit")
		check(r.DrainSeconds <= cfg.MaxDelay.Seconds(), "alarm drain exceeded completion limit")
	}
	check(r.ArchiveWritten == r.Planned && r.ArchiveRetained == min(r.Planned, telemetry.MaxSamples) && r.ArchiveMismatches == 0,
		fmt.Sprintf("archive written=%d/%d retained=%d mismatches=%d", r.ArchiveWritten, r.Planned, r.ArchiveRetained, r.ArchiveMismatches))
	check(r.ObservedEvents == r.ExpectedEvents && r.EventMismatches == 0 && r.StateMismatches == 0,
		fmt.Sprintf("alarm events=%d/%d mismatches=%d final states=%d", r.ObservedEvents, r.ExpectedEvents, r.EventMismatches, r.StateMismatches))
	check(r.ServerErrors == 0, fmt.Sprintf("server logged %d errors", r.ServerErrors))
	r.Passed = len(r.Problems) == 0
	return r
}

func percentile(sorted []time.Duration, fraction float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return float64(sorted[int(math.Ceil(float64(len(sorted))*fraction))-1]) / float64(time.Millisecond)
}

func verify(t *testing.T, db *sql.DB, streams []stream, start time.Time, r *result) {
	t.Helper()
	// AUTOINCREMENT is monotonic in a fresh database: seq counts committed
	// inserts even after the shared retention cap prunes older samples.
	if err := db.QueryRow("SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='telemetry_samples'), 0)").Scan(&r.ArchiveWritten); err != nil {
		t.Fatal(err)
	}
	byName, byRule := map[string]stream{}, map[int64]stream{}
	for _, s := range streams {
		byName[s.Name], byRule[s.RuleID] = s, s
	}
	rows, err := db.Query("SELECT name, entity, value, ts FROM telemetry_samples ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[struct {
		name     string
		sequence int
	}]bool{}
	for rows.Next() {
		var name, entity string
		var v float64
		var ts int64
		if err := rows.Scan(&name, &entity, &v, &ts); err != nil {
			t.Fatal(err)
		}
		r.ArchiveRetained++
		s, ok := byName[name]
		n := int(math.Abs(v)) - 1
		key := struct {
			name     string
			sequence int
		}{name, n}
		if !ok || entity != "PERF" || n < 0 || n >= s.Count || v != value(n) || seen[key] || ts != start.Add(offset(n, s.Hz)).UnixMilli() {
			r.ArchiveMismatches++
		}
		seen[key] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !r.Scenario.Rules {
		return
	}
	rows, err = db.Query("SELECT a.rule_id, e.type, e.at FROM alarm_events e JOIN alarms a ON a.id=e.alarm_id ORDER BY e.id")
	if err != nil {
		t.Fatal(err)
	}
	next := map[int64]int{}
	r.ObservedEvents = 0
	for rows.Next() {
		var ruleID int64
		var kind string
		var at time.Time
		if err := rows.Scan(&ruleID, &kind, &at); err != nil {
			t.Fatal(err)
		}
		s, ok := byRule[ruleID]
		n := next[ruleID]
		want := "activated"
		if n%2 == 1 {
			want = "cleared"
		}
		if !ok || n >= s.Count || kind != want || !at.Equal(start.Add(offset(n, s.Hz))) {
			r.EventMismatches++
		}
		next[ruleID]++
		r.ObservedEvents++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, s := range streams {
		if next[s.RuleID] != s.Count {
			r.EventMismatches++
		}
		var state string
		err := db.QueryRow("SELECT state FROM alarms WHERE rule_id=? AND entity='PERF'", s.RuleID).Scan(&state)
		want := "active"
		if s.Count%2 == 0 {
			want = "inactive"
		}
		if err != nil || state != want {
			r.StateMismatches++
		}
	}
}

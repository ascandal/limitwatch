package telemetry

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ascandal/limitwatch/internal/ingest"
	"github.com/ascandal/limitwatch/internal/storage"
)

func fixture(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := storage.Open("file:" + filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}

func TestHistoryKeepsExcursionsAndSourceTime(t *testing.T) {
	store, _ := fixture(t)
	ctx := context.Background()
	start := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	points := []ingest.Point{
		{Name: "Temp", Entity: "A", TS: start.Add(2 * time.Second), Value: 2},
		{Name: "Temp", Entity: "A", TS: start.Add(time.Second), Value: 100},
		{Name: "Temp", Entity: "A", TS: start, Value: 3}, // Arrives late, still visible as latest received.
		{Name: "Temp", Entity: "B", TS: start, Value: 999},
		{Name: "Pressure", Entity: "A", TS: start, Value: 999},
	}

	if err := store.Record(ctx, points); err != nil {
		t.Fatal(err)
	}

	buckets, err := store.History(ctx, "Temp", "A", start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	if len(buckets) != 1 || buckets[0].Count != 3 || buckets[0].Min != 2 || buckets[0].Max != 100 || buckets[0].Mean != 35 || !buckets[0].Start.Equal(start) {
		t.Fatalf("wrong aggregate: %+v", buckets)
	}

	streams, _, err := store.Streams(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, sample := range streams {
		if sample.Name == "Temp" && sample.Entity == "A" && (sample.Value != 3 || !sample.TS.Equal(start) || sample.ReceivedAt.IsZero()) {
			t.Fatalf("latest received sample is wrong: %+v", sample)
		}
	}

	empty, err := store.History(ctx, "Temp", "A", start.Add(time.Hour), start.Add(2*time.Hour))
	if err != nil || len(empty) != 0 {
		t.Fatalf("bad empty window: %+v %v", empty, err)
	}
}

func TestRetentionAndBucketBounds(t *testing.T) {
	store, db := fixture(t)
	ctx := context.Background()

	// Fill beyond the cap in one SQL statement, then exercise normal pruning.
	_, err := db.Exec(`WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n < ?)
        INSERT INTO telemetry_samples(name, entity, value, ts, received_at)
        SELECT 'Temp', 'A', n, n, n FROM seq`, MaxSamples+2)

	if err != nil {
		t.Fatal(err)
	}

	if err := store.Record(ctx, []ingest.Point{{Name: "Temp", Entity: "A", Value: 8, TS: time.UnixMilli(MaxSamples + 3)}}); err != nil {
		t.Fatal(err)
	}

	var count, first, last int64
	if err := db.QueryRow(`SELECT COUNT(*),MIN(id),MAX(id) FROM telemetry_samples`).Scan(&count, &first, &last); err != nil {
		t.Fatal(err)
	}

	if count != MaxSamples || first != 4 || last != MaxSamples+3 {
		t.Fatalf("retention failed: %d %d %d", count, first, last)
	}

	buckets, err := store.History(ctx, "Temp", "A", time.UnixMilli(0), time.UnixMilli(MaxSamples+3))
	if err != nil {
		t.Fatal(err)
	}

	var total int64
	for _, bucket := range buckets {
		total += bucket.Count
	}

	if len(buckets) > MaxBuckets || total != MaxSamples {
		t.Fatalf("bucket limit lost data: buckets=%d samples=%d", len(buckets), total)
	}
}

func TestFailedBatchRollsBack(t *testing.T) {
	store, db := fixture(t)
	_, err := db.Exec(`CREATE TRIGGER reject_bad BEFORE INSERT ON telemetry_samples WHEN NEW.name='bad' BEGIN SELECT RAISE(ABORT,'injected failure'); END`)

	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := store.Record(context.Background(), []ingest.Point{{Name: "good", Entity: "A", TS: now}, {Name: "bad", Entity: "A", TS: now}}); err == nil {
		t.Fatal("expected failure")
	}

	streams, _, err := store.Streams(context.Background())
	if err != nil || len(streams) != 0 {
		t.Fatalf("partial history batch: %+v %v", streams, err)
	}
}

// Package telemetry stores a bounded monitoring history independently of alarm
// evaluation. Timestamps are Unix milliseconds in SQLite and UTC in the API.
package telemetry

import (
	"context"
	"database/sql"
	"time"

	"github.com/ascandal/limitwatch/internal/ingest"
)

const MaxSamples = 100000
const MaxStreams = 200
const MaxBuckets = 300

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

type Sample struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Entity     string    `json:"entity"`
	Value      float64   `json:"value"`
	TS         time.Time `json:"ts"`
	ReceivedAt time.Time `json:"received_at"`
}

type Bucket struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Min   float64   `json:"min"`
	Max   float64   `json:"max"`
	Mean  float64   `json:"mean"`
	Count int64     `json:"count"`
}

// Record saves only points accepted by the engine queue. A single transaction
// keeps each HTTP batch together and prunes IDs outside the most recent cap.
func (s *Store) Record(ctx context.Context, points []ingest.Point) error {
	if len(points) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() { _ = tx.Rollback() }()
	statement, err := tx.PrepareContext(ctx, `INSERT INTO telemetry_samples(name, entity, value, ts, received_at) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}

	defer func() { _ = statement.Close() }()
	received := time.Now().UTC().UnixMilli()
	var lastID int64
	for _, point := range points {
		result, err := statement.ExecContext(ctx, point.Name, point.Entity, point.Value, point.TS.UnixMilli(), received)
		if err != nil {
			return err
		}
		lastID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	}

	if lastID > MaxSamples {
		if _, err := tx.ExecContext(ctx, `DELETE FROM telemetry_samples WHERE id <= ?`, lastID-MaxSamples); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Streams returns each stream's most recently received sample, not its largest
// source timestamp. This also makes delayed or replayed input visible.
func (s *Store) Streams(ctx context.Context) ([]Sample, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.name, s.entity, s.value, s.ts, s.received_at
        FROM telemetry_samples s JOIN (
            SELECT MAX(id) AS id FROM telemetry_samples GROUP BY name, entity
        ) latest ON s.id = latest.id ORDER BY s.id DESC LIMIT ?`, MaxStreams+1)

	if err != nil {
		return nil, false, err
	}

	defer func() { _ = rows.Close() }()
	samples := make([]Sample, 0)

	for rows.Next() {
		var sample Sample
		var ts, received int64
		if err := rows.Scan(&sample.ID, &sample.Name, &sample.Entity, &sample.Value, &ts, &received); err != nil {
			return nil, false, err
		}
		sample.TS = time.UnixMilli(ts).UTC()
		sample.ReceivedAt = time.UnixMilli(received).UTC()
		samples = append(samples, sample)
	}

	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	more := len(samples) > MaxStreams
	if more {
		samples = samples[:MaxStreams]
	}

	return samples, more, nil
}

// History covers the full requested source-time window. Each time bucket keeps
// minimum and maximum as well as mean, so averaging does not hide excursions.
func (s *Store) History(ctx context.Context, name, entity string, from, to time.Time) ([]Bucket, error) {
	width := (to.UnixMilli()-from.UnixMilli())/MaxBuckets + 1
	if width < 1 {
		width = 1
	}

	rows, err := s.db.QueryContext(ctx, `SELECT MIN(ts), MAX(ts), MIN(value), MAX(value), AVG(value), COUNT(*)
        FROM telemetry_samples WHERE name = ? AND entity = ? AND ts >= ? AND ts <= ?
        GROUP BY (ts - ?) / ? ORDER BY MIN(ts)`, name, entity, from.UnixMilli(), to.UnixMilli(), from.UnixMilli(), width)

	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	buckets := make([]Bucket, 0)

	for rows.Next() {
		var bucket Bucket
		var start, end int64

		if err := rows.Scan(&start, &end, &bucket.Min, &bucket.Max, &bucket.Mean, &bucket.Count); err != nil {
			return nil, err
		}

		bucket.Start = time.UnixMilli(start).UTC()
		bucket.End = time.UnixMilli(end).UTC()
		buckets = append(buckets, bucket)
	}

	return buckets, rows.Err()
}

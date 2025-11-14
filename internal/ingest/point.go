package ingest

import (
    "errors"
    "fmt"
    "math"
    "time"
)

// A single telemetry point.
// TS is event time (UTC). If zero, Normalize() sets it to time.Now().
type Point struct {
    Name   string    `json:"name"`
    Value  float64   `json:"value"`
    TS     time.Time `json:"ts"`
    Entity string    `json:"entity,omitempty"`
}

// Validates and fills defaults.
func (p *Point) Normalize() error {
    if p.Name == "" {
        return errors.New("missing name")
    }

    if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
        return fmt.Errorf("invalid value: %v", p.Value)
    }

    if p.TS.IsZero() {
        p.TS = time.Now().UTC()
    } else {
        p.TS = p.TS.UTC()
    }

    if p.Entity == "" {
        p.Entity = "default"
    }

    return nil
}

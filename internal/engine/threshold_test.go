package engine

import (
    "testing"
    "time"

    "github.com/ascandal/limitwatch/internal/ingest"
)

func TestThreshold_HysteresisDuration(t *testing.T) {
    lo := 23.5
    cfg := &ThresholdConfig{Lo: &lo, Hysteresis: 0.2, DurationMs: 500}
    s := &ruleState{}

    // Below threshold but not long enough
    p1 := ingest.Point{Name: "Battery.Voltage", Value: 23.4, TS: time.Unix(0,0).UTC()}

    if trip, _ := evalThreshold(cfg, s, p1); trip {
        t.Fatalf("expected not trip yet (arming)")
    }

    // After 300ms still not enough
    p2 := ingest.Point{Name: "Battery.Voltage", Value: 23.4, TS: time.Unix(0,300_000_000).UTC()}

    if trip, _ := evalThreshold(cfg, s, p2); trip {
        t.Fatalf("expected not trip at 300ms")
    }

    // After 600ms -> trip
    p3 := ingest.Point{Name: "Battery.Voltage", Value: 23.4, TS: time.Unix(0,600_000_000).UTC()}

    if trip, _ := evalThreshold(cfg, s, p3); !trip {
        t.Fatalf("expected trip at 600ms")
    }

    // Now raise above hysteresis clear (23.7) -> clear
    p4 := ingest.Point{Name: "Battery.Voltage", Value: 23.75, TS: time.Unix(0,700_000_000).UTC()}
    active, detail := evalThreshold(cfg, s, p4)

    if active {
        t.Fatalf("expected clear, got active: %s", detail)
    }
}

package engine

import (
    "testing"
    "time"

    "github.com/ascandal/limitwatch/internal/ingest"
)

func TestDuration_GreaterThan(t *testing.T) {
    cfg := &DurationConfig{Op: ">", Threshold: 10, DurationMs: 1000, Hysteresis: 1.0}
    s := &ruleState{}
    base := time.Unix(0,0).UTC()

    pts := []ingest.Point{
        {Value: 9.5, TS: base},
        {Value: 10.2, TS: base.Add(200 * time.Millisecond)},
        {Value: 10.3, TS: base.Add(800 * time.Millisecond)},
        {Value: 10.4, TS: base.Add(1200 * time.Millisecond)}, // now > 1s over threshold
    }

    // arm
    if act, _ := evalDuration(cfg, s, pts[0]); act {
        t.Fatal("should be inactive")
    }

    if act, _ := evalDuration(cfg, s, pts[1]); act {
        t.Fatal("should be arming")
    }

    if act, _ := evalDuration(cfg, s, pts[2]); act {
        t.Fatal("should be arming")
    }

    if act, _ := evalDuration(cfg, s, pts[3]); !act {
        t.Fatal("should be active after duration")
    }

    // go down but within hysteresis; stay active
    if act, _ := evalDuration(cfg, s, ingest.Point{Value: 9.2, TS: base.Add(1500 * time.Millisecond)}); !act {
        t.Fatal("should hold until passes hysteresis clear")
    }

    // now below clear boundary => clear
    if act, _ := evalDuration(cfg, s, ingest.Point{Value: 8.8, TS: base.Add(1800 * time.Millisecond)}); act {
        t.Fatal("should be cleared")
    }
}

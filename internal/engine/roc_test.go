package engine

import (
    "testing"
    "time"

    "github.com/ascandal/limitwatch/internal/ingest"
)

func TestROC_Basic(t *testing.T) {
    cfg := &ROCConfig{WindowMs: 1000, GT: 0.15, Abs: true}
    s := &ruleState{}
    base := time.Unix(0, 0).UTC()

    // increase ~0.2 per second
    pts := []ingest.Point{
        {Value: 20.0, TS: base},
        {Value: 20.1, TS: base.Add(500 * time.Millisecond)},
        {Value: 20.2, TS: base.Add(1000 * time.Millisecond)},
    }

    // First point: not enough history → arming only
    if act, _ := evalROC(cfg, s, pts[0]); act {
        t.Fatal("should not trip on first point (arming only)")
    }

    // Second point: enough slope over 0.5s → should trip
    if act, _ := evalROC(cfg, s, pts[1]); !act {
        t.Fatal("should trip once there is enough slope over the window")
    }

    // Third point: still strong slope → remain tripped
    if act, _ := evalROC(cfg, s, pts[2]); !act {
        t.Fatal("should remain tripped; rate ~0.2/s > 0.15")
    }

    // Back to flat → should clear
    if act, _ := evalROC(cfg, s, ingest.Point{
        Value: 20.2,
        TS: base.Add(2000 * time.Millisecond),
    }); act {
        t.Fatal("should clear when rate <= threshold")
    }
}

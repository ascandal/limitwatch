package engine

import (
    "testing"
    "time"

    "github.com/ascandal/limitwatch/internal/ingest"
)

func TestROC_Basic(t *testing.T) {
    cfg := &ROCConfig{WindowMs: 1000, GT: 0.15, Abs: true}
    s := &ruleState{}
    base := time.Unix(0,0).UTC()

    // increase 0.2 per second
    pts := []ingest.Point{
        {Value: 20.0, TS: base},
        {Value: 20.1, TS: base.Add(500 * time.Millisecond)},
        {Value: 20.2, TS: base.Add(1000 * time.Millisecond)},
    }

    if act, _ := evalROC(cfg, s, pts[0]); act {
        t.Fatal("arming only")
    }

    if act, _ := evalROC(cfg, s, pts[1]); act {
        t.Fatal("arming only")
    }

    if act, _ := evalROC(cfg, s, pts[2]); !act {
        t.Fatal("should trip; rate ~0.2/s > 0.15")
    }

    // back to flat -> should clear
    if act, _ := evalROC(cfg, s, ingest.Point{Value: 20.2, TS: base.Add(2000 * time.Millisecond)}); act {
        t.Fatal("should clear when rate <= threshold")
    }
}

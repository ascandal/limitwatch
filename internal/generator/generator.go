/*
Generates fake telemetry signals.

Usage:

	// Optional deterministic generator for demo
	if config.BoolEnv("GEN_ENABLED", false) {
		seed := int64(config.GetenvInt("GEN_SEED", 42))
		rate := time.Duration(config.GetenvInt("GEN_RATE_HZ", 10))
		pattern := config.Getenv("GEN_PATTERN", "step") // step|sine|ramp|noise

		go runGenerator(ctx, eng, seed, pattern, rate, logger)

		logger.Info("generator enabled", "pattern", pattern, "rate_hz", rate, "seed", seed)
	}
*/
package generator

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/ascandal/limitwatch/internal/engine"
	"github.com/ascandal/limitwatch/internal/ingest"
)

func RunGenerator(
	ctx context.Context,
	eng *engine.Engine,
	seed int64,
	pattern string,
	rateHz time.Duration,
	logger *slog.Logger,
) {
	// rateHz is number of ticks per second (as int in env)
	if rateHz <= 0 {
		rateHz = 10
	}

	ticker := time.NewTicker(time.Second / rateHz)
	defer ticker.Stop()

	rnd := rand.New(rand.NewSource(seed))
	t0 := time.Now().UTC()
	var i int64 = 0

	for {
		select {
		case <-ctx.Done():
			return
		case ts := <-ticker.C:
			// Generate Battery.Voltage and Temp.C
			v := 24.2

			switch pattern {
			case "step":
				if i%50 < 25 {
					v = 24.2
				} else {
					// low step to cross threshold
					v = 23.3
				}
			case "sine":
				// simple low-frequency sine around 24
				v = 24.0 + 0.8*mathSin(2*3.14159*float64(i)/100.0)
			case "ramp":
				// ramps down then wraps
				v = 24.2 - float64((i%200))/100.0
			case "noise":
				v = 24.0 + (rnd.Float64()-0.5)*1.5
			}

			p1 := ingest.Point{Name: "Battery.Voltage", Value: v, Entity: "SC-1", TS: ts}

			// Temperature: ramp up to trigger ROC (0.2 °C/s)
			base := 20.0 + float64((ts.Sub(t0).Seconds()))*0.2
			temp := base + (rnd.Float64()-0.5)*0.1
			p2 := ingest.Point{Name: "Temp.C", Value: temp, Entity: "SC-1", TS: ts}

			if err := eng.Enqueue(p1); err != nil {
				logger.Warn("generator enqueue failed", "err", err)
			}

			if err := eng.Enqueue(p2); err != nil {
				logger.Warn("generator enqueue failed", "err", err)
			}

			i++
		}
	}
}

// -------------------------------------
// Local helpers for safety and clarity.
// -------------------------------------

// Small local sine to avoid importing math just for one call
func mathSin(x float64) float64 {
	// 5-term Taylor approximation
	x3 := x * x * x
	x5 := x3 * x * x

	return x - x3/6 + x5/120
}

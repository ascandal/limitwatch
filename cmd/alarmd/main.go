/*
Package main provides the LimitWatch server entrypoint.

Key behaviors:
  - Uses stdlib http.ServeMux (no external router yet).
  - Endpoints:
      - GET /healthz   → "ok" (liveness)
      - GET /readyz    → "ready" (readiness)
      - GET /version   → JSON with build info from internal/version
      - /debug/pprof/* → Go’s built-in profiling (disabled by default; enable via PPROF_ENABLED=true)
  - Port defaults to 8080 (override with PORT env).
  - Graceful shutdown on SIGINT/SIGTERM with a 10s timeout for in-flight requests.
  - JSON structured logging via slog at info level.
  - Security notes:
      - pprof is disabled by default, explicitly enable for local debugging.
      - Server has read/write timeouts to mitigate slowloris-style attacks.
      - Health/ready/version handlers restrict methods to GET/HEAD and set no-store cache headers.
*/

package main

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log/slog"
    "math/rand"
    "net/http"
    "net/http/pprof"
    "os"
    "os/signal"
    "runtime"
    "strings"
    "syscall"
    "time"

    "github.com/ascandal/limitwatch/internal/config"
    "github.com/ascandal/limitwatch/internal/engine"
    "github.com/ascandal/limitwatch/internal/ingest"
    "github.com/ascandal/limitwatch/internal/repository/sqlite"
    "github.com/ascandal/limitwatch/internal/storage"
    "github.com/ascandal/limitwatch/internal/version"
)

const defaultPort = "8080"

func main() {
    // ----- Logging setup -----
    logger := slog.New(
        slog.NewJSONHandler(
            os.Stdout,
            &slog.HandlerOptions{Level: slog.LevelInfo},
        ),
    )
    // Uncomment for all packages to use logger via slog.Default():
    // slog.SetDefault(logger)

    // ----- DB open + migrations -----
    // Start the server with a ready database.
    // TODO: update core usage later.
    dsn := config.Getenv(
        "DB_DSN",
        "file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)",
    )

    db, err := storage.Open(dsn)
    if err != nil {
        logger.Error("open database failed", "err", err, "dsn", dsn)
        os.Exit(1)
    }

    defer db.Close()
    if err := storage.Migrate(context.Background(), db); err != nil {
        logger.Error("migrate failed", "err", err)
        os.Exit(1)
    }

    logger.Info("database ready", "dsn", dsn)

    repo := sqlite.New(db)
    rules, err := repo.Rules.List(context.Background())

    if err != nil {
        logger.Error("list rules failed", "err", err)
        os.Exit(1)
    }

    logger.Info("rules loaded", "count", len(rules))

    // ----- Engine setup -----
    qsize := config.GetenvInt("INGEST_QUEUE_SIZE", 4096)
    workers := config.GetenvInt("WORKERS", runtime.NumCPU())
    trace := config.BoolEnv("TRACE_ENABLED", false)

    eng, err := engine.New(rules, qsize, workers, trace, logger)

    if err != nil {
        logger.Error("engine init failed", "err", err)
        os.Exit(1)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    eng.Start(ctx)

    // Optional deterministic generator for demo
    if config.BoolEnv("GEN_ENABLED", false) {
        seed := int64(config.GetenvInt("GEN_SEED", 42))
        rate := time.Duration(config.GetenvInt("GEN_RATE_HZ", 10))
        pattern := config.Getenv("GEN_PATTERN", "step") // step|sine|ramp|noise

        go runGenerator(ctx, eng, seed, pattern, rate, logger)

        logger.Info("generator enabled", "pattern", pattern, "rate_hz", rate, "seed", seed)
    }

    port := config.Getenv("PORT", defaultPort)
    addr := ":" + port

    mux := http.NewServeMux()

    // Health (GET/HEAD only)
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
            return
        }

        noStore(w)
        writeText(w, http.StatusOK, "ok")
    })

    // Readiness (GET/HEAD only)
    mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
        if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
            return
        }

        noStore(w)
        writeText(w, http.StatusOK, "ready")
    })

    // Version (uses encoding/json for correctness/escaping)
    mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
        if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
            return
        }

        noStore(w)

        payload := struct {
            Service string `json:"service"`
            Version string `json:"version"`
            Commit string `json:"commit"`
            Date string `json:"date"`
        }{
            Service: "alarmd",
            Version: version.Version,
            Commit: version.Commit,
            Date: version.Date,
        }

        writeJSON(w, http.StatusOK, payload)
    })

    // Basic root info
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
            return
        }

        noStore(w)

        writeText(
            w,
            http.StatusOK,
            "LimitWatch alarmd up. See /healthz, /readyz, /version, /v1/ingest. Enable /debug/pprof/* via PPROF_ENABLED=true",
        )
    })

    // /v1/ingest: accepts object or array of {name, value, ts?, entity?}
    mux.HandleFunc("/v1/ingest", func(w http.ResponseWriter, r *http.Request) {
        if !allowMethods(w, r, http.MethodPost) {
            return
        }

        if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "application/json") {
            http.Error(
                w,
                "content-type must be application/json",
                http.StatusUnsupportedMediaType,
            )

            return
        }

        body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit

        if err != nil {
            http.Error(w, "read body failed", http.StatusBadRequest)
            return
        }
        defer r.Body.Close()

        // Try array first
        var pts []ingest.Point

        if err := json.Unmarshal(body, &pts); err != nil {
            // Try single object
            var one ingest.Point
            if err2 := json.Unmarshal(body, &one); err2 != nil {
                http.Error(w, "invalid JSON: expected object or array", http.StatusBadRequest)
                return
            }

            pts = []ingest.Point{one}
        }

        n := 0
        for i := range pts {
            if err := pts[i].Normalize(); err != nil {
                http.Error(w, fmt.Sprintf("invalid point at index %d: %v", i, err), http.StatusBadRequest)
                return
            }
            if err := eng.Enqueue(pts[i]); err != nil {
                if errors.Is(err, engine.ErrQueueFull) {
                    w.Header().Set("X-Queue-Len", fmt.Sprintf("%d", eng.QueueLen()))
                    http.Error(w, "queue full", http.StatusServiceUnavailable)

                    return
                }

                http.Error(w, "enqueue failed", http.StatusInternalServerError)

                return
            }
            n++
        }

        w.Header().Set("X-Queue-Len", fmt.Sprintf("%d", eng.QueueLen()))
        writeJSON(w, http.StatusAccepted, map[string]any{"ingested": n})
    })

    if config.BoolEnv("PPROF_ENABLED", false) {
        registerPprof(mux)
        logger.Info("pprof enabled (development only)", "paths", "/debug/pprof/*")
    }

    srv := &http.Server{
        Addr:              addr,
        Handler:           mux,
        // Timeouts to protect against slowloris and resource exhaustion.
        ReadTimeout:       10 * time.Second,
        ReadHeaderTimeout: 5 * time.Second,
        WriteTimeout:      15 * time.Second,
        IdleTimeout:       90 * time.Second,
    }

    // ----- Start server (in background) -----
    go func() {
        logger.Info(
            "starting server",
            "addr", addr,
            "version", version.Version,
            "commit", version.Commit,
        )

        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Error("server error", "err", err)
            os.Exit(1)
        }
    }()

    // ----- Graceful shutdown on SIGINT/SIGTERM -----
    stopCtx, stop := signal.NotifyContext(
        context.Background(),
        syscall.SIGINT,
        syscall.SIGTERM,
    )
    <-stopCtx.Done()
    stop()
    cancel()

    shutdownCtx, cancel2 := context.WithTimeout(
        context.Background(),
        10*time.Second,
    )
    defer cancel2()

    logger.Info("shutting down")
    if err := srv.Shutdown(shutdownCtx); err != nil {
        logger.Error("graceful shutdown failed", "err", err)
        os.Exit(1)
    }
    logger.Info("shutdown complete")
}

// -------------------------------------
// Local helpers for safety and clarity.
// -------------------------------------

func runGenerator(
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
                if i%50 < 25 { v = 24.2 } else { v = 23.3 } // low step to cross threshold 23.5
            case "sine":
                // simple low-frequency sine around 24
                v = 24.0 + 0.8*mathSin(2*3.14159*float64(i)/100.0)
            case "ramp":
                v = 24.2 - float64((i%200))/100.0 // ramps down then wraps
            case "noise":
                v = 24.0 + (rnd.Float64()-0.5)*1.5
            }
            p1 := ingest.Point{Name: "Battery.Voltage", Value: v, Entity: "SC-1", TS: ts}

            // Temperature: ramp up to trigger ROC
            base := 20.0 + float64((ts.Sub(t0).Seconds()))*0.2 // 0.2 °C/s
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


func allowMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
    m := r.Method
    for _, allowed := range methods {
        if m == allowed {
            return true
        }
    }

    w.Header().Set("Allow", strings.Join(methods, ", "))
    http.Error(
        w,
        http.StatusText(http.StatusMethodNotAllowed),
        http.StatusMethodNotAllowed,
    )

    return false
}

// Marks responses as non-cacheable.
func noStore(w http.ResponseWriter) {
    w.Header().Set("Cache-Control", "no-store")
}

// writeText writes a plain text response with status code.
func writeText(w http.ResponseWriter, status int, msg string) {
    w.Header().Set("Content-Type", "text/plain; charset=utf-8")
    w.WriteHeader(status)

    _, _ = w.Write([]byte(msg))
}

// writeJSON writes a JSON response with status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(status)

    _ = json.NewEncoder(w).Encode(v)
}

// registerPprof registers standard pprof handlers under /debug/pprof/.
func registerPprof(mux *http.ServeMux) {
    mux.HandleFunc("/debug/pprof/", pprof.Index)
    mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
    mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
    mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
    mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}

// small local sine to avoid importing math just for one call
func mathSin(x float64) float64 {
    // 5-term Taylor approximation
    x3 := x*x*x
    x5 := x3*x*x

    return x - x3/6 + x5/120
}

/*
The server (cmd/alarmd/main.go)

Key behaviors:
- Uses stdlib http.ServeMux (no external router yet).
- Endpoints:
    - /healthz → "ok"
    - /readyz → "ready"
    - /version → JSON with build info from internal/version
    - /debug/pprof/* → Go’s built‑in profiling UI (CPU, heap, trace) (dev only, disabled in production)
- Port defaults to 8080 (override with PORT env).
- Graceful shutdown: traps SIGINT/SIGTERM, then Server.Shutdown(ctx) with a 10s timeout to drain in‑flight requests.
- Logging: slog in JSON at info level prints structured “starting server”, “shutting down”, and error events.
*/

package main

import (
    "context"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/ascandal/limitwatch/internal/version"
    "net/http/pprof"
)

func main() {
    // Set logging and ports
    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
    port := getenv("PORT", "8080")
    addr := ":" + port

    mux := http.NewServeMux()

    // health & readiness
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/plain")
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ok"))
    })
    mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/plain")
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ready"))
    })
    // version
    mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _, _ = fmt.Fprintf(w, `{"service":"alarmd","version":"%s","commit":"%s","date":"%s"}`,
            version.Version, version.Commit, version.Date)
    })
    // basic root
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/plain")
        _, _ = w.Write([]byte("LimitWatch alarmd up. See /healthz, /readyz, /version, /debug/pprof/"))
    })
    // pprof
    mux.HandleFunc("/debug/pprof/", pprof.Index)
    mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
    mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
    mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
    mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

    srv := &http.Server{
        Addr:              addr,
        Handler:           mux,
        ReadHeaderTimeout: 5 * time.Second,
        IdleTimeout:       90 * time.Second,
    }

    // Start server
    go func() {
        logger.Info("starting server", "addr", addr, "version", version.Version, "commit", version.Commit)
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Error("server error", "err", err)
            os.Exit(1)
        }
    }()

    // Graceful shutdown
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    <-ctx.Done()
    stop()

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    logger.Info("shutting down")
    if err := srv.Shutdown(shutdownCtx); err != nil {
        logger.Error("graceful shutdown failed", "err", err)
        os.Exit(1)
    }
    logger.Info("shutdown complete")
}

func getenv(k, def string) string {
    if v := os.Getenv(k); v != "" {
        return v
    }
    return def
}

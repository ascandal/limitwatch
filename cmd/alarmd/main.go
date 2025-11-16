/*
Provides the LimitWatch server entrypoint using the in-process
server constructor in internal/server/app.go.

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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ascandal/limitwatch/internal/config"
	"github.com/ascandal/limitwatch/internal/server"
	"github.com/ascandal/limitwatch/internal/version"
)

const defaultPort = "8080"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// ----- Logging setup -----
	logger := slog.New(
		slog.NewJSONHandler(
			os.Stdout,
			&slog.HandlerOptions{Level: slog.LevelInfo},
		),
	)
	// Uncomment for all packages to use logger via slog.Default():
	// slog.SetDefault(logger)

	// ----- Server config HTTP routing (env-driven) -----
	port := config.Getenv("PORT", defaultPort)
	addr := ":" + port

	dsn := config.Getenv(
		"DB_DSN",
		"file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)",
	)

	cfg := server.Config{
		DSN:         dsn,
		JWTSecret:   []byte(config.Getenv("JWT_SECRET", "")),
		QueueSize:   config.GetenvInt("INGEST_QUEUE_SIZE", 4096),
		Workers:     config.GetenvInt("WORKERS", 0), // 0 -> default inside server.NewApp
		Trace:       config.BoolEnv("TRACE_ENABLED", false),
		EnablePprof: config.BoolEnv("PPROF_ENABLED", false),
	}

	// ----- Build app (DB -> repos -> engine -> manager -> mux) -----
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app, err := server.NewApp(ctx, cfg, logger)

	if err != nil {
		logger.Error("initialize app failed", "err", err)
		return fmt.Errorf("initialize app: %w", err)
	}

	defer func() {
		if cerr := app.Close(context.Background()); cerr != nil {
			logger.Error("app close failed", "err", cerr)
		}
	}()

	// TODO: Not sure where this belongs or if still needed.
	// defer storage.Close(db, func(err error) {
	// 	logger.Error("close database failed", "err", err)
	// })

	// ----- HTTP server -----
	srv := &http.Server{
		Addr:    addr,
		Handler: app.Handler, // mux,
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
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}
	logger.Info("shutdown complete")

	return nil
}

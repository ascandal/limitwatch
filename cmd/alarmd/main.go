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
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ascandal/limitwatch/internal/version"
)

const defaultPort = "8080"

func main() {
	// ----- Logging setup -----
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	// If you want all packages to use this logger via slog.Default():
	// slog.SetDefault(logger)

	// ----- Server config -----
	port := getenv("PORT", defaultPort)
	addr := ":" + port

	mux := http.NewServeMux()

	// Health & readiness (GET/HEAD only)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
			return
		}

		noStore(w)
		writeText(w, http.StatusOK, "ok")
	})

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
			Commit  string `json:"commit"`
			Date    string `json:"date"`
		}{
			Service: "alarmd",
			Version: version.Version,
			Commit:  version.Commit,
			Date:    version.Date,
		}

		writeJSON(w, http.StatusOK, payload)
	})

	// Basic root info
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
			return
		}

		noStore(w)

		writeText(w, http.StatusOK, "LimitWatch alarmd up. See /healthz, /readyz, /version. Enable /debug/pprof/* via PPROF_ENABLED=true")
	})

	// pprof (disabled by default; enable only for local debugging)
	if boolEnv("PPROF_ENABLED", false) {
		registerPprof(mux)
		logger.Info("pprof enabled (development only)", "paths", "/debug/pprof/*")
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		// Timeouts help protect against slowloris and resource exhaustion.
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	// ----- Start server (in background) -----
	go func() {
		logger.Info("starting server", "addr", addr, "version", version.Version, "commit", version.Commit)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// ----- Graceful shutdown on SIGINT/SIGTERM -----
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

// getenv returns the env var value or a default.
func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}

	return def
}

// boolEnv parses a boolean-like environment variable with common values.
func boolEnv(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "t", "true", "y", "yes", "on":
			return true
		case "0", "f", "false", "n", "no", "off":
			return false
		}
	}

	return def
}

// allowMethods ensures the request uses one of the allowed methods.
func allowMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	m := r.Method

	for _, allowed := range methods {
		if m == allowed {
			return true
		}
	}

	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

	return false
}

// noStore marks responses as non-cacheable.
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

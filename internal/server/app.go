package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"runtime"
	"strings"
	"time"

	"github.com/ascandal/limitwatch/internal/api"
	"github.com/ascandal/limitwatch/internal/config"
	"github.com/ascandal/limitwatch/internal/engine"
	"github.com/ascandal/limitwatch/internal/ingest"
	"github.com/ascandal/limitwatch/internal/manager"
	"github.com/ascandal/limitwatch/internal/repository"
	"github.com/ascandal/limitwatch/internal/repository/sqlite"
	"github.com/ascandal/limitwatch/internal/storage"
	"github.com/ascandal/limitwatch/internal/version"
)

type Config struct {
	DSN         string
	JWTSecret   []byte
	QueueSize   int
	Workers     int
	Trace       bool
	EnablePprof bool // off by default in tests
}

type App struct {
	Handler http.Handler

	ctx    context.Context
	cancel context.CancelFunc
	db     *sql.DB
	repos  repository.Repositories
	eng    *engine.Engine
	mgr    *manager.Manager
	logger *slog.Logger
}

// Sets up the application to be served with:
//   - DB, repos, engine, manager, and HTTP mux.
//
// The returned App is ready to serve via httptest.NewServer(app.Handler).
func NewApp(parent context.Context, cfg Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}

	ctx, cancel := context.WithCancel(parent)

	// --- DB open + migrations ---
	// Start the server with a ready database.
	// TODO: update core usage later.
	db, err := storage.Open(cfg.DSN)
	if err != nil {
		cancel()
		// logger.Error("open database failed", "err", err, "dsn", cfg.DNS)
		return nil, fmt.Errorf("open database failed: %w", err)
	}

	if err := storage.Migrate(ctx, db); err != nil {
		_ = db.Close()
		cancel()
		// logger.Error("migrate failed", "err", err)
		return nil, fmt.Errorf("migrate failed: %w", err)
	}

	logger.Info("database ready", "dsn", cfg.DSN)

	sql := sqlite.New(db)
	repos := repository.Repositories{Rules: sql.Rules, Alarms: sql.Alarms}

	// --- Engine ---
	rules, err := repos.Rules.List(ctx)
	// rules, err := sql.Rules.List(context.Background())

	if err != nil {
		_ = db.Close()
		cancel()
		// logger.Error("list rules failed", "err", err)
		return nil, fmt.Errorf("load rules failed: %w", err)
	}

	logger.Info("rules loaded", "count", len(rules))

	if cfg.QueueSize <= 0 {
		cfg.QueueSize = config.GetenvInt("INGEST_QUEUE_SIZE", 4096)
	}

	if cfg.Workers <= 0 {
		cfg.Workers = config.GetenvInt("WORKERS", runtime.NumCPU())
	}

	eng, err := engine.New(rules, cfg.QueueSize, cfg.Workers, cfg.Trace, logger)

	if err != nil {
		_ = db.Close()
		cancel()
		// logger.Error("engine init failed", "err", err)
		return nil, fmt.Errorf("engine init failed: %w", err)
	}

	// ctx, cancel := context.WithCancel(context.Background())
	// defer cancel()
	eng.Start(ctx)

	// --- Manager ---
	mgr := manager.New(repos, logger)
	mgr.Start(ctx, eng.Reports())

	// --- HTTP routing mux ---
	mux := http.NewServeMux()
	registerCommonRoutes(mux, logger)
	registerIngest(mux, eng, logger)

	// Secure APIs (JWT)
	if len(cfg.JWTSecret) == 0 {
		logger.Warn("JWT disabled (JWT_SECRET empty); all API calls allowed as admin (dev mode).")
	}

	api.New(repos, mgr, cfg.JWTSecret, logger).Routes(mux)

	if cfg.EnablePprof {
		registerPprof(mux)
		logger.Info("pprof enabled (dev only)", "paths", "/debug/pprof/*")
	}

	return &App{
		Handler: mux,
		ctx:     ctx,
		cancel:  cancel,
		db:      db,
		repos:   repos,
		eng:     eng,
		mgr:     mgr,
		logger:  logger,
	}, nil
}

// Cancels workers and closes the DB.
func (a *App) Close(ctx context.Context) error {
	a.cancel()

	// Small wait so engine goroutines observe ctx.Done(); not strictly required.
	timer := time.NewTimer(50 * time.Millisecond)

	select {
	case <-ctx.Done():
	case <-timer.C:
	}

	return a.db.Close()
}

// -------------------------------------
// Local helpers for safety and clarity.
// -------------------------------------

func registerCommonRoutes(mux *http.ServeMux, logger *slog.Logger) {
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

	// Version (uses encoding/json for correctness and escaping)
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

		writeText(w, http.StatusOK, "LimitWatch in-process server")
	})
}

func registerIngest(mux *http.ServeMux, eng *engine.Engine, logger *slog.Logger) {
	// Telemetry point injection (single or multiple points)
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

		defer func() {
			if err := r.Body.Close(); err != nil {
				logger.Warn("close request body failed", "err", err)
			}
		}()

		// First try array of points.
		var pts []ingest.Point

		if err := json.Unmarshal(body, &pts); err != nil {
			// Try single point.
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
				http.Error(
					w,
					fmt.Sprintf("invalid point at index %d: %v", i, err),
					http.StatusBadRequest,
				)
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

// Writes a plain text response with status code.
func writeText(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)

	_, _ = w.Write([]byte(msg))
}

// Writes a JSON response with status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(v)
}

// Registers standard pprof handlers under /debug/pprof/.
func registerPprof(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}

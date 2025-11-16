package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ascandal/limitwatch/internal/auth"
	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/manager"
	"github.com/ascandal/limitwatch/internal/repository"
)

// Exposes HTTP handlers for rules and alarms, including JWT-based
// authentication and role-based access control.
type API struct {
	repos       repository.Repositories
	mgr         *manager.Manager
	logger      *slog.Logger
	jwtSecret   []byte
	authEnabled bool
}

// Constructs an API with the given repositories, manager, JWT secret, and logger.
func New(repos repository.Repositories, mgr *manager.Manager, jwtSecret []byte, logger *slog.Logger) *API {
	return &API{
		repos:       repos,
		mgr:         mgr,
		logger:      logger,
		jwtSecret:   jwtSecret,
		authEnabled: len(jwtSecret) > 0,
	}
}

// Registers all API endpoints on the provided ServeMux.
func (a *API) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/rules", a.withAuth(a.handleRules))
	mux.HandleFunc("/v1/alarms", a.withAuth(a.handleAlarms))

	// /v1/alarms/{id}/(ack|silence|clear)
	mux.HandleFunc("/v1/alarms/", a.withAuth(a.handleAlarmOps))
}

// --- auth middleware ---
type ctxKey int

const claimsKey ctxKey = 1

// Wraps an HTTP handler with JWT-based authentication.
// When JWT is disabled, it attaches a default admin claim for development use.
func (a *API) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authEnabled {
			// attach default admin claims when auth disabled
			c := &auth.Claims{Sub: "dev", Role: "admin"}
			ctx := context.WithValue(r.Context(), claimsKey, c)
			next.ServeHTTP(w, r.WithContext(ctx))

			return
		}

		hdr := r.Header.Get("Authorization")

		if hdr == "" || !strings.HasPrefix(strings.ToLower(hdr), "bearer ") {
			if a.logger != nil {
				a.logger.Warn("missing bearer token", "path", r.URL.Path)
			}

			http.Error(w, "missing bearer token", http.StatusUnauthorized)

			return
		}

		tok := strings.TrimSpace(hdr[len("Bearer "):])
		c, err := auth.ParseAndValidateHS256(tok, a.jwtSecret)

		if err != nil {
			if a.logger != nil {
				a.logger.Warn("invalid token", "path", r.URL.Path, "err", err)
			}

			http.Error(w, "invalid token", http.StatusUnauthorized)

			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// Extracts authentication claims from the request context.
func getClaims(r *http.Request) *auth.Claims {
	v := r.Context().Value(claimsKey)

	if v == nil {
		return nil
	}

	if c, ok := v.(*auth.Claims); ok {
		return c
	}

	return nil
}

// Checks that the caller's role is at least the given minimum.
// If authorization fails, writes an HTTP error response and returns false.
func requireRole(w http.ResponseWriter, r *http.Request, min string) bool {
	c := getClaims(r)

	if c == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)

		return false
	}

	if auth.RoleOrder(c.Role) < auth.RoleOrder(min) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return false
	}

	return true
}

// --- /v1/rules ---
func (a *API) handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// viewer+
		if !requireRole(w, r, "viewer") {
			return
		}

		list, err := a.repos.Rules.List(r.Context())

		if err != nil {
			if a.logger != nil {
				a.logger.Error("list rules failed", "err", err)
			}

			http.Error(w, "list rules failed", http.StatusInternalServerError)

			return
		}

		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		// admin only
		if !requireRole(w, r, "admin") {
			return
		}

		var in struct {
			Name     string          `json:"name"`
			Signal   string          `json:"signal"`
			Type     string          `json:"type"`
			Severity string          `json:"severity"`
			Latch    bool            `json:"latch"`
			Enabled  bool            `json:"enabled"`
			Config   json.RawMessage `json:"config"`
		}

		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		if in.Name == "" || in.Signal == "" || in.Type == "" || in.Severity == "" {
			http.Error(w, "missing fields", http.StatusBadRequest)
			return
		}

		rule := &domain.Rule{
			Name: in.Name, Signal: in.Signal,
			Type:     domain.RuleType(strings.ToLower(in.Type)),
			Severity: domain.Severity(strings.ToLower(in.Severity)),
			Latch:    in.Latch, Enabled: in.Enabled,
			Config: in.Config,
		}

		if err := a.repos.Rules.Create(r.Context(), rule); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				http.Error(w, "rule name already exists", http.StatusConflict)
				return
			}

			if a.logger != nil {
				a.logger.Error("create rule failed", "err", err)
			}

			http.Error(w, "create failed", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusCreated, rule)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- /v1/alarms ---
func (a *API) handleAlarms(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !requireRole(w, r, "viewer") {
			return
		}

		limit := 100

		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				limit = n
			}
		}

		list, err := a.repos.Alarms.List(r.Context(), limit)

		if err != nil {
			if a.logger != nil {
				a.logger.Error("list alarms failed", "err", err)
			}

			http.Error(w, "list alarms failed", http.StatusInternalServerError)

			return
		}

		writeJSON(w, http.StatusOK, list)
	default:
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- /v1/alarms/{id}/(ack|silence|clear) ---
func (a *API) handleAlarmOps(w http.ResponseWriter, r *http.Request) {
	// path: /v1/alarms/{id}/op
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/alarms/"), "/")

	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)

	if err != nil {
		http.NotFound(w, r)
		return
	}

	op := parts[1]

	switch op {
	case "ack":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}

		if !requireRole(w, r, "operator") {
			return
		}

		actor := actorFrom(r)

		if err := a.mgr.Ack(r.Context(), id, actor); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				http.Error(w, "alarm inactive", http.StatusConflict)
				return
			}

			if a.logger != nil {
				a.logger.Error("ack failed", "err", err, "alarm", id)
			}

			http.Error(w, "ack failed", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "acked"})
	case "silence":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}

		if !requireRole(w, r, "operator") {
			return
		}

		var in struct {
			TTL   int    `json:"ttl_sec,omitempty"`
			Until string `json:"until,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		var until time.Time

		switch {
		case in.Until != "":
			t, err := time.Parse(time.RFC3339, in.Until)

			if err != nil {
				http.Error(w, "bad until", http.StatusBadRequest)
				return
			}
			until = t.UTC()
		case in.TTL > 0:
			until = time.Now().UTC().Add(time.Duration(in.TTL) * time.Second)
		default:
			http.Error(w, "ttl_sec or until required", http.StatusBadRequest)
			return
		}

		actor := actorFrom(r)

		if err := a.mgr.Silence(r.Context(), id, until, actor); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				http.Error(w, "alarm inactive", http.StatusConflict)
				return
			}

			if a.logger != nil {
				a.logger.Error("silence failed", "err", err, "alarm", id)
			}

			http.Error(w, "silence failed", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "silenced", "until": until.Format(time.RFC3339)})
	case "clear":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}

		if !requireRole(w, r, "operator") {
			return
		}

		actor := actorFrom(r)

		if err := a.mgr.Clear(r.Context(), id, actor); err != nil {
			if a.logger != nil {
				a.logger.Error("clear failed", "err", err, "alarm", id)
			}

			http.Error(w, "clear failed", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
	default:
		http.NotFound(w, r)
	}
}

// Returns the subject from the JWT claims, or "unknown" if not present.
func actorFrom(r *http.Request) string {
	if c := getClaims(r); c != nil && c.Sub != "" {
		return c.Sub
	}

	return "unknown"
}

// Writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	_ = json.NewEncoder(w).Encode(v)
}

// Small helper for returning a 405 response with an Allow header.
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

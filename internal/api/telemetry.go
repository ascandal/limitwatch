package api

import (
	"net/http"
	"time"

	"github.com/ascandal/limitwatch/internal/telemetry"
)

// TelemetryRoutes exposes monitoring reads using the existing viewer role.
func (a *API) TelemetryRoutes(mux *http.ServeMux, store *telemetry.Store) {
	mux.HandleFunc("/v1/telemetry/streams", a.withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		if !requireRole(w, r, "viewer") {
			return
		}
		streams, more, err := store.Streams(r.Context())
		if err != nil {
			http.Error(w, "read telemetry failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"streams": streams, "has_more": more, "retention_samples": telemetry.MaxSamples,
		})
	}))
	mux.HandleFunc("/v1/telemetry/history", a.withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		if !requireRole(w, r, "viewer") {
			return
		}
		query := r.URL.Query()
		name, entity := query.Get("name"), query.Get("entity")
		if name == "" || entity == "" {
			http.Error(w, "name and entity are required", http.StatusBadRequest)
			return
		}
		to := time.Now().UTC()
		var err error
		if query.Get("to") != "" {
			to, err = time.Parse(time.RFC3339Nano, query.Get("to"))
			if err != nil {
				http.Error(w, "to must be an RFC3339 timestamp", http.StatusBadRequest)
				return
			}
		}
		from := to.Add(-time.Hour)
		if query.Get("from") != "" {
			from, err = time.Parse(time.RFC3339Nano, query.Get("from"))
			if err != nil {
				http.Error(w, "from must be an RFC3339 timestamp", http.StatusBadRequest)
				return
			}
		}
		if !from.Before(to) || to.Sub(from) > 31*24*time.Hour {
			http.Error(w, "time range must be positive and no longer than 31 days", http.StatusBadRequest)
			return
		}
		buckets, err := store.History(r.Context(), name, entity, from, to)
		if err != nil {
			http.Error(w, "read telemetry history failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"buckets": buckets, "from": from.UTC(), "to": to.UTC(), "retention_samples": telemetry.MaxSamples,
		})
	}))
}

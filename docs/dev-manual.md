# Development Manual

## Phase-0 - Project Scaffold
- A minimal HTTP service (binary: alarmd) exposing:
    - **GET /healthz** – liveness
    - **GET /readyz** – readiness
    - **GET /version** – build metadata (version/commit/date)
    - **GET /debug/pprof/*** – profiling endpoints
- Graceful shutdown on SIGINT/SIGTERM with a 10‑second timeout.
- JSON logging via Go’s slog.
- CI pipeline (GitHub Actions):
    - format check
    - vet
    - static analysis (staticcheck)
    - go test -race
    - build
- Container image (multi‑stage, distroless, non‑root).
- Helm chart skeleton (Deployment/Service, probes wired up).
- Other
    - MIT license
    - .gitignore
    - Makefile
    - placeholders for Grafana/Helm.


### Directory Structure

```text
LimitWatch/
├─ cmd/
│  ├─ alarmd/                 # Service entrypoint (main.go)
│  └─ alarmctl/
├─ internal/version/           # Build-time version variables (ldflags)
├─ deploy/
│  ├─ docker/Dockerfile        # Multi-stage → distroless:nonroot runtime
│  └─ helm/
│     ├─ Chart.yaml            # Chart metadata
│     ├─ values.yaml           # Image/port values
│     └─ templates/
│        ├─ deployment.yaml    # Deployment + probes
│        └─ service.yaml       # ClusterIP Service
├─ grafana/README.md           # Placeholder (dashboards come in Phase 5)
├─ .github/workflows/ci.yml    # CI: fmt, vet, staticcheck, test -race, build
├─ Makefile                    # Common tasks (build/run/docker/etc.)
├─ go.mod                      # Module path
├─ .gitignore                  # Sensible ignores
├─ LICENSE                     # MIT
└─ README.md                   # Quick start
```

- The server (cmd/alarmd/main.go)
- Build‑time versioning (internal/version + ldflags)
- Logging
    - Uses `slog.NewJSONHandler` → clean, structured JSON logs.
    - Startup log includes addr, version, and commit.
    - Error logs include `"err": "<message>"` field.
- CI pipeline (.github/workflows/ci.yml)


## Phase 1 — Core domain & storage

- **Domain types**:
    - Centralized, stable, and storage-agnostic data types.
    - Rule
    - Alarm
    - AlarmEvent
    - typed enums
- **Migrations**: Safe and repeatable SQL schema migrations and auto-bootstrap on startup.
- **SQLite default**: Minimal external dependency for SQLite-backed fast demos.
    - Later, point to Postgres by switching DSN/driver.
- **CLI**: Temporary CLI (`alarmctl`) to exercise the storage layer.
    - Create/read rules and alarms. (Milestone M1)

### Getting started

1) Ensure your module path matches your repo (e.g., `github.com/ascandal/limitwatch`) and run:

```bash
go mod tidy
```

2) Run migrations implicitly by starting either the server or the CLI (both call `storage.Migrate`):

```bash
# Start server (db: file:limitwatch.db)
go run ./cmd/alarmd
```

3) Create a rule via CLI:
Threshold rule with config JSON (example: low threshold with hysteresis)

```bash
go run ./cmd/alarmctl rule create \
    --name BatteryLow \
    --signal Battery.Voltage \
    --type threshold \
    --severity warn \
    --latch \
    --config-json '{"lo": 23.5, "hysteresis": 0.2, "deadband": 0.1, "duration_ms": 0}'
```

4) List rules:

```bash
go run ./cmd/alarmctl rule list
```

5) Create a test alarm and list it:
```bash
go run ./cmd/alarmctl alarm create --rule-id 1 --entity SC-1 --state active --note "Triggered by test"
go run ./cmd/alarmctl alarm list
```

### Configuration

- `DB_DSN` — database DSN (defaults to `file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`).
- For SQLite, the DSN above enables WAL mode and a 5s busy timeout for better concurrency and fewer "database is locked" errors.

### What’s next (Phase 2 preview)
- Add ingest endpoint and a rule engine to evaluate thresholds/ROC/duration in-process with bounded latency.
- Extend repositories with update methods and implement the Alarm FSM transitions and audit entries.
- Wire API endpoints for rules CRUD and alarm listing/ops.
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


## Tooling

- `tools.go` pins CLI tool dependency versions.
    - Blank imports (`_ "module/path"`) to declare dependencies on CLI tools.
    - The tools build tag `//go:build tools tag` prevents the file from getting built into the app. It generates a durable list so `go.mod` can track the tool versions.
- `Taskfile.yml` defines fast, cross‑platform commands to run locally and in CI.

To add the Go bin directory to the shell PATH run:

```zsh
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

Install the command runner (**Task**):
[Github go-task](https://github.com/go-task/task)
[Taskfile.dev](https://taskfile.dev/)

```zsh
npm install -g @go-task/cli
```

or

```zsh
go install github.com/go-task/task/v3/cmd/task@latest
```

Install the tool binaries at the pinned versions

```zsh
go install golang.org/x/tools/cmd/goimports@$(go list -f '{{.Version}}' -m golang.org/x/tools)
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(go list -f '{{.Version}}' -m github.com/golangci/golangci-lint/v2)
go install gotest.tools/gotestsum@$(go list -f '{{.Version}}' -m gotest.tools/gotestsum)
go install golang.org/x/vuln/cmd/govulncheck@$(go list -f '{{.Version}}' -m golang.org/x/vuln)
```

Notes from install:

```zsh
# github.com/golangci/golangci-lint/cmd/golangci-lint
ld: warning: -bind_at_load is deprecated on macOS
```

### Run the Dev Tools

Run any of these commands from the root directory.

#### List all tasks:

```zsh
task --list
```

#### Format (canonical style + imports):

```zsh
task fmt
```

- `gofmt -s -w .` rewrites code with Go’s standard formatting and simplifications.
- `goimports -w .` does gofmt and fixes/organizes imports (adds/removes/sorts).

#### Lint (static analysis):

```zsh
task lint
```

- Runs golangci-lint (a fast meta‑linter that aggregates many linters).

#### Test (concise, readable output):

```zsh
task test
```

- `gotestsum --format=short-verbose -- ./...` runs all package tests and prints a compact per‑test summary.

#### Vulnerability scan (supply‑chain + code paths):

```zsh
task vuln
```

- govulncheck consults Go’s vuln DB and reports reachable vulnerabilities, reducing false positives.

#### Run the app:

```zsh
task run
```

- Executes `go run ./cmd/alarmd`.

#### Build a slim binary:

```zsh
task build
```

- `-trimpath` strips local paths from the binary for reproducibility.
- `-ldflags="-s -w"` drops symbol & DWARF tables for smaller size.
- Output goes to bin/app.


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
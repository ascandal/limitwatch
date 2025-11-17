# Developer Guide

## Directory Structure

```text
LimitWatch/
├─ cmd/
│  ├─ alarmd/                  # Service entrypoint (main.go)
│  └─ alarmctl/                # developer/admin CLI for storage ops
├─ deploy/
│  ├─ docker/Dockerfile        # Multi-stage → distroless:nonroot runtime
│  └─ helm/
│     ├─ Chart.yaml            # Chart metadata
│     ├─ values.yaml           # Image/port values
│     └─ templates/
│        ├─ deployment.yaml    # Deployment + probes
│        └─ service.yaml       # ClusterIP Service
├─ docs/
├─ grafana/README.md           # Placeholder (dashboards come in Phase 5)
├─ internal/
│  ├─ api/
│  ├─ auth/
│  ├─ config/
│  ├─ domain/
│  ├─ engine/
│  ├─ generator/
│  ├─ ingest/
│  ├─ manager/
│  ├─ repository/
│  ├─ server/
│  ├─ storage/
│  └─ version/                 # Build-time version variables (ldflags)
├─ tests/
│  └─ e2e/
├─ Makefile                    # Common tasks (build/run/docker/etc.)
├─ go.mod                      # Module path
├─ .gitignore                  # Sensible ignores
├─ LICENSE                     # MIT
└─ README.md                   # Quick start
```

- `internal/engine`: Now emits `EvalReport` on a channel; manager consumes it.
- `internal/manager`: Updates `alarms` and `alarm_events` on trip/clear; provides Ack/Silence/Clear ops.
- `internal/api`: REST endpoints.
- `internal/auth`: Minimal HS256 JWT validator; roles `viewer|operator|admin`.
- `cmd/jwtgen`: helper to mint demo tokens.
- `tools.go` pins CLI tool dependency versions.
    - Blank imports (`_ "module/path"`) to declare dependencies on CLI tools.
    - The tools build tag `//go:build tools tag` prevents the file from getting built into the app. It generates a durable list so `go.mod` can track the tool versions.
- `Taskfile.yml` defines fast, cross‑platform commands to run locally and in CI.


## Tooling
- **Go** 1.22+
- **golangci‑lint** v2.x (config in `.golangci.yml`)
- **gofumpt** + **goimports**
- **go-task**

    **Install the command runner (**Task**):**
    [Github go-task](https://github.com/go-task/task)
    [Taskfile.dev](https://taskfile.dev/)

    ```zsh
    npm install -g @go-task/cli
    ```

    or

    ```zsh
    go install github.com/go-task/task/v3/cmd/task@latest
    ```

### To add the Go bin directory to the shell PATH run:

```zsh
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

### Install the tool binaries at the pinned versions

```zsh
go install golang.org/x/tools/cmd/goimports@$(go list -f '{{.Version}}' -m golang.org/x/tools)
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(go list -f '{{.Version}}' -m github.com/golangci/golangci-lint/v2)
go install gotest.tools/gotestsum@$(go list -f '{{.Version}}' -m gotest.tools/gotestsum)
go install golang.org/x/vuln/cmd/govulncheck@$(go list -f '{{.Version}}' -m golang.org/x/vuln)
```

## Install toolchain and run:

```bash
go mod tidy
golangci-lint run
go test ./...
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

```bash
golangci-lint run
```

- Runs golangci-lint (a fast meta‑linter that aggregates many linters).

#### Test (concise, readable output):

```zsh
task test
```

```bash
go test -v ./tests/e2e -run TestE2E_Httptest_M3
# Optional
go test -race ./...
```

- Unit tests: engine rule logic (threshold, duration, ROC).
- E2E (httptest): seeds DB, starts in‑proc server, ingests points, verifies alarm FSM.
- `gotestsum --format=short-verbose -- ./...` runs all package tests and prints a compact per‑test summary.


#### Vulnerability scan (supply‑chain + code paths):

```zsh
task vuln
```

- `govulncheck` consults Go’s vuln DB and reports reachable vulnerabilities, reducing false positives.

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


## Helper CLI `./cmd/alarmctl` to exercise the storage layer.

- **Threshold** (lo 23.5V, hysteresis 0.2V, latched):
  ```bash
  go run ./cmd/alarmctl rule create \
    --name BatteryLow \
    --signal Battery.Voltage \
    --type threshold \
    --severity warn \
    --latch \
    --config-json '{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}'
  ```

- **ROC** (Temp rising faster than 0.15 °C/s over a 1s window):
  ```bash
  go run ./cmd/alarmctl rule create \
    --name TempRiseFast \
    --signal Temp.C \
    --type roc \
    --severity warn \
    --config-json '{"window_ms":1000,"gt":0.15,"abs":true}'
  ```

- **Duration** (Pressure > 1.2 for at least 3s):
  ```bash
  go run ./cmd/alarmctl rule create \
    --name PressureHighSustain \
    --signal Pressure.Bar \
    --type duration \
    --severity crit \
    --config-json '{"op":">","threshold":1.2,"duration_ms":3000,"hysteresis":0.05}'
  ```

## Project Goals

**Primary goals**

1. **Detect out‑of‑limits conditions** on incoming telemetry in near‑real time (thresholds, rate‑of‑change, hysteresis, latching).
2. **Manage alarm lifecycle**: raise, acknowledge, silence, clear; preserve a complete audit trail.
3. **Trigger runbook actions** via reliable webhooks (e.g., Slack/Jira/HTTP), with retries and idempotency.
4. **Expose a minimal, secure API** and metrics for operations and dashboards (Prometheus/Grafana).
5. **Be deployable anywhere** (Docker/K8s/single binary) with clear configuration and a deterministic demo.

**Success criteria (demo‑ready)**

- Sustains **≥2k points/sec** on a laptop with **p50 alarm decision latency ≤50 ms** and **no data loss**.
- Prometheus **/metrics** and **Grafana dashboard** show ingest rate, rule hit rate, active alarms, MTTA.
- End‑to‑end runbook webhook fires within **≤500 ms p95** of condition detection (with retries on failure).
- **Audit log** shows who/what/when for rules and alarms; **JWT‑protected** endpoints; **healthz/readyz** OK.

**Non‑goals (for this phase)**

- Full UI (a minimal built‑in HTML status page is optional; Grafana covers visibility).
- Distributed clustering/sharding; multi‑tenant RBAC beyond simple roles.
- Time‑series storage/queries at scale (we evaluate rules in‑memory and persist alarm/audit records only).

---

## Scope & Declarative Requirements

**Ingest**

- **MUST** accept telemetry via POST /v1/ingest (JSON newline or array). Each point: {name, value, ts?}.
- **SHOULD** include a built‑in generator (--gen.rate, --gen.pattern) for deterministic demos.
- **MUST** tolerate burstiness; **MUST NOT** block the caller on rule execution (bounded queue + back‑pressure metric).

**Rule engine**

- **MUST** support rule types:
    - **LO/HI** thresholds with **hysteresis** and **deadband**.
    - **Rate‑of‑change (ROC)** over sliding window.
    - **Duration** (condition must hold for **T** to trip).
    - **Latching** (requires explicit clear) vs **auto‑clear**.
- **MUST** allow rules configured via **YAML or REST**; live reload without restart.
- **MUST** execute deterministically per point using **event time** (ts) with server‑time fallback.

**Alarms**

- **MUST** implement states: inactive → active → acked → (silenced)? → cleared.
- **MUST** record **every transition** with who/why (audit table).
- **MUST** deduplicate identical alarms (same rule, entity) and track **first‑seen** and **last‑seen** timestamps.

**Actions**

- **MUST** support **webhook actions** with:
    - Exponential backoff + jitter.
    - **Idempotency keys** to avoid duplicate effects.
    - Dead‑letter capture after N failures.
- **SHOULD** provide Slack‑compatible payload and generic JSON webhook.

**APIs & security**

- **MUST** provide:
    - GET/POST /v1/rules
    - GET /v1/alarms (filters)
    - POST /v1/alarms/:id/ack, /silence, /clear
    - GET /healthz, /readyz, /metrics, /debug/pprof/
- **MUST** secure with **JWT bearer** (HS256 for demo; pluggable mTLS later).
- **MUST** log structured JSON with correlation IDs.

**Persistence**

- **MUST** persist: rules (versioned), alarms, alarm events, actions, webhook attempts.
- **SHOULD** use **SQLite** by default; support **Postgres** via DSN switch.
- **MUST** run schema migrations automatically on startup.

**Observability**

- **MUST** export Prometheus metrics (ingest rate, queue depth, evaluation latency, action success/failures).
- **SHOULD** emit OpenTelemetry traces for ingest → evaluate → action.

**Operations**

- **MUST** build as a static binary; **MUST** ship Docker image (multi‑stage, non‑root, minimal).
- **SHOULD** include Helm chart & sample Grafana dashboard JSON.

---

## High‑Level System Design

**Component overview**

- **API & Ingest Server** (net/http / chi): validates input, enqueues points, exposes REST, health, metrics, pprof.
- **Evaluation Pipeline**: goroutine pool consuming from a bounded channel:
    - **Point Router** groups by signal name/entity.
    - **Rule Engine** evaluates configured rules per point (uses per‑rule state).
    - **Alarm Manager** applies state machine & dedup; persists transitions.
- **Action Dispatcher**: async worker that reads alarm events, executes webhooks, handles retries/DLQ.
- **Persistence**: DB adapter (SQLite/Postgres) + migrations.
- **Config Loader**: merges YAML files and DB‑stored rules; supports hot reload.
- **Observability**: Prometheus metrics + optional OTel tracing.

**ASCII architecture**

```
           +------------------+       +-------------------+
POST /ingest -->  API Server  | ----> |  Ingest Queue     | (bounded chan)
           +---------+--------+       +---------+---------+
                     |                          |
                     v                          v
              +------+------+            +------+------+
              | Point Router |--grp-->   | Rule Engine | --results-->
              +------+------+            +------+------+
                     |                          |
                     v                          v
                +----+---------------------------+----+
                |          Alarm Manager (FSM)        |
                |  persist alarms & events (DB)       |
                +----+---------------------------+----+
                     |                          |
                     v                          v
           +---------+--+              +--------+----------+
           |  Metrics   |              | Action Dispatcher |
           | /healthz   |              |  (webhooks/DLQ)   |
           +------------+              +-------------------+
```

**Key data entities (high‑level)**

- Rule: id, name, signal selector, type (LO/HI/ROC/DUR), params, hysteresis, deadband, window, duration, latch, enabled, version, created_by.
- Alarm: id, rule_id, entity, state, first_seen, last_seen, acked_by, silenced_until.
- AlarmEvent: id, alarm_id, type (activated/acked/silenced/cleared), at, actor, metadata (reason, values).
- ActionAttempt: id, alarm_event_id, endpoint, payload_hash, idempotency_key, status, attempts, last_error.

**Concurrency model**

- One **ingest queue** (bounded) to decouple HTTP from evaluation.
- **N workers** (configurable) evaluate rules; **per‑signal locks** avoided by using **per‑rule state** stored in a sync.Map keyed by (rule_id, entity).
- The **alarm manager** is invoked inline per evaluation result but writes to DB via a small **batched writer** (to reduce I/O) protected by a chan + single writer goroutine (preserves ordering).
- The **action dispatcher** subscribes to alarm events via a DB notification table/queue.

**State machine (alarm lifecycle)**

```
inactive --(condition true for duration)--> active
active --(ack)--> acked
active/acked --(silence with TTL)--> silenced (auto-expire → prior state)
active/acked --(clear when condition false & unlatch)--> inactive
active/acked --(clear when condition false & latch=false)--> inactive
```


## Notes for contributors
- Structured logs via slog (JSON).
- Strict error handling (check/propagate; avoid swallowing).
- Graceful shutdown and HTTP timeouts are in place.

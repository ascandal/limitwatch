# LimitWatch — Real-Time Limits, Alarms & Runbook Actioner

**LimitWatch** is a **_Go_** service for evaluating live telemetry against rules and generating alarm state (trip/hold/clear) in near‑real time.

- Fast, robust ingest: Bounded queue prevents slow evaluation from blocking clients; fast 503 on backpressure.
- Idiomatic, testable rule logic: Clean, per‑rule state enables hysteresis, duration, and ROC without DB round‑trips.
- Reproducible demos: Built‑in generator with seed & patterns; unit tests cover key rule semantics.
- Production‑style scaffolding: Docker, Helm, migrations, structured logs, timeouts, graceful shutdown.

## Implemented
- Phase 0 — Scaffold (M0 ✅)
    - Minimal HTTP server (alarmd) with `/healthz`, `/readyz`, `/version`, optional `/debug/pprof/*` (gated).
    - JSON structured logging (slog), graceful shutdown, timeouts.
    - Multi‑stage Dockerfile (distroless, non‑root) and Helm skeleton (probes wired).
    - CI workflow prepared (`fmt/vet/staticcheck/tests/build`) — you chose to keep CI off for now.
- Phase 1 — Core domain & storage (M1 ✅)
    - Domain types: Rule, Alarm, AlarmEvent with enums and sentinel errors.
    - Storage: embedded SQL migrations, SQLite (CGO‑free) as default DB via database/sql.
    - Repository layer: interfaces + SQLite impl (create/list/get for rules/alarms; events create/list).
    - Temporary CLI (`alarmctl`) to create/list rules and alarms (developer/admin tool to exercise storage).
- Phase 2 — Ingest path & rule engine (M2 ✅)
    - POST `/v1/ingest` endpoint (accepts object or array of points), validation, size‑limited body.
    - Bounded ingest queue (non‑blocking enqueue; returns 503 when full).
    - Rule engine with per‑rule/per‑entity state:
    - Threshold (LO/HI) with hysteresis, deadband, optional duration.
    - Duration (>, <) with dwell time and hysteresis‑like clear.
    - ROC (rate‑of‑change) over a sliding window (absolute or signed), optional deadband.
    - Deterministic generator (optional) to produce realistic streams for demo and reproducibility.
    - Unit tests for rule logic (threshold, duration, ROC).

### Not yet built (Phase‑3+)
- alarm FSM persistence & API (ack/silence/clear)
- runbook actions/webhooks
- metrics dashboards (Grafana)
- auth/JWT plumbing for APIs
- config validation UI

---

## How the program works (today)

### 1) Boot & configuration
- **alarmd** starts and reads env vars (e.g., PORT, DB_DSN, INGEST_QUEUE_SIZE, WORKERS).
- Opens the DB and runs migrations (creates rules, alarms, alarm_events tables).
- Loads enabled rules from the DB and compiles them for fast evaluation.
- Initializes the bounded queue and N workers (default `runtime.NumCPU()`).
- (Optional) Starts the deterministic generator for demo: sends Battery.Voltage, Temp.C, etc.
    - TODO: move this to a separate fake signal generator.

### 2) Ingest path
- Clients send telemetry to POST `/v1/ingest`.
    - Accepts either a single or multiple telemetry points (**JSON** format).
    - Validates each point.
        - non‑empty name
        - finite value
        - fallback timestamp to `now`
        - fallback entity to `"default"`
    - Attempts non‑blocking enqueue.
        - If full, immediately responds `503 Service Unavailable`.
    - On success, returns `202 Accepted` with `{"ingested": N}` and current `X-Queue-Len`.

### 3) Evaluation
- Workers read points from the queue.
- Based on the point’s name, the engine finds all applicable rules.
- For each rule:
    - Retrieves the state bucket for (rule_id, entity).
    - Runs the rule’s evaluation function (threshold, duration, ROC).
    - Updates the Active flag in the state (trip, hold, clear).
    - (Optional) emits a verbose trace log (trip, hold, clear) when `TRACE_ENABLED=true`.

> Note: Phase‑2 keeps alarm state in memory only. Phase‑3 feeds evaluation results into a persistent alarm FSM, produce Alarm + AlarmEvent records, and expose the REST APIs to ack/silence/clear.

### 4) Observability & Ops (so far)
- Health endpoints: `/healthz`, `/readyz`.
- Build info: `/version` (version/commit/date via ldflags).
- pprof: `/debug/pprof/*` is disabled by default; enable with `PPROF_ENABLED=true` for local profiling.
- Deterministic traces (Phase‑2) for quick inspection; Grafana/Prometheus metrics come later.

---

## How to run a quick end‑to‑end demo
1. Start the service (migrations run automatically):

    ```bash
    TRACE_ENABLED=true go run ./cmd/alarmd
    ```

2. Create some rules (with the Phase‑1 CLI):

    - Threshold: low battery with hysteresis, deadband, dwell

    ```bash
    go run ./cmd/alarmctl rule create \
        --name BatteryLow \
        --signal Battery.Voltage \
        --type threshold \
        --severity warn \
        --latch \
        --config-json '{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}'
    ```

    - ROC: temperature rising faster than 0.15 °C/s over 1s

    ```bash
    go run ./cmd/alarmctl rule create \
      --name TempRiseFast \
      --signal Temp.C \
      --type roc \
      --severity warn \
      --config-json '{"window_ms":1000,"gt":0.15,"abs":true}'
    ```

3. Use the generator (reproducible):

    ```bash
    GEN_ENABLED=true GEN_PATTERN=step GEN_RATE_HZ=10 GEN_SEED=42 TRACE_ENABLED=true \
      go run ./cmd/alarmd
    ```

4. Or post your own points:

    ```bash
    curl -s -X POST localhost:8080/v1/ingest \
        -H 'content-type: application/json' \
        -d '{"name":"Battery.Voltage","value":23.3,"entity":"SC-1"}'
    ```

5. Run tests:

    ```bash
    go test ./...
    ```

---

## Current configuration (common env vars)
- **Server:** PORT (default 8080), PPROF_ENABLED (default false)
- **DB:** DB_DSN (default file:limitwatch.db?... with WAL & busy timeout)
- **Engine:** INGEST_QUEUE_SIZE (default 4096), WORKERS (default NumCPU), TRACE_ENABLED (default false)
- **Generator:** GEN_ENABLED, GEN_PATTERN=step|sine|ramp|noise, GEN_RATE_HZ (default 10), GEN_SEED (default 42)

---

## Known limitations to be addressed
- Alarm persistence & lifecycle are not wired to the engine yet (that’s Phase‑3).
- No auth for ingest or rule ops (we’ll add JWT/mTLS in the API phase).
- No metrics/dashboarding yet (Prometheus + Grafana arrive in Phase‑5).
- No web UI (CLI and curl are enough for early phases; basic UI comes later).

---


## Run Locally

```bash
go mod tidy
```

### Start the server

With **pprof** disabled:

```bash
go run ./cmd/alarmd
```

Or with **pprof** enabled for local debugging `/debug/pprof/`:

```bash
PPROF_ENABLED=true go run ./cmd/alarmd
```


### In another terminal
```bash
curl -sf localhost:8080/healthz
```
> Returns: `ok`

```bash
curl -sf localhost:8080/readyz
```
> Returns: `ready`

```bash
curl -sf localhost:8080/version
```
> Returns: `{"service":"alarmd", "version":"dev", "commit":"none", "date":"unknown"}`


### Build (with version info):

```bash
make build VERSION=0.0.1
./bin/alarmd
```

**Makefile (common tasks):**
- make fmt – format code (writes in place).
- make vet – go vet.
- make staticcheck – installs and runs staticcheck if missing.
- make test – go test -race ./...
- make build – builds bin/alarmd with ldflags (Version, Commit, Date).
- make run – runs the service on port 8080.
- make docker – builds a container image named limitwatch:<VERSION>.

### Docker Image

```bash
make docker VERSION=0.0.1
docker run -p 8080:8080 limitwatch:0.0.1
```

**Container image (deploy/docker/Dockerfile)**
- **Builder:** golang:1.25.4, CGO disabled, trims path; injects version/commit/date via ldflags.
- **Runtime:** distroless/static:nonroot (no shell, minimal attack surface).
- **User:** Runs as nonroot.
- **Port:** Exposes port 8080.

#### Build and run
```bash
# Build with metadata
docker build -t limitwatch:dev \
  --build-arg VERSION=0.0.1 \
  --build-arg COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo none) \
  --build-arg BUILDDATE=$(date -u +'%Y-%m-%dT%H:%M:%SZ') \
  -f deploy/docker/Dockerfile .

# Run it
docker run --rm -p 8080:8080 limitwatch:dev
# Then hit http://localhost:8080/healthz, /readyz, /version
```


### Helm chart skeleton (deploy/helm)
- Chart.yaml: metadata (v0.1.0 chart, appVersion 0.0.1 placeholder).
- values.yaml:
    - image.repository: ghcr.io/ascandal/limitwatch
    - image.tag: latest
    - service.port: 80 (front door) → maps to container 8080
    - env.PORT: "8080"
- templates/deployment.yaml:
    - Probes: readiness → /readyz, liveness → /healthz
    - Sets PORT env from values
- templates/service.yaml:
    - ClusterIP on port 80 → targetPort 8080

**Install (run from repo root, once image is pushed):**
```bash
helm install limitwatch ./deploy/helm \
  --set image.repository=ghcr.io/ascandal/limitwatch \
  --set image.tag=0.0.1
```


### CI (Github Actions)

CI workflow for formatting, vetting, static analysis, tests, and build.

Runs on **Push** and **Pull Request**:
1. Formatting check (`gofmt -s -l .`)
    1. Fails if files need formatting
1. Static checks
    1. From the Go toolchain (`go vet ./...`)
    2. broader static analysis (`staticcheck ./...`)
1. Tests (`go test -race ./...`)
1. Build (`go build ./cmd/alarmd`)



## Structure
```text
cmd/alarmd/            # main entrypoint
internal/version/      # build-time version info
deploy/docker/         # Dockerfile
deploy/helm/           # (placeholder) helm chart skeleton
grafana/               # (placeholder) dashboard artifacts
.github/workflows/     # CI pipeline
```

## Notes
- Default port is `8080` (env `PORT` to override).
- `/debug/pprof` endpoints are enabled for local profiling.
- CI enforces `gofmt`, `go vet`, `staticcheck`, `go test -race`, and build.
- This is a minimal starting point intended to be extended in the next phases.

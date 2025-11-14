# Phase 2 — Ingest path & Rule Engine

This phase adds:
- `/v1/ingest` HTTP endpoint (object or array of points).
- Bounded ingest queue (non-blocking enqueue; returns 503 when full).
- Rule engine supporting:
  - **threshold** (LO/HI) with **hysteresis**, **deadband**, and **duration**.
  - **duration** with operator (`>` or `<`) and **hysteresis** for clear.
  - **ROC** (rate-of-change) over a sliding window with optional absolute value.
- Per-rule, per-entity state (hysteresis and windows).
- Optional deterministic **generator** for demo (`GEN_ENABLED=true`).
- Unit tests for threshold, duration, and ROC evaluation functions.
- Milestone M2: Deterministic demo via generator → engine; unit tests green.

## Run the service

```bash
go run ./cmd/alarmd
# Optional: enable generator for a self-contained demo
GEN_ENABLED=true GEN_PATTERN=step GEN_RATE_HZ=10 GEN_SEED=42 go run ./cmd/alarmd
```

## Ingest API

- **POST** `/v1/ingest` with either a single object or an array:
  ```json
  { "name": "Battery.Voltage", "value": 23.4, "ts": "2025-01-01T00:00:00Z", "entity": "SC-1" }
  ```
  or
  ```json
  [
    { "name": "Battery.Voltage", "value": 23.4, "ts": "2025-01-01T00:00:00Z", "entity": "SC-1" },
    { "name": "Temp.C", "value": 20.1, "entity": "SC-1" }  // ts default = now
  ]
  ```
- Returns `202 Accepted` and `{"ingested": N}` on success.
- Adds `X-Queue-Len` header with current queue length.
- Returns `503 Service Unavailable` if the bounded queue is full.

## Configure via env

- `INGEST_QUEUE_SIZE` (default **4096**): bounded queue capacity.
- `WORKERS` (default **NumCPU**): number of evaluation workers.
- `TRACE_ENABLED` (default **false**): log engine traces (trip/clear/hold).
- `GEN_ENABLED` (default **false**): start built-in generator for demo.
- `GEN_PATTERN` (default **step**): one of `step|sine|ramp|noise`.
- `GEN_RATE_HZ` (default **10**): generator points per second.
- `GEN_SEED` (default **42**): deterministic seed for reproducibility.

## Prepare rules (examples)

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

## Quick demo flow (Milestone M2)

1. Start DB-backed server (migrations run automatically):
   ```bash
   go run ./cmd/alarmd
   ```
2. Create the rules above using `alarmctl`.
3. Enable the deterministic generator (or post points with curl):
   ```bash
   GEN_ENABLED=true GEN_PATTERN=step GEN_RATE_HZ=10 GEN_SEED=42 go run ./cmd/alarmd
   ```
4. Watch the logs for **trip/clear/hold** traces (enable `TRACE_ENABLED=true` for verbose traces).
5. (Optional) POST your own points:
   ```bash
   curl -s -X POST localhost:8080/v1/ingest \
    -H 'content-type: application/json' \
    -d '{"name":"Battery.Voltage","value":23.3,"entity":"SC-1"}'
   ```

## Tests

```bash
go test ./...    # includes engine unit tests
```

The tests cover:
- Threshold: duration + hysteresis clear.
- Duration: sustained condition with hysteresis on clear.
- ROC: slope-based detection and clear.

# LimitWatch - Real-Time Limits, Alarms & Runbook Actioner

**LimitWatch** is a small, secure **Go** service that ingests telemetry, evaluates configurable rules, raises alarms, and manages their lifecycle via a simple HTTP API. It’s designed to run as a compact, production‑style container on a laptop, VM, or Kubernetes.

## Provides

- **Near‑real‑time detection**
  Fast, deterministic evaluation of out‑of‑limits conditions with bounded latency under bursty loads. Supports:
  - Threshold rules (LO/HI) with hysteresis, deadband, and optional duration.
  - Duration rules (condition must hold for N ms).
  - Rate‑of‑change (ROC) rules over sliding windows.

- **Operational control**
  Alarm lifecycle with role‑based access and full auditability:
  - `inactive → active → acked → silenced/cleared`.

- **Easy integration**
  Simple HTTP ingest and JSON APIs, minimal dependencies (SQLite by default), and sensible defaults.

## Representative Use Cases

In mission‑critical environments (e.g., spacecraft ground operations), minutes matter. LimitWatch reduces **time‑to‑detect** and **time‑to‑act**, enforces consistent responses, and provides the traceability operations teams need during reviews and anomaly investigations. It serves as a guardrail for human error and a foundation for higher‑level automation.

- **Spacecraft Operations:**
    - Trip an alarm when **battery voltage** drops below a safe threshold with hysteresis to avoid flap.
    - Detect **thermal runaway** using rate‑of‑change rules; auto‑notify the on‑call channel.
    - Monitor **pressurization** parameters that must remain out‑of‑bounds for N seconds before tripping.

- **Ground Systems:**
    - Alert on **downlink gap** duration or **packet loss** spikes; trigger a playbook to switch antennas or increase buffering.
    - Watch **service health** (latency, queue depth) and open a ticket automatically when limits are exceeded.

- **General Operations:**
    - Evaluate sensor streams on test stands or hardware‑in‑the‑loop rigs and execute standard operating procedures on excursions.

## Getting started

### Prereqs

- Go `1.22+` installed.

### 1) (Optional) Seed Rules

`cmd/alarmctl` is a **developer/testing** helper. Prefer the REST API for normal operations. If you create rules via the API, restart `alarmd` for them to take effect (engine loads rules at startup).

For a quick local demo, you can seed a rule before starting the server:

```bash
go run ./cmd/alarmctl rule create \
  --name BatteryLow \
  --signal Battery.Voltage \
  --type threshold \
  --severity warn \
  --latch \
  --config-json '{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}'
```

### 2) Start the server

```bash
go run ./cmd/alarmd
```

Check health:

```bash
curl -sf localhost:8080/healthz
curl -sf localhost:8080/readyz
curl -sf localhost:8080/version
```

### 3) Ingest telemetry

Use HTTP to send telemetry manually or from an external service:

```bash
curl -s -X POST localhost:8080/v1/ingest \
  -H 'content-type: application/json' \
  -d '{"name":"Battery.Voltage","value":23.3,"entity":"SC-1"}'
```

- Returns `202 Accepted` and `{"ingested": N}` on success.
- Adds `X-Queue-Len` header with current queue length.
- Returns `503 Service Unavailable` if the bounded queue is full.

### 4) View alarms and act on them

List alarms:

```bash
curl -s http://localhost:8080/v1/alarms | jq .
```

Acknowledge / silence / clear (with auth if enabled, see below):

```bash
ALARM_ID=$(curl -s http://localhost:8080/v1/alarms | jq -r '.[0].id')

curl -s -X POST http://localhost:8080/v1/alarms/$ALARM_ID/ack
curl -s -X POST -H 'content-type: application/json' -d '{"ttl_sec":120}' http://localhost:8080/v1/alarms/$ALARM_ID/silence
curl -s -X POST http://localhost:8080/v1/alarms/$ALARM_ID/clear
```

> In a real deployment you’ll typically drive these via a UI or automation, not by hand.

## Authentication (optional)

LimitWatch supports two auth modes:

- **Dev mode (default)**
  `JWT_SECRET` is **unset** → no tokens required; all callers are treated as admin. Good for local development.

- **Auth mode**
  `JWT_SECRET` is **set** → JWT tokens required; roles enforced:
  - `viewer` — read‑only (GET).
  - `operator` — ack/silence/clear.
  - `admin` — create rules.

To enable auth and mint tokens:

```bash
export JWT_SECRET="devsecret"

go run ./cmd/jwtgen -sub ops@org_name -role admin -secret "$JWT_SECRET" -exp 1h > admin.jwt
go run ./cmd/jwtgen -sub ops@org_name -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
```

Use them with:

```bash
-H "authorization: Bearer $(cat operator.jwt)"
```

## Demo Generator (Optional)

For quick demos you can enable a built‑in generator that feeds synthetic signals into the engine:

```bash
GEN_ENABLED=true GEN_PATTERN=step GEN_RATE_HZ=10 GEN_SEED=42 TRACE_ENABLED=true \
  go run ./cmd/alarmd
```

The generator produces simple patterns (`Battery.Voltage`, `Temp.C`) that exercise threshold and ROC rules.

## Known limitations

- **Rules reload:** engine loads rules at startup. Rules created via the REST API take effect after restart (no hot‑reload yet).
- **Ingest auth:** `/v1/ingest` is currently open (no JWT). Consider requiring at least `viewer` or a dedicated producer token in hardened deployments.

## Continue Reading
- [User Guide](./docs/USER-GUIDE.md) - running, config, and API examples.
- [Developer Guide](./docs/DEVELOPER-GUIDE.md) - repo layout, tooling, lint/test/e2e.
- [Architecture](./docs/ARCHITECTURE.md) - how the engine/manager/API fit together.
- [Auth](./docs/AUTHENTICATION.md) - JWT roles and examples.
- [Glossary](./docs/GLOSSARY.md) - key terms.

# LimitWatch - Real-Time Limits, Alarms & Runbook Actioner

**LimitWatch** is a small and secure HTTP API **Go** service that ingests telemetry, evaluates configurable rules, raises alarms, manages their lifecycle, and triggers runbook actions.

It’s a compact, production-style container that can be deployed on a laptop, VM, or Kubernetes.

## Provides

- **Near‑real‑time detection:** Fast and deterministic evaluation of out‑of‑limits conditions with bounded latency under bursty loads.
    - **Configurable Rules:** threshold, duration, and rate-of-change (ROC) with hysteresis and latching.
- **Operational control:** Alarm state lifecycle with role‑based access and full auditability.
    - active → acked (acknowledged) → silenced/cleared
- **Reliable automation:** Idempotent webhooks with retries, backoff, and dead‑letter handling reduce noisy or lost actions.
- **Easy integration & visibility:** Simple HTTP ingest, /metrics for Grafana dashboards, and minimal dependencies (SQLite/Postgres).

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

## Getting Started

### Prereqs
- Go `1.22+` installed.

### 1) Create Rules (admin)

CLI (`alarmctl`) is provided to exercise the storage layer.

```bash
go run ./cmd/alarmctl rule create \
  --name BatteryLow \
  --signal Battery.Voltage \
  --type threshold \
  --severity warn \
  --latch \
  --config-json '{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}'
```

```bash
curl -s -H "authorization: Bearer $(cat admin.jwt)"          -H 'content-type: application/json'          -d '{"name":"BatteryLow","signal":"Battery.Voltage","type":"threshold","severity":"warn","latch":true,"enabled":true,"config":{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}}'          http://localhost:8080/v1/rules | jq .
```

> Note: Engine loads rules at startup.

**List rules:**
```bash
go run ./cmd/alarmctl rule list
```

### 2) Create Alarms

```bash
go run ./cmd/alarmctl alarm create --rule-id 1 --entity SC-1 --state active --note "Triggered by test"
```

**List alarms:**
```bash
go run ./cmd/alarmctl alarm list
```

### 3) Start the Server

```bash
go run ./cmd/alarmd
```

```bash
export JWT_SECRET="devsecret"   # enable JWT
go run ./cmd/jwtgen -sub ops@org_name -role admin -secret "$JWT_SECRET" -exp 1h > admin.jwt
go run ./cmd/jwtgen -sub ops@org_name -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
TRACE_ENABLED=true GEN_ENABLED=true GEN_PATTERN=step GEN_RATE_HZ=10 GEN_SEED=42          go run ./cmd/alarmd
```

Open a separate terminal and run health checks:

```bash
curl -sf localhost:8080/healthz
curl -sf localhost:8080/readyz
```

### 4) Ingest telemetry point(s)

Use to send telemetry manually or from external service:

```bash
curl -s -X POST localhost:8080/v1/ingest \
  -H 'content-type: application/json' \
  -d '{"name":"Battery.Voltage","value":23.3,"entity":"SC-1"}'
```

- Returns `202 Accepted` and `{"ingested": N}` on success.
- Adds `X-Queue-Len` header with current queue length.
- Returns `503 Service Unavailable` if the bounded queue is full.

> Note: a fake signal generator is provided for testing (`internal/generator`).

### 5) Watch Alarms (optional JWT)

```bash
curl -s -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms | jq .
```

By default (dev mode), JWT is off.

To require tokens:

```bash
export JWT_SECRET="devsecret"
go run ./cmd/jwtgen -sub ops@example -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
curl -s -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms | jq .
```

### 6) Actions on Alarm State (Ack, silence, clear):

```bash
ALARM_ID=$(curl -s -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms | jq -r '.[0].id')
curl -s -X POST -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms/$ALARM_ID/ack | jq .
curl -s -X POST -H "content-type: application/json" -H "authorization: Bearer $(cat operator.jwt)"          -d '{"ttl_sec":120}' http://localhost:8080/v1/alarms/$ALARM_ID/silence | jq .
curl -s -X POST -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms/$ALARM_ID/clear | jq .
```


> Note: Newly created rules via the API will take effect after restart (engine currently compiles rules at startup).

## Continue Reading
- [User Guide](./docs/USER-GUIDE.md) - running, config, and API examples
- [Developer Guide](./docs/DEVELOPER-GUIDE.md) - repo layout, tooling, lint/test/e2e
- [Architecture](./docs/ARCHITECTURE.md) - how the engine/manager/API fit together
- [Auth](./docs/AUTHENTICATION.md) - JWT roles and examples
- [Roadmap](./docs/ROADMAP.md) - phases and milestones
- [Glossary](./docs/GLOSSARY.md) - key terms

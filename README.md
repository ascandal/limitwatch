# LimitWatch - Real-Time Limits, Alarms & Runbook Actioner

LimitWatch ingests telemetry, evaluates rules and manages alarms with a small, secure HTTP API. It’s a compact, production-style building block that can run on a laptop, VM, or Kubernetes.

**Provides:**
- **Near real-time detection** of out‑of‑limits conditions with bounded latency.
    - Rules: threshold, duration, rate-of-change with hysteresis and latching
- **Operational control**: ack/silence/clear with audit trail and roles.
    - Alarm States: active → acked → silenced/cleared
- **Simple integration**: HTTP ingest + minimal dependencies (SQLite by default).

## Getting Started

### Prereqs
- Go `1.22+` installed.

### 1) Create Rules (engine loads rules at startup)

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

> Note: a fake signal generator is provided for testing (`internal/generator`).

### 5) View Alarms (optional JWT)

By default (dev mode), JWT is off.

To require tokens:

```bash
export JWT_SECRET="devsecret"
go run ./cmd/jwtgen -sub ops@example -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
curl -s -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms | jq .
```

> Note: Newly created rules via the API will take effect after restart (engine currently compiles rules at startup).

## Continue Reading
- [User Guide](./docs/USER-GUIDE.md) - running, config, and API examples
- [Developer Guide](./docs/DEVELOPER-GUIDE.md) - repo layout, tooling, lint/test/e2e
- [Architecture](./docs/ARCHITECTURE.md) - how the engine/manager/API fit together
- [Auth](./docs/AUTHENTICATION.md) - JWT roles and examples
- [Roadmap](./docs/ROADMAP.md) - phases and milestones
- [Glossary](./docs/GLOSSARY.md) - key terms

# LimitWatch

**Real‑Time Limits, Alarms & Runbook Actioner**

LimitWatch ingests telemetry, evaluates rules (threshold, duration, rate‑of‑change with hysteresis/latching), and manages alarms (active → acked → silenced/cleared) with a small, secure HTTP API. It’s a compact, production‑style building block that can run on a laptop, VM, or Kubernetes.

**Provides:**
- **Near‑real‑time detection** of out‑of‑limits conditions with bounded latency.
- **Operational control**: ack/silence/clear with audit trail and roles.
- **Simple integration**: HTTP ingest + minimal dependencies (SQLite by default).

## Getting Started

### Prereqs
- Go `1.22+` installed.

### 1) Seed a Rule (engine loads rules at startup)
```bash
go run ./cmd/alarmctl rule create \
  --name BatteryLow \
  --signal Battery.Voltage \
  --type threshold \
  --severity warn \
  --latch \
  --config-json '{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}'
```

### 2) Start the Server

```bash
go run ./cmd/alarmd
# Health checks
curl -sf localhost:8080/healthz
curl -sf localhost:8080/readyz
```

### 3) Send a Point

```bash
curl -s -X POST localhost:8080/v1/ingest \
  -H 'content-type: application/json' \
  -d '{"name":"Battery.Voltage","value":23.3,"entity":"SC-1"}'
```

### 4) View Alarms (optional JWT)

By default (dev mode), JWT is off. To require tokens:

```bash
export JWT_SECRET="devsecret"
go run ./cmd/jwtgen -sub ops@example -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
curl -s -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms | jq .
```

> Note: Newly created rules via the API will take effect after restart (engine currently compiles rules at startup).

## Continue Reading
- User Guide – running, config, and API examples: USER-GUIDE.md
- Developer Guide – repo layout, tooling, lint/test/e2e: DEVELOPER-GUIDE.md
- Architecture – how the engine/manager/API fit together: ARCHITECTURE.md
- Auth – JWT roles and examples: AUTHENTICATION.md
- Roadmap – phases and milestones: ROADMAP.md
- Glossary – key terms: GLOSSARY.md

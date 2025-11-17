# User Guide

> **Note:** `cmd/alarmctl` is a **developer/testing** helper. For normal use, prefer the REST API. Because the engine loads rules at startup, rules created via the API take effect **after you restart** the server. For quick local demos, you can seed rules with `alarmctl` **before** starting the server.

## Start the server with defaults (SQLite file:limitwatch.db)

```bash
go run ./cmd/alarmd
```

- Database will be read from `limitwatch.db` in the current working directory.
- Migrations are run implicitly on startup (`alarmd`) and from the CLI (`alarmctl`).
- Set `TRACE_ENABLED=true` for verbose evaluation traces in logs.

## Check Health

```bash
curl -sf localhost:8080/healthz
curl -sf localhost:8080/readyz
curl -sf localhost:8080/version
```

## Configuration (env)

- **Server**
  - `PORT` — listen port (default `8080`).
  - `PPROF_ENABLED` — enable `/debug/pprof/*` when `true` (default `false`).

- **DB**
  - `DB_DSN` — database DSN (defaults to `file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`).
  - For SQLite, the default DSN enables WAL mode and a 5s busy timeout for better concurrency and fewer “database is locked” errors.

- **Engine**
  - `INGEST_QUEUE_SIZE` — bounded queue capacity (default `4096`).
  - `WORKERS` — number of evaluation workers (default `NumCPU`).
  - `TRACE_ENABLED` — when `true`, log trace entries for rule evaluations (trip/clear/hold).

- **Auth**
  - `JWT_SECRET` — when empty → **dev mode** (no tokens required). When set → JWT tokens required and roles enforced.

### Optional: Demo Generator (for local testing)

For quick demos, enable a built‑in generator that feeds synthetic signals (e.g., `Battery.Voltage`, `Temp.C`) into the engine:

- `GEN_ENABLED` (default **false**): start built‑in generator.
- `GEN_PATTERN` (default **step**): one of `step|sine|ramp|noise`.
- `GEN_RATE_HZ` (default **10**): points per second.
- `GEN_SEED` (default **42**): deterministic seed for reproducibility.

Example:

```bash
GEN_ENABLED=true GEN_PATTERN=step GEN_RATE_HZ=10 GEN_SEED=42 TRACE_ENABLED=true \
  go run ./cmd/alarmd
```

## Authentication (JWT)

**Two modes:**
- If `JWT_SECRET` is **unset** → dev mode (no tokens required).
- If `JWT_SECRET` is **set** → tokens required; role checks enforced.

- Roles:
  - `viewer` — GET endpoints only (e.g., list rules/alarms).
  - `operator` — ack/silence/clear alarms.
  - `admin` — create rules.

Use `cmd/jwtgen` to create tokens:

```bash
export JWT_SECRET="devsecret"
go run ./cmd/jwtgen -sub ops@example -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
```

Then call APIs with:

```bash
-H "Authorization: Bearer $(cat operator.jwt)"
```

> For more details see [Authentication](./AUTHENTICATION.md).

## Current limitation

The engine loads rules at startup. Rules created after startup (via API) take effect on restart. To try rules immediately, create them with `alarmctl` before starting the server. `alarmctl` is intended for development/testing; prefer the REST API in normal operations.

## API quick reference

| **Endpoint**                   | **Role**  | **Description**                                                       |
| ------------------------------ | --------- | --------------------------------------------------------------------- |
| POST `/v1/ingest`              | open      | Ingest telemetry point(s) (JSON). Returns `202` and `{"ingested":N}`. |
| GET `/v1/rules`                | viewer+   | List rules.                                                           |
| POST `/v1/rules`               | admin     | Create a rule. Takes JSON payload with rule config.                   |
| GET `/v1/alarms`               | viewer+   | List alarms (active/acked/silenced/inactive).                         |
| POST `/v1/alarms/{id}/ack`     | operator+ | Acknowledge an alarm.                                                 |
| POST `/v1/alarms/{id}/silence` | operator+ | Silence an alarm until time or TTL.                                   |
| POST `/v1/alarms/{id}/clear`   | operator+ | Clear an alarm (needed for latched alarms).                           |
| GET `/healthz`                 | open      | Liveness                                                              |
| GET `/readyz`                  | open      | Readiness                                                             |
| GET `/version`                 | open      | Build metadata (version/commit/date)                                  |
| GET `/debug/pprof/*`           | open      | Debug profiling endpoints (enable with `PPROF_ENABLED=true`)          |

## Examples

### Ingest

```bash
curl -s -X POST localhost:8080/v1/ingest \
  -H 'content-type: application/json' \
  -d '[{"name":"Battery.Voltage","value":23.4,"entity":"SC-1"}]'
```

### Create a rule (admin token required)

```bash
curl -s -H "authorization: Bearer $(cat admin.jwt)" \
  -H 'content-type: application/json' \
  -d '{"name":"BatteryLow","signal":"Battery.Voltage","type":"threshold","severity":"warn","latch":true,"enabled":true,"config":{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}}' \
  http://localhost:8080/v1/rules
```

> There is no `POST /v1/alarms` endpoint to create an alarm directly. Alarms are created automatically when incoming telemetry violates a configured rule.

### Ack / Silence / Clear

```bash
ALARM_ID=$(curl -s -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms | jq -r '.[0].id')

curl -s -X POST \
  -H "authorization: Bearer $(cat operator.jwt)" \
  http://localhost:8080/v1/alarms/$ALARM_ID/ack

curl -s -X POST \
  -H "content-type: application/json" \
  -H "authorization: Bearer $(cat operator.jwt)" \
  -d '{"ttl_sec":120}' \
  http://localhost:8080/v1/alarms/$ALARM_ID/silence

curl -s -X POST \
  -H "authorization: Bearer $(cat operator.jwt)" \
  http://localhost:8080/v1/alarms/$ALARM_ID/clear
```

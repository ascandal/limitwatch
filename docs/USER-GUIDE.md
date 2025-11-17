# User Guide

## Start the server with defaults (SQLite file:limitwatch.db)

```bash
go run ./cmd/alarmd
```

- Database will be read from file: `limitwatch.db`.
- Runs migrations implicitly by starting either the server or the CLI (both call `storage.Migrate`).
- Enable `TRACE_ENABLED=true` for verbose traces.

## Check Health

```bash
curl -sf localhost:8080/healthz
curl -sf localhost:8080/readyz
curl -sf localhost:8080/version
```

### Configuration (env)
- **Server:** `PORT` (8080), `PPROF_ENABLED` (false)
- **DB:** `DB_DSN` (SQLite DSN with WAL & busy timeout by default)
    - `DB_DSN` — database DSN (defaults to `file:limitwatch.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`).
    - For SQLite, the DSN above enables WAL mode and a 5s busy timeout for better concurrency and fewer "database is locked" errors.
- **Engine:** `INGEST_QUEUE_SIZE` (4096), `WORKERS` (NumCPU), `TRACE_ENABLED` (false)
- **Auth:** `JWT_SECRET` (empty → dev mode, tokens not required)

### NEE TO UPDATE THIS (OLD)
- `INGEST_QUEUE_SIZE` (default **4096**): bounded queue capacity.
- `WORKERS` (default **NumCPU**): number of evaluation workers.
- `TRACE_ENABLED` (default **false**): log engine traces (trip/clear/hold).
- `GEN_ENABLED` (default **false**): start built-in generator for demo.
- `GEN_PATTERN` (default **step**): one of `step|sine|ramp|noise`.
- `GEN_RATE_HZ` (default **10**): generator points per second.
- `GEN_SEED` (default **42**): deterministic seed for reproducibility.

## Authentication (JWT)
- If `JWT_SECRET` set → JWT required; if unset → dev mode (all calls act as admin).
- Roles:
    - viewer (GET)
    - operator (ack/silence/clear)
    - admin (create rules)
- Use `cmd/jwtgen` to create tokens:

    ```bash
    export JWT_SECRET="devsecret"
    go run ./cmd/jwtgen -sub ops@example -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
    ```

- Then call APIs with `Authorization: Bearer <token>`.

> For more details see [Authentication](./AUTHENTICATION.md).

## Current limitation

The engine loads rules at startup. Rules created after startup (via API) take effect on restart. To try rules immediately, create them with alarmctl before starting the server.

## API quick reference

| **Endpoint**                   | **Role**  | **Description**                                                     |
| ------------------------------ | --------- | ------------------------------------------------------------------- |
| POST `/v1/ingest`              | open      | Ingest telemetry point(s) (JSON). Returns 202 and `{"ingested":N}`. |
| GET `/v1/rules`                | viewer+   | List rules.                                                         |
| POST `/v1/rules`               | admin     | Create a rule. Takes JSON payload with rule config.                 |
| GET `/v1/alarms`               | viewer+   | List alarms (active/acked/silenced/inactive).                       |
| POST `/v1/alarms/{id}/ack`     | operator+ | Acknowledge an alarm.                                               |
| POST `/v1/alarms/{id}/silence` | operator+ | Silence an alarm until time or TTL.                                 |
| POST `/v1/alarms/{id}/clear`   | operator+ | Clear an alarm (needed for latched alarms).                         |
| GET `/healthz`                 | open      | Liveness                                                            |
| GET `/readyz`                  | open      | Readiness                                                           |
| GET `/version`                 | open      | Build metadata (version/commit/date)                                |
| GET `/debug/pprof/*`           | open      | Debug profiling endpoints (Enable with `PPROF_ENABLED=true`)        |

## Examples

### Ingest:

```bash
curl -s -X POST localhost:8080/v1/ingest \
  -H 'content-type: application/json' \
  -d '[{"name":"Battery.Voltage","value":23.4,"entity":"SC-1"}]'
```

### Create a rule (admin token required):

```bash
curl -s -H "authorization: Bearer $(cat admin.jwt)" \
  -H 'content-type: application/json' \
  -d '{"name":"BatteryLow","signal":"Battery.Voltage","type":"threshold","severity":"warn","latch":true,"enabled":true,"config":{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}}' \
  http://localhost:8080/v1/rules
```

### Ack / Silence / Clear:

```bash
ALARM_ID=$(curl -s -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms | jq -r '.[0].id')
curl -s -X POST -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms/$ALARM_ID/ack
curl -s -X POST -H "authorization: Bearer $(cat operator.jwt)" -H 'content-type: application/json' -d '{"ttl_sec":120}' http://localhost:8080/v1/alarms/$ALARM_ID/silence
curl -s -X POST -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms/$ALARM_ID/clear
```

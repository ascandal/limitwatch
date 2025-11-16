# User Guide

## Run

```bash
# Start with defaults (SQLite file:limitwatch.db)
go run ./cmd/alarmd

# Health
curl -sf localhost:8080/healthz
curl -sf localhost:8080/readyz
curl -sf localhost:8080/version
```

## Configuration (env)
- **Server:** PORT (8080), PPROF_ENABLED (false)
- **DB:** DB_DSN (SQLite DSN with WAL & busy timeout by default)
- **Engine:** INGEST_QUEUE_SIZE (4096), WORKERS (NumCPU), TRACE_ENABLED (false)
- **Auth:** JWT_SECRET (empty → dev mode, tokens not required)

## Authentication (JWT)
- If `JWT_SECRET` set → JWT required; if unset → dev mode (all calls act as admin).
- Roles:
    - viewer (GET)
    - operator (ack/silence/clear)
    - admin (create rules)
- Generate a token:

    ```bash
    export JWT_SECRET="devsecret"
    go run ./cmd/jwtgen -sub ops@example -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
    ```

> See more details in AUTHENTICATION.md.

## Current limitation

The engine loads rules at startup. Rules created after startup (via API) take effect on restart. To try rules immediately, create them with alarmctl before starting the server.

## API quick reference

| **Endpoint**                   | **Role**  | **Description**                                                 |
| ------------------------------ | --------- | --------------------------------------------------------------- |
| POST `/v1/ingest`              | open      | Send 1 or many points (JSON). Returns 202 and `{"ingested":N}`. |
| GET `/v1/rules`                | viewer+   | List rules.                                                     |
| POST `/v1/rules`               | admin     | Create a rule. Takes JSON payload with rule config.             |
| GET `/v1/alarms`               | viewer+   | List alarms (active/acked/silenced/inactive).                   |
| POST `/v1/alarms/{id}/ack`     | operator+ | Acknowledge an alarm.                                           |
| POST `/v1/alarms/{id}/silence` | operator+ | Silence until time or TTL.                                      |
| POST `/v1/alarms/{id}/clear`   | operator+ | Clear (needed for latched alarms).                              |

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

## Health & diagnostics

- GET `/healthz`, GET `/readyz`, GET `/version`
- GET `/debug/pprof/*` (enable with `PPROF_ENABLED=true`)

# Phase 3 — Alarm Manager & API

This phase wires the engine to a persistent alarm FSM and exposes REST APIs with JWT roles.
Milestone M3: **post rule → ingest points → see alarm → ack/clear via API**.

## New components

- `internal/engine`: now emits `EvalReport` on a channel; manager consumes it.
- `internal/manager`: updates `alarms`/`alarm_events` on trip/clear; provides Ack/Silence/Clear ops.
- `internal/api`: REST endpoints for **rules** (GET/POST) and **alarms** (GET, ack/silence/clear).
- `internal/auth`: minimal HS256 JWT validator; roles `viewer|operator|admin`.
- `cmd/jwtgen`: helper to mint demo tokens.

## Endpoints

- `POST /v1/ingest` – ingest telemetry point(s).
- `GET  /v1/rules`  – list rules (viewer+).
- `POST /v1/rules`  – create rule (admin).
- `GET  /v1/alarms` – list alarms (viewer+).
- `POST /v1/alarms/{id}/ack`     – ack (operator+).
- `POST /v1/alarms/{id}/silence` – body: `{"ttl_sec":60}` or `{"until":"2025-01-01T00:00:00Z"}` (operator+).
- `POST /v1/alarms/{id}/clear`   – clear (operator+).

## Auth

- Set `JWT_SECRET` to enable JWT checks. Without it, **dev mode** allows all as admin (warning logged).
- Use `cmd/jwtgen` to create tokens:
  ```bash
  go run ./cmd/jwtgen -sub you@host -role operator -secret "$JWT_SECRET" -exp 1h
  ```
  Then call APIs with `Authorization: Bearer <token>`.

## Quick demo (M3)

1) Start server (migrations auto-run):
   ```bash
   export JWT_SECRET="devsecret"   # enable JWT
   go run ./cmd/jwtgen -sub ops@vast -role admin -secret "$JWT_SECRET" -exp 1h > admin.jwt
   go run ./cmd/jwtgen -sub ops@vast -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
   TRACE_ENABLED=true GEN_ENABLED=true GEN_PATTERN=step GEN_RATE_HZ=10 GEN_SEED=42          go run ./cmd/alarmd
   ```

2) Create a rule (admin):
   ```bash
   curl -s -H "authorization: Bearer $(cat admin.jwt)"          -H 'content-type: application/json'          -d '{"name":"BatteryLow","signal":"Battery.Voltage","type":"threshold","severity":"warn","latch":true,"enabled":true,"config":{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}}'          http://localhost:8080/v1/rules | jq .
   ```

3) Watch alarms appear (engine + manager):
   ```bash
   curl -s -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms | jq .
   ```

4) Ack, silence, clear:
   ```bash
   ALARM_ID=$(curl -s -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms | jq -r '.[0].id')
   curl -s -X POST -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms/$ALARM_ID/ack | jq .
   curl -s -X POST -H "content-type: application/json" -H "authorization: Bearer $(cat operator.jwt)"          -d '{"ttl_sec":120}' http://localhost:8080/v1/alarms/$ALARM_ID/silence | jq .
   curl -s -X POST -H "authorization: Bearer $(cat operator.jwt)" http://localhost:8080/v1/alarms/$ALARM_ID/clear | jq .
   ```

## Notes

- Manager auto-clears **only non-latched** rules when the condition goes false.
- Ack and silence are **idempotent** (no duplicate events on repeated calls).
- Silence stores `silence_until`. Expiry behavior (auto-unsilence) can be added later via a sweeper.
- The API uses stdlib `ServeMux`; path parsing handles `/v1/alarms/{id}/op`.

## Next (Phase 4 preview)
- Actions dispatcher (webhooks with retries & DLQ) and richer alarm queries.

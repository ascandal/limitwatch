# Architecture

**LimitWatch** evaluates incoming telemetry against configured rules in memory, produces evaluation results, and persists alarm state and audit events. The API exposes ingest, rules, and alarm operations.

## High‑Level System Design

```
           +------------------+       +-------------------+
POST /ingest -->  API Server  | ----> |  Ingest Queue     | (bounded chan)
           +---------+--------+       +---------+---------+
                     |                          |
                     v                          v
              +------+-------+            +------+-----+
              | Point Router | --grp-->  | Rule Engine | --results-->
              +------+-------+            +------+-----+
                     |                          |
                     v                          v
                +----+--------------------------+----+
                |          Alarm Manager (FSM)       |
                |  persist alarms & events (DB)      |
                +----+--------------------------+----+
                     |                          |
                     v                          v
           +---------+--+              +--------+----------+
           |  Metrics   |              | Action Dispatcher |
           | /healthz   |              |  (webhooks/DLQ)   |
           +------------+              +-------------------+
```

## Components

- **API & Ingest**: Validates input, enqueues points, exposes REST (rules/alarms), health, and optional pprof.
- **Evaluation Pipeline**: goroutine pool consuming from a bounded channel:
    - **Point Router** groups by signal name/entity.
    - **Rule Engine**: N workers consume a bounded queue and evaluate rules with per‑rule/per‑entity state (hysteresis, windows).
    - **Alarm Manager (FSM)**: Consumes evaluation results; creates/updates alarms and writes audit events.
    - **Storage**: SQLite by default (migrations auto‑run); interfaces allow alternate DBs later.
- **Action Dispatcher**: async worker that reads alarm events, executes webhooks, handles retries/DLQ.
- **Persistence**: DB adapter (SQLite/Postgres) + migrations.
- **Config Loader**: Merges YAML files and DB‑stored rules; supports hot reload.

## Key Data Entities

- **Rule:** id, name, signal selector, type (LO/HI/ROC/DUR), params, hysteresis, deadband, window, duration, latch, enabled, version, created_by.
- **Alarm:** id, rule_id, entity, state, first_seen, last_seen, acked_by, silenced_until.
- **AlarmEvent:** id, alarm_id, type (activated/acked/silenced/cleared), at, actor, metadata (reason, values).
- **ActionAttempt:** id, alarm_event_id, endpoint, payload_hash, idempotency_key, status, attempts, last_error.

**Concurrency model**

- One **ingest queue** (bounded) to decouple HTTP from evaluation.
- **N workers** (configurable) evaluate rules; **per‑signal locks** avoided by using **per‑rule state** stored in a sync.Map keyed by (rule_id, entity).
- The **alarm manager** is invoked inline per evaluation result but writes to DB via a small **batched writer** (to reduce I/O) protected by a chan + single writer goroutine (preserves ordering).
- The **action dispatcher** subscribes to alarm events via a DB notification table/queue.

**State machine (alarm lifecycle)**

```
inactive --(condition true for duration)--> active
active --(ack)--> acked
active/acked --(silence with TTL)--> silenced (auto-expire → prior state)
active/acked --(clear when condition false & unlatch)--> inactive
active/acked --(clear when condition false & latch=false)--> inactive
```

## Data flow

POST /v1/ingest –> [bounded queue] –> Engine (rules/state) –> EvalReport –> Manager (FSM) –> DB
^ trace (optional)

## Operational notes

- **Backpressure**: queue is bounded; `/v1/ingest` returns `503` if full.
- **Determinism**: time semantics use point timestamps; tests cover rule behavior.
- **Rules reload**: engine compiles rules at startup; after creating rules via API, restart to take effect (current limitation).

# Architecture

## Overview

LimitWatch evaluates incoming telemetry against configured rules in memory, produces evaluation results, and persists alarm state and audit events. The API exposes ingest, rules, and alarm operations.


## Components

- **API & Ingest**: Validates requests, enqueues points, exposes REST (rules/alarms), health, and optional pprof.
- **Engine**: N workers consume a bounded queue and evaluate rules with per‑rule/per‑entity state (hysteresis, windows).
- **Alarm Manager (FSM)**: Consumes evaluation results; creates/updates alarms and writes audit events.
- **Storage**: SQLite by default (migrations auto‑run); interfaces allow alternate DBs later.


## Data flow

POST /v1/ingest –> [bounded queue] –> Engine (rules/state) –> EvalReport –> Manager (FSM) –> DB
^ trace (optional)


## Key entities (high-level)

- **Rule**: id, name, signal, type (threshold/duration/roc), params, hysteresis, deadband, duration, latch, enabled
- **Alarm**: id, rule_id, entity, state, first_seen, last_seen, acked_by, silence_until
- **AlarmEvent**: id, alarm_id, type (activated/acked/silenced/cleared), at, actor, payload (metadata)


## Operational notes

- **Backpressure**: queue is bounded; `/v1/ingest` returns `503` if full.
- **Determinism**: time semantics use point timestamps; tests cover rule behavior.
- **Rules reload**: engine compiles rules at startup; after creating rules via API, restart to take effect (current limitation).
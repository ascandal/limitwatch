# Summary

**LimitWatch** is a **Go** service that ingests telemetry, evaluates configurable rules (thresholds, rate‑of‑change, duration, hysteresis, latching), raises alarms, manages their lifecycle (active → acknowledged → silenced/cleared), and triggers runbook actions via reliable webhooks (e.g., Slack/Jira/HTTP). It exposes a secure REST API, Prometheus metrics, and an audit log, and ships as a small container deployable to a laptop, VM, or Kubernetes.

LimitWatch is a compact, production‑style building block for monitoring, alerting, and automated response—ideal for demonstrating professional Go practices while delivering immediate operational value.

It’s scoped:
- is production‑style (secure REST, DB, CI/CD, observability)
- and maps directly to ground operations: **monitor**, **alarm**, and **act**.

## Provides

- **Near‑real‑time detection:** Fast, deterministic evaluation with bounded latency under bursty loads.
- **Operational control:** Acknowledge, silence, and clear alarms with role‑based access and full auditability.
- **Reliable automation:** Idempotent webhooks with retries, backoff, and dead‑letter handling reduce noisy or lost actions.
- **Easy integration & visibility:** Simple HTTP ingest, /metrics for Grafana dashboards, and minimal dependencies (SQLite/Postgres).

## Why it’s important

In mission‑critical environments (e.g., spacecraft ground operations), minutes matter. LimitWatch reduces **time‑to‑detect** and **time‑to‑act**, enforces consistent responses, and provides the traceability operations teams need during reviews and anomaly investigations. It serves as a guardrail for human error and a foundation for higher‑level automation.

## Representative Use Cases

- **Spacecraft ops:**
    - Trip an alarm when **battery voltage** drops below a safe threshold with hysteresis to avoid flap.
    - Detect **thermal runaway** using rate‑of‑change rules; auto‑notify the on‑call channel.
    - Monitor **pressurization** parameters that must remain out‑of‑bounds for N seconds before tripping.

- **Ground systems:**
    - Alert on **downlink gap** duration or **packet loss** spikes; trigger a playbook to switch antennas or increase buffering.
    - Watch **service health** (latency, queue depth) and open a ticket automatically when limits are exceeded.

- **General ops/industrial:**
    - Evaluate sensor streams on test stands or hardware‑in‑the‑loop rigs and execute standard operating procedures on excursions.

---

## Project Goals

**Primary goals**

1. **Detect out‑of‑limits conditions** on incoming telemetry in near‑real time (thresholds, rate‑of‑change, hysteresis, latching).
2. **Manage alarm lifecycle**: raise, acknowledge, silence, clear; preserve a complete audit trail.
3. **Trigger runbook actions** via reliable webhooks (e.g., Slack/Jira/HTTP), with retries and idempotency.
4. **Expose a minimal, secure API** and metrics for operations and dashboards (Prometheus/Grafana).
5. **Be deployable anywhere** (Docker/K8s/single binary) with clear configuration and a deterministic demo.

**Success criteria (demo‑ready)**

- Sustains **≥2k points/sec** on a laptop with **p50 alarm decision latency ≤50 ms** and **no data loss**.
- Prometheus **/metrics** and **Grafana dashboard** show ingest rate, rule hit rate, active alarms, MTTA.
- End‑to‑end runbook webhook fires within **≤500 ms p95** of condition detection (with retries on failure).
- **Audit log** shows who/what/when for rules and alarms; **JWT‑protected** endpoints; **healthz/readyz** OK.

**Non‑goals (for this phase)**

- Full UI (a minimal built‑in HTML status page is optional; Grafana covers visibility).
- Distributed clustering/sharding; multi‑tenant RBAC beyond simple roles.
- Time‑series storage/queries at scale (we evaluate rules in‑memory and persist alarm/audit records only).

---

## Scope & Declarative Requirements

**Ingest**

- **MUST** accept telemetry via POST /v1/ingest (JSON newline or array). Each point: {name, value, ts?}.
- **SHOULD** include a built‑in generator (--gen.rate, --gen.pattern) for deterministic demos.
- **MUST** tolerate burstiness; **MUST NOT** block the caller on rule execution (bounded queue + back‑pressure metric).

**Rule engine**

- **MUST** support rule types:
    - **LO/HI** thresholds with **hysteresis** and **deadband**.
    - **Rate‑of‑change (ROC)** over sliding window.
    - **Duration** (condition must hold for **T** to trip).
    - **Latching** (requires explicit clear) vs **auto‑clear**.
- **MUST** allow rules configured via **YAML or REST**; live reload without restart.
- **MUST** execute deterministically per point using **event time** (ts) with server‑time fallback.

**Alarms**

- **MUST** implement states: inactive → active → acked → (silenced)? → cleared.
- **MUST** record **every transition** with who/why (audit table).
- **MUST** deduplicate identical alarms (same rule, entity) and track **first‑seen** and **last‑seen** timestamps.

**Actions**

- **MUST** support **webhook actions** with:
    - Exponential backoff + jitter.
    - **Idempotency keys** to avoid duplicate effects.
    - Dead‑letter capture after N failures.
- **SHOULD** provide Slack‑compatible payload and generic JSON webhook.

**APIs & security**

- **MUST** provide:
    - GET/POST /v1/rules
    - GET /v1/alarms (filters)
    - POST /v1/alarms/:id/ack, /silence, /clear
    - GET /healthz, /readyz, /metrics, /debug/pprof/
- **MUST** secure with **JWT bearer** (HS256 for demo; pluggable mTLS later).
- **MUST** log structured JSON with correlation IDs.

**Persistence**

- **MUST** persist: rules (versioned), alarms, alarm events, actions, webhook attempts.
- **SHOULD** use **SQLite** by default; support **Postgres** via DSN switch.
- **MUST** run schema migrations automatically on startup.

**Observability**

- **MUST** export Prometheus metrics (ingest rate, queue depth, evaluation latency, action success/failures).
- **SHOULD** emit OpenTelemetry traces for ingest → evaluate → action.

**Operations**

- **MUST** build as a static binary; **MUST** ship Docker image (multi‑stage, non‑root, minimal).
- **SHOULD** include Helm chart & sample Grafana dashboard JSON.

---

## High‑Level System Design

**Component overview**

- **API & Ingest Server** (net/http / chi): validates input, enqueues points, exposes REST, health, metrics, pprof.
- **Evaluation Pipeline**: goroutine pool consuming from a bounded channel:
    - **Point Router** groups by signal name/entity.
    - **Rule Engine** evaluates configured rules per point (uses per‑rule state).
    - **Alarm Manager** applies state machine & dedup; persists transitions.
- **Action Dispatcher**: async worker that reads alarm events, executes webhooks, handles retries/DLQ.
- **Persistence**: DB adapter (SQLite/Postgres) + migrations.
- **Config Loader**: merges YAML files and DB‑stored rules; supports hot reload.
- **Observability**: Prometheus metrics + optional OTel tracing.

**ASCII architecture**

```
           +------------------+       +-------------------+
POST /ingest -->  API Server  | ----> |  Ingest Queue     | (bounded chan)
           +---------+--------+       +---------+---------+
                     |                          |
                     v                          v
              +------+------+            +------+------+
              | Point Router |--grp-->   | Rule Engine | --results-->
              +------+------+            +------+------+
                     |                          |
                     v                          v
                +----+---------------------------+----+
                |          Alarm Manager (FSM)        |
                |  persist alarms & events (DB)       |
                +----+---------------------------+----+
                     |                          |
                     v                          v
           +---------+--+              +--------+----------+
           |  Metrics   |              | Action Dispatcher |
           | /healthz   |              |  (webhooks/DLQ)   |
           +------------+              +-------------------+
```

**Key data entities (high‑level)**

- Rule: id, name, signal selector, type (LO/HI/ROC/DUR), params, hysteresis, deadband, window, duration, latch, enabled, version, created_by.
- Alarm: id, rule_id, entity, state, first_seen, last_seen, acked_by, silenced_until.
- AlarmEvent: id, alarm_id, type (activated/acked/silenced/cleared), at, actor, metadata (reason, values).
- ActionAttempt: id, alarm_event_id, endpoint, payload_hash, idempotency_key, status, attempts, last_error.

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

# Development Roadmap

## Phases & milestones
- **P0 – Scaffold (M0):** CI, basic server (/healthz), Docker/Helm skeleton.
- **P1 – Core & Storage (M1):** Domain (Rule/Alarm/Event), SQLite + migrations, repos, CLI.
- **P2 – Ingest & Engine (M2):** /v1/ingest, bounded queue, threshold/duration/ROC + tests.
- **P3 – Manager & API (M3):** Eval → FSM (active/acked/silenced/cleared), rules/alarms REST, JWT roles.
- **P4 – Actions & Reliability (M4):** Webhooks with retries & DLQ; correlation IDs.
- **P5 – Observability & Ops (M5):** Prometheus metrics, Grafana panels, Docker/Helm polish.
- **P6 – Polish & Docs (M6):** One‑command demo, walkthrough.

## Key risks & mitigations (selected)
- **Burst performance:** bounded queue, worker pool, pre‑compiled rules.
- **Time semantics:** event time with small tolerance; document assumptions.
- **Webhook noise:** latching + debounce; idempotency keys; backoff/DLQ.
- **Concurrency bugs:** `-race` in CI; single‑writer for persistence.

---

## Development Timeline (Phases & Milestones)

> **Assumption:** single engineer, ~7–10 days, 6–8 focused hours/day.

### Phase 0 — Project scaffold
- Repo init, go mod init, CI workflow (fmt, vet, staticcheck, go test -race, build).
- Folder layout (/cmd/alarmd, /internal/..., /deploy/docker, /deploy/helm, /grafana).
- **Milestone M0:** CI green; hello‑world server with /healthz.

### Phase 1 — Core domain & storage
- Define domain types (Rule, Alarm, AlarmEvent), errors, interfaces.
- Implement DB schema + migrations (SQLite default), repository layer.
- **Milestone M1:** Create/read rules and alarms in DB via a temporary CLI.

### Phase 2 — Ingest path & rule engine
- Implement /v1/ingest (validation, enqueue) and bounded queue.
- Implement rule engine (LO/HI, hysteresis/deadband, duration, ROC).
- Per‑rule state w/ window buffers; property tests; deterministic seed.
- **Milestone M2:** Deterministic demo: generator → engine → in‑process traces; unit tests passing.

### Phase 3 — Alarm manager & API
- FSM transitions; idempotent updates; alarm deduping.
- REST endpoints for rules CRUD and alarm listing/ops (ack/silence/clear).
- JWT middleware; roles (viewer/operator/admin).
- **Milestone M3:** End‑to‑end: post rule → ingest points → see alarm → ack/clear via API.

### Phase 4 — Actions & reliability
- Action dispatcher; webhook with retries, idempotency key, DLQ table.
- Structured logs; correlation IDs; error budgets/metrics.
- **Milestone M4:** Slack/webhook fires on activation; retry logic proven with induced 500s.

### Phase 5 — Observability & Ops
- Prometheus metrics and useful labels; optional OpenTelemetry traces.
- Dockerfile (multi‑stage), Helm chart (deployment, svc, config), sample Grafana dashboard JSON.
- **Milestone M5:** docker run demo + Grafana panels show ingest, rule hits, active alarms, MTTA.

### Phase 6 — Polish & docs
- README: architecture, runbook, env vars, curl examples, performance notes.
- 2‑minute screencast script; canned dataset for replay demo.
- **Milestone M6 (DoD):** One‑command demo script: start → load rules → run generator → alarm triggers → webhook → ack in API → metrics visible.

### Stretch (time‑permitting)
- WebSocket /ws live event stream.
- Postgres DSN mode + Helm values.
- Replay mode (evaluate rules on historical file).

---

## Risks & Mitigations

|**Risk**|**Impact**|**Mitigation**|
|---|---|---|
|**Rule engine performance under burst**|Dropped points or rising latency|Bounded queue with back‑pressure metric; configurable worker pool; avoid per‑point DB I/O; pre‑compile rules; reuse buffers; property benchmarks early.|
|**Time semantics (event time vs arrival)**|False positives/negatives|Use point ts as event time; guard out‑of‑order with small tolerance; document assumptions; track clock skew metric.|
|**Webhook flapping/duplicates**|Noisy ops, inconsistent runbooks|Latching + debounce; idempotency keys; exponential backoff + DLQ; suppression windows.|
|**Data races/concurrency bugs**|Unreliable alarms|-race in CI; table‑driven tests; confine mutable rule state; single‑writer pattern for DB changes.|
|**JWT & secrets handling**|Security gaps|Accept configurable HS256 secret via env/secret; minimum token TTL; audit all auth failures; option to disable auth only in dev.|
|**Schema evolution**|Upgrade pain|Versioned migrations; backward‑compatible changes; seed data loader; ephemeral demo DB reset command.|
|**Over‑scoping**|Missed deadline|Fixed feature set; “stretch” items explicitly optional; daily cutline review.|

---

## Technical Choices

- **Language:** Go 1.22+ (modules, context, net/http, time, sync, expvar/pprof).
- **HTTP router:** chi (lightweight) or stdlib mux.
- **Metrics:** prometheus/client_golang.
- **Tracing:** otel (optional).
- **Database:** SQLite (demo) via modernc.org/sqlite or CGO‑free; Postgres via lib/pq (switch by DSN).
- **Migrations:** golang-migrate (CLI or go‑binding).
- **Config:** env + YAML file; viper optional or simple env parser.
- **Logging:** log/slog with JSON handler.
- **Testing:** go test -race, property tests for ROC/hysteresis; integration tests spin DB in‑proc.

---

## Operational Considerations

- **Configuration** (env vars): PORT, DB_DSN, JWT_SECRET, INGEST_QUEUE_SIZE, WORKERS, ACTION_MAX_RETRIES, ACTION_BACKOFF_BASE, GEN_* (demo generator).
- **Deployment**: Static Linux binary; Docker multi‑stage to distroless/non‑root; Helm chart (Deployment, ConfigMap, Service, HPA optional).
- **Security**: Minimal JWT roles: viewer (GET only), operator (ack/silence/clear), admin (rules CRUD). All writes audited.
- **Backup/retention**: Not required for demo; include TTL_DAYS for alarm events to bound DB size.
- **API versioning**: Prefix /v1/…; avoid breaking changes; accept idempotent POST with X‑Idempotency‑Key.

---

## Acceptance Tests (high‑level, not the detailed checklist)

1. **Threshold trip with hysteresis**: rule Battery.V < 23.5 (hyst 0.2) trips on drop to 23.4; does not clear until ≥23.7.
2. **ROC trip**: rule Temp ROC > 5.0°C/10s trips on step change; does not on noise (deadband).
3. **Duration**: rule Pressure > 1.2 for 3s requires sustained condition; brief spikes don’t trip.
4. **Latching**: latched rule remains active until explicit clear, even after value returns in‑bounds.
5. **Acknowledgment**: POST /v1/alarms/:id/ack transitions active → acked; audit entry created.
6. **Silence**: POST …/silence mutes actions for TTL; alarm stays visible; TTL expiry restores behavior.
7. **Webhook reliability**: action endpoint returns 500 for first 2 attempts → dispatcher retries, then succeeds; single external effect (idempotency).
8. **Performance**: with generator at 2k pts/s and 10 rules, p95 evaluation latency ≤100 ms; no 5xx on ingest.
9. **Security**: requests without JWT are 401; viewer cannot POST; admin can CRUD rules.
10. **Observability**: /metrics exposes ingest_points_total, rule_evaluations_seconds, alarm_active_total, action_attempts_total{status=…}; Grafana shows alarms and MTTA.

---

## Definition of Done (DoD)

- All **M0–M6 milestones** complete and demo script runs end‑to‑end.
- README includes architecture diagram (ASCII), run instructions, curl examples, env reference, and performance notes.
- CI green: fmt, vet, staticcheck, go test -race; Docker image builds.
- Sample **Grafana dashboard JSON** committed.
- Minimal **Helm chart** committed and linted.
- Short **walkthrough video script** included.

---

## Assumptions

- Single‑process deployment is sufficient for the demo; horizontal scaling is out of scope.
- Telemetry arrives as scalar numeric values (float64); non‑numeric signals ignored.
- A single JWT secret is acceptable for demo; mTLS and external IdP are future work.

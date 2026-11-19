# LimitWatch - Real-Time Limits, Alarms & Runbook Actioner

**LimitWatch** is a small, secure **Go** service that ingests telemetry, evaluates configurable rules, raises alarms, and manages their lifecycle via a simple HTTP API. It’s designed to run as a compact, production‑style container on a laptop, VM, or Kubernetes.

## Provides

- **Near‑real‑time detection**
  Fast, deterministic evaluation of out‑of‑limits conditions with bounded latency under bursty loads. Supports:
  - Threshold rules (LO/HI) with hysteresis, deadband, and optional duration.
  - Duration rules (condition must hold for N ms).
  - Rate‑of‑change (ROC) rules over sliding windows.

- **Operational control**
  Alarm lifecycle with role‑based access and full auditability:
  - `inactive → active → acked → silenced/cleared`.

- **Easy integration**
  Simple HTTP ingest and JSON APIs, minimal dependencies (SQLite by default), and sensible defaults.

## Representative Use Cases

In mission‑critical environments (e.g., spacecraft ground operations), minutes matter. LimitWatch reduces **time‑to‑detect** and **time‑to‑act**, enforces consistent responses, and provides the traceability operations teams need during reviews and anomaly investigations. It serves as a guardrail for human error and a foundation for higher‑level automation.

- **Spacecraft Operations:**
    - Trip an alarm when **battery voltage** drops below a safe threshold with hysteresis to avoid flap.
    - Detect **thermal runaway** using rate‑of‑change rules; auto‑notify the on‑call channel.
    - Monitor **pressurization** parameters that must remain out‑of‑bounds for N seconds before tripping.

- **Ground Systems:**
    - Alert on **downlink gap** duration or **packet loss** spikes; trigger a playbook to switch antennas or increase buffering.
    - Watch **service health** (latency, queue depth) and open a ticket automatically when limits are exceeded.

- **General Operations:**
    - Evaluate sensor streams on test stands or hardware‑in‑the‑loop rigs and execute standard operating procedures on excursions.

## Getting started

### Prereqs

- Go `1.25.4+` installed.
- Node.js `24.x` and npm to develop or build the optional console. Python 3 is needed for the demo script.

### 1) (Optional) Seed Rules

`cmd/alarmctl` is a **developer/testing** helper. Prefer the REST API for normal operations. If you create rules via the API, restart `alarmd` for them to take effect (engine loads rules at startup).

For a quick local demo, you can seed a rule before starting the server:

```bash
go run ./cmd/alarmctl rule create \
  --name BatteryLow \
  --signal Battery.Voltage \
  --type threshold \
  --severity warn \
  --latch \
  --config-json '{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}'
```

### 2) Start the server

```bash
go run ./cmd/alarmd
```

Check health:

```bash
curl -sf localhost:8080/healthz
curl -sf localhost:8080/readyz
curl -sf localhost:8080/version
```

### 3) Ingest telemetry

Use HTTP to send telemetry manually or from an external service:

```bash
curl -s -X POST localhost:8080/v1/ingest \
  -H 'content-type: application/json' \
  -d '{"name":"Battery.Voltage","value":23.3,"entity":"SC-1"}'
```

- Returns `202 Accepted` and `{"ingested": N}` on success.
- Adds `X-Queue-Len` header with current queue length.
- Returns `503 Service Unavailable` if the bounded queue is full.

### 4) View alarms and act on them

List alarms:

```bash
curl -s http://localhost:8080/v1/alarms | jq .
```

Acknowledge / silence / clear (with auth if enabled, see below):

```bash
ALARM_ID=$(curl -s http://localhost:8080/v1/alarms | jq -r '.[0].id')

curl -s -X POST http://localhost:8080/v1/alarms/$ALARM_ID/ack
curl -s -X POST -H 'content-type: application/json' -d '{"ttl_sec":120}' http://localhost:8080/v1/alarms/$ALARM_ID/silence
curl -s -X POST http://localhost:8080/v1/alarms/$ALARM_ID/clear
```

### 5) Open the operator console

Run Go and Next.js as separate services. From the repository root, start Go:

```bash
WORKERS=1 go run ./cmd/alarmd
```

In another terminal, run the console:

```bash
make web-install
make web-dev
```

Open **http://localhost:3000**. The Telemetry tab shows live values and retained history with time filters and min/max trend charts, refreshing every two seconds. Restart the updated Go service to enable capture of newly ingested samples. History retains up to 100,000 samples across all streams in SQLite.

The Alarms tab lists the latest 100 alarms, filters by state or text, and shows rule conditions and event history. Select an alarm to acknowledge or clear it. Alarm polling runs every five seconds; refresh failures keep the previous snapshot visible and disable actions. Rule definitions are read-only and require a service restart to activate changes.

Next.js connects to Go through `LIMITWATCH_API_URL` (default `http://127.0.0.1:8080`), read at runtime by its server. Go serves only its HTTP API and does not build or serve the frontend. Each service has its own Docker image. See [the web README](web/README.md) for configuration, production startup, and deployment.

For a production console, run `make web-build` and then `make console`. Build and start Go separately. The web build needs no running Go service; the Go build needs no Node.js or web assets.

When JWT authentication is enabled, use **Access token** in the header to enter a token generated below. The console keeps it in memory only; reload clears it. Go enforces permissions and the page displays authentication or permission errors. Never put `JWT_SECRET` into the web application.

History is available to `viewer` and higher roles at:

```text
GET /v1/alarms/{id}/events?after_id=0&limit=50
```

The response contains `events`, `next_after_id`, and `has_more`. Limits must be between 1 and 100. Pass `next_after_id` to read the next page. A full final page can be followed by an empty page. Events are ordered by ID, independently of source clock differences. The console labels engine event times as source time and operator events as action time. Each alarm transition and its event commit in one SQLite transaction, with writer reservation before reads to prevent stale concurrent operator/evaluator updates.

### Repeatable console demo

Start the isolated Go demo API:

```bash
make demo
```

This builds Go, starts one worker with a temporary database, creates the fictional `TestStand.TempC` rule, restarts to load it, and sends `78, 82, 82, 82, 79, 76` at event-time offsets `0, 0.3, 0.6, 0.9, 1.2, 1.5` seconds. The rule trips above 80 for 500 ms and recovers at 77 or lower. Start Next.js separately with `make web-dev` and open **http://localhost:3000** to acknowledge and clear the latched alarm. Stop the demo with Ctrl+C; the temporary data is removed. Use `--port` if 8080 is occupied, and set `LIMITWATCH_API_URL` for Next.js to the printed API origin.

To verify acknowledgment survives a restart and the HTTP history contains `activated`, `acked`, and `cleared`, run:

```bash
python3 scripts/console_demo.py --check --port 8081
```

The replay check needs only Go and Python; it does not build or start Next.js. The demo uses local development auth. It does not test notification delivery, continuous evaluation across restart, or silence expiry.

## Authentication (optional)

LimitWatch supports two auth modes:

- **Dev mode (default)**
  `JWT_SECRET` is **unset** → no tokens required; all callers are treated as admin. Good for local development.

- **Auth mode**
  `JWT_SECRET` is **set** → JWT tokens required; roles enforced:
  - `viewer` — read‑only (GET).
  - `operator` — ack/silence/clear.
  - `admin` — create rules.

To enable auth and mint tokens:

```bash
export JWT_SECRET="devsecret"

go run ./cmd/jwtgen -sub ops@org_name -role admin -secret "$JWT_SECRET" -exp 1h > admin.jwt
go run ./cmd/jwtgen -sub ops@org_name -role operator -secret "$JWT_SECRET" -exp 1h > operator.jwt
```

Use them with:

```bash
-H "authorization: Bearer $(cat operator.jwt)"
```

## Demo Generator (Optional)

For quick demos you can start a generator that sends synthetic signals (e.g., `Battery.Voltage`, `Temp.C`) to limitwatch.

In a new console terminal run:

```bash
go run ./cmd/alarmctl gen \
  -url http://127.0.0.1:8080 \
  -pattern step \
  -rate 10 \
  -entity SC-1 \
  -v
```

If auth is enabled, run with `-token operator.jwt`:

```bash
go run ./cmd/alarmctl gen \
  -url http://192.168.1.60:8080 \
  -pattern step \
  -rate 10 \
  -entity SC-1 \
  -token operator.jwt \
  -v
```

The generator produces simple patterns (`Battery.Voltage`, `Temp.C`) that exercise threshold and ROC rules.

## Known limitations

- **Rules reload:** engine loads rules at startup. Rules created via the REST API take effect after restart (no hot‑reload yet).
- **Ingest auth:** `/v1/ingest` is currently open (no JWT). Consider requiring at least `viewer` or a dedicated producer token in hardened deployments.

## Continue Reading
- [User Guide](./docs/USER-GUIDE.md) - running, config, and API examples.
- [Developer Guide](./docs/DEVELOPER-GUIDE.md) - repo layout, tooling, lint/test/e2e.
- [Architecture](./docs/ARCHITECTURE.md) - how the engine/manager/API fit together.
- [Auth](./docs/AUTHENTICATION.md) - JWT roles and examples.
- [Glossary](./docs/GLOSSARY.md) - key terms.

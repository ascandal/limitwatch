# Run the live LimitWatch demo

Use three terminals in `/Users/s4alien/Coding/limitwatch`. This setup uses a separate demo database, Go on **8081**, and the web app on **3001**. Demo authentication is disabled; no token is needed.

## Prepare before the presentation

Requires Go 1.25.4+, Node.js 24.x, npm, and curl. Build everything ahead of time:

```bash
cd /Users/s4alien/Coding/limitwatch
npm --prefix web ci
npm --prefix web run build
mkdir -p bin
go build -o bin/alarmd ./cmd/alarmd
go build -o bin/alarmctl ./cmd/alarmctl
```

## Terminal 1: start the Go service

```bash
cd /Users/s4alien/Coding/limitwatch
export DB_DSN='file:/tmp/limitwatch-live-demo.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)'

# Create this rule once, before starting Go. Skip creation on later starts.
./bin/alarmctl rule create \
  --name BatteryLowDemo --signal Battery.Voltage \
  --type threshold --severity warn --latch \
  --config-json '{"lo":23.5,"hysteresis":0.2,"duration_ms":500}'

JWT_SECRET= WORKERS=1 PORT=8081 ./bin/alarmd
```

Leave this terminal running. The rule trips below **23.5 V** for at least **500 ms**, recovers at **23.7 V**, and stays latched until cleared. One worker keeps the demo evaluation ordered.

## Terminal 2: start the web app

```bash
cd /Users/s4alien/Coding/limitwatch
LIMITWATCH_API_URL=http://127.0.0.1:8081 \
  HOSTNAME=127.0.0.1 PORT=3001 npm --prefix web start
```

Open **http://127.0.0.1:3001**. Leave this terminal running. Use the prebuilt app for the presentation; it avoids development compilation delays.

## Terminal 3: start fake telemetry

```bash
cd /Users/s4alien/Coding/limitwatch
export DB_DSN='file:/tmp/limitwatch-live-demo.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)'
./bin/alarmctl gen \
  -url http://127.0.0.1:8081 \
  -entity DEMO-1 -pattern step -rate 10 -seed 42 -v
```

This sends `Battery.Voltage` and `Temp.C` ten times per second each. Battery voltage alternates between **24.2 V** and **23.3 V** every **2.5 seconds**. Keep the generator running until the recovery step below.

## Rehearse and warm up

Start all three terminals at least **one minute before presenting** so the chart has history. Confirm:

- `curl -fsS http://127.0.0.1:8081/healthz` returns `ok`.
- **Telemetry** shows both signals for `DEMO-1`. Select `Battery.Voltage` and **Last 5 minutes**.
- **Alarms** shows `BatteryLowDemo` as active, within about ten seconds of starting the generator, including the UI refresh. Telemetry refreshes every two seconds; alarms refresh every five seconds.

## Three-minute walkthrough

1. **Live monitoring:** show the two signals, changing values, and source/reception times. Pause the chart to inspect a spike, then choose **Go live**. Pausing the view does not stop ingestion.
2. **Operator response:** open **Alarms**, select `BatteryLowDemo`, and click **Acknowledge**. Explain that acknowledgment records a response; it does not change the measured voltage.
3. **Recovery:** stop the generator with **Ctrl+C in Terminal 3**, then send a healthy value from that terminal:

```bash
curl -fsS -X POST http://127.0.0.1:8081/v1/ingest \
  -H 'Content-Type: application/json' \
  -d '{"name":"Battery.Voltage","value":24.2,"entity":"DEMO-1"}'
```

4. **Clear and inspect:** wait five seconds. The latched alarm should remain acknowledged. Click **Clear alarm** and show its `activated`, `acked`, and `cleared` history. In **Telemetry**, show the retained chart and expand the time-bucket table.

**Optional persistence check:** with the generator stopped, restart Go in Terminal 1 using the same startup command and database. Keep Next.js running. After reconnecting, show that telemetry history and the cleared alarm remain.

## Quick recovery and shutdown

| Problem | Action |
| --- | --- |
| Port is busy | Stop your previous demo process. If changing ports, update the Go `PORT`, web `LIMITWATCH_API_URL`, generator `-url`, and curl URLs together. If changing the web port, also update its `PORT` and the browser URL. |
| Rule already exists | Skip `rule create` and start Go. Reusing the database preserves the previous demo's alarms and history. |
| No data or connection error | Check Terminal 1 and the generator's `WARN` output. Verify the health URL and that the browser is on port 3001. |
| Alarm reactivates after clearing | Stop the generator and send the healthy value before clearing. New low samples can activate it again. |

Stop the generator first, then the web app and Go with **Ctrl+C** in their terminals. The demo database remains available for another run. For a clean rehearsal, stop Go and the generator, choose a new demo database filename in **both** terminals, and create the rule again before starting Go.

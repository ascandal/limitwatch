# LimitWatch operator console

A Next.js/React application that talks to the Go LimitWatch service through HTTP. It has its own dependencies, build, server, and Docker image. It never opens SQLite or starts Go.

```text
Browser → Next.js :3000 → Go HTTP API :8080 → SQLite
```

The Next.js server forwards the browser's bearer token. Go owns authentication, rule evaluation, alarm state, event history, and telemetry history. The proxy exposes only the reads, acknowledgment, and clearing used by the console. It does not expose telemetry ingestion or rule creation.

## Development

Use Node.js 24.x. From `web/`:

```bash
npm ci
cp .env.example .env.local
npm run dev
```

Open http://localhost:3000. The server reads `LIMITWATCH_API_URL`, defaulting to `http://127.0.0.1:8080`. Set it to another HTTP(S) origin when Go runs elsewhere. It is a server-only value, not a `NEXT_PUBLIC_` variable. The same proxy handles development and production requests; Go needs no browser CORS configuration.

Run Go separately from the repository root:

```bash
WORKERS=1 go run ./cmd/alarmd
```

Alternatively, `make demo` starts an isolated Go API with synthetic data for the console. Use the API URL it prints if you selected a different demo port.

## Telemetry monitoring

The console opens on **Telemetry**. Select a signal/entity to see its latest received value, source timestamp, reception time, and trend. It refreshes every two seconds. Pause the view to inspect it, choose a recent time window, or apply a custom source-time range up to 31 days. The chart shows bucket means and a minimum/maximum band so spikes remain visible. Expand the time-bucket table for exact counts and values. Refresh failures preserve the last successful data and display an error.

Restart the updated Go service once to create the telemetry table, then ingest new samples. Existing alarm records cannot reconstruct telemetry from before this update. Up to 100,000 recent samples are retained across all streams in the existing SQLite database, surviving normal restarts. The signal list shows the 200 most recently received streams. History responses contain at most 300 time buckets over the full selected range.

Monitoring capture is best effort after queue acceptance; storage failures are logged without changing alarm ingestion responses. It is not a durable ingestion queue. The read endpoints require the existing `viewer` role or higher:

```text
GET /v1/telemetry/streams
GET /v1/telemetry/history?name=TestStand.TempC&entity=STAND-1&from=2026-11-19T00:00:00Z&to=2026-11-19T01:00:00Z
```

`from` and `to` are RFC3339 source timestamps. Without them, history defaults to the last hour. Each bucket includes start/end source times, count, minimum, maximum, and mean. The latest received value is separate from the chart: delayed telemetry can arrive now with an older source time.

## Production

The web build succeeds while Go is stopped:

```bash
npm run build
LIMITWATCH_API_URL=http://127.0.0.1:8080 PORT=3000 npm start
```

The build packages the Next.js standalone server and static assets in `.next/standalone/`. Copy that directory to deploy it with Node.js 24.x, then run `node server.js`. Set `LIMITWATCH_API_URL` when starting the server; the same build can target a different Go service without rebuilding. Next.js and Go can be restarted or deployed separately. The console reports a connection error if Go is unavailable.

To build only the web image, use this directory as the build context:

```bash
docker build -t limitwatch-web .
docker run --rm -p 3000:3000 \
  -e LIMITWATCH_API_URL=http://host.docker.internal:8080 \
  limitwatch-web
```

`host.docker.internal` addresses the host on Docker Desktop. On a Docker network, use the Go service's network name instead. The Go image remains under `deploy/docker/Dockerfile` and builds without Node or web assets.

## Checks

```bash
npm run typecheck
npm run lint
npm test
npm run format:check
npm run build
```

`web/go.mod` is only a module boundary that keeps npm dependencies out of root Go package scans. The console contains no Go code and does not invoke the Go toolchain.

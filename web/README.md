# LimitWatch operator console

A Next.js/React application that talks to the Go LimitWatch service through HTTP. It has its own dependencies, build, server, and Docker image. It never opens SQLite or starts Go.

```text
Browser → Next.js :3000 → Go HTTP API :8080 → SQLite
```

The Next.js server forwards the browser's bearer token. Go owns authentication, rule evaluation, alarm state, and event history. The proxy exposes only the reads, acknowledgment, and clearing used by the console. It does not expose telemetry ingestion or rule creation.

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

# LimitWatch — Real-Time Limits, Alarms & Runbook Actioner (Scaffold)

Phase 0 scaffold for the LimitWatch project. This provides a minimal HTTP server with `/healthz`
and the initial CI workflow. Subsequent phases will add the rule engine, alarm FSM, actions,
observability, and deployment artifacts.

## Run Locally

```bash
# Change module path first (optional)
sed -i 's|github.com/ascandal/limitwatch|github.com/ascandal/limitwatch|g' go.mod

# Start the server
go run ./cmd/alarmd
```

### In another terminal
```bash
curl -sf localhost:8080/healthz
```
> Returns: `ok`

```bash
curl -sf localhost:8080/readyz
```
> Returns: `ready`

```bash
curl -sf localhost:8080/version
```
> Returns: `{"service":"alarmd", "version":"dev", "commit":"none", "date":"unknown"}`

> pprof is available at /debug/pprof/


### Build (with version info):

```bash
make build VERSION=0.0.1
./bin/alarmd
```

**Makefile (common tasks):**
- make fmt – format code (writes in place).
- make vet – go vet.
- make staticcheck – installs and runs staticcheck if missing.
- make test – go test -race ./...
- make build – builds bin/alarmd with ldflags (Version, Commit, Date).
- make run – runs the service on port 8080.
- make docker – builds a container image named limitwatch:<VERSION>.

### Docker Image

```bash
make docker VERSION=0.0.1
docker run -p 8080:8080 limitwatch:0.0.1
```

**Container image (deploy/docker/Dockerfile)**
- **Builder:** golang:1.25.4, CGO disabled, trims path; injects version/commit/date via ldflags.
- **Runtime:** distroless/static:nonroot (no shell, minimal attack surface).
- **User:** Runs as nonroot.
- **Port:** Exposes port 8080.

### Helm chart skeleton (deploy/helm)
- Chart.yaml: metadata (v0.1.0 chart, appVersion 0.0.1 placeholder).
- values.yaml:
    - image.repository: ghcr.io/ascandal/limitwatch (change to your registry)
    - image.tag: latest
    - service.port: 80 (front door) → maps to container 8080
    - env.PORT: "8080"
- templates/deployment.yaml:
    - Probes: readiness → /readyz, liveness → /healthz
    - Sets PORT env from values
- templates/service.yaml:
    - ClusterIP on port 80 → targetPort 8080

**Install (run from repo root, once you’ve pushed your image):**
```bash
helm install limitwatch ./deploy/helm \
  --set image.repository=ghcr.io/<you>/limitwatch \
  --set image.tag=0.0.1
```


### CI (Github Actions)

CI workflow for formatting, vetting, static analysis, tests, and build.

Runs on **Push** and **Pull Request**:
1. Formatting check (`gofmt -s -l .`)
    1. Fails if files need formatting
1. Static checks
    1. From the Go toolchain (`go vet ./...`)
    2. broader static analysis (`staticcheck ./...`)
1. Tests (`go test -race ./...`)
1. Build (`go build ./cmd/alarmd`)



## Structure
```text
cmd/alarmd/            # main entrypoint
internal/version/      # build-time version info
deploy/docker/         # Dockerfile
deploy/helm/           # (placeholder) helm chart skeleton
grafana/               # (placeholder) dashboard artifacts
.github/workflows/     # CI pipeline
```

## Notes
- Default port is `8080` (env `PORT` to override).
- `/debug/pprof` endpoints are enabled for local profiling.
- CI enforces `gofmt`, `go vet`, `staticcheck`, `go test -race`, and build.
- This is a minimal starting point intended to be extended in the next phases.

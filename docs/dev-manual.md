# Development Manual

## Phase-0 (Project Scaffold)
- A minimal HTTP service (binary: alarmd) exposing:
    - **GET /healthz** – liveness
    - **GET /readyz** – readiness
    - **GET /version** – build metadata (version/commit/date)
    - **GET /debug/pprof/*** – profiling endpoints
- Graceful shutdown on SIGINT/SIGTERM with a 10‑second timeout.
- JSON logging via Go’s slog.
- CI pipeline (GitHub Actions):
    - format check
    - vet
    - static analysis (staticcheck)
    - go test -race
    - build
- Container image (multi‑stage, distroless, non‑root).
- Helm chart skeleton (Deployment/Service, probes wired up).
- Other
    - MIT license
    - .gitignore
    - Makefile
    - placeholders for Grafana/Helm.


### Directory Structure

```text
LimitWatch/
├─ cmd/alarmd/                 # Service entrypoint (main.go)
├─ internal/version/           # Build-time version variables (ldflags)
├─ deploy/
│  ├─ docker/Dockerfile        # Multi-stage → distroless:nonroot runtime
│  └─ helm/
│     ├─ Chart.yaml            # Chart metadata
│     ├─ values.yaml           # Image/port values
│     └─ templates/
│        ├─ deployment.yaml    # Deployment + probes
│        └─ service.yaml       # ClusterIP Service
├─ grafana/README.md           # Placeholder (dashboards come in Phase 5)
├─ .github/workflows/ci.yml    # CI: fmt, vet, staticcheck, test -race, build
├─ Makefile                    # Common tasks (build/run/docker/etc.)
├─ go.mod                      # Module path
├─ .gitignore                  # Sensible ignores
├─ LICENSE                     # MIT
└─ README.md                   # Quick start
```

- The server (cmd/alarmd/main.go)
- Build‑time versioning (internal/version + ldflags)
- Logging
    - Uses `slog.NewJSONHandler` → clean, structured JSON logs.
    - Startup log includes addr, version, and commit.
    - Error logs include `"err": "<message>"` field.
- CI pipeline (.github/workflows/ci.yml)

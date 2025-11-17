# Developer Guide

## Directory Structure

```text
LimitWatch/
├─ cmd/
│  ├─ alarmd/                  # Service entrypoint (main.go)
│  └─ alarmctl/                # developer/admin CLI for storage ops
├─ deploy/
│  ├─ docker/Dockerfile        # Multi-stage → distroless:nonroot runtime
│  └─ helm/
│     ├─ Chart.yaml            # Chart metadata
│     ├─ values.yaml           # Image/port values
│     └─ templates/
│        ├─ deployment.yaml    # Deployment + probes
│        └─ service.yaml       # ClusterIP Service
├─ docs/
├─ grafana/README.md           # Placeholder (dashboards come in Phase 5)
├─ internal/
│  ├─ api/
│  ├─ auth/
│  ├─ config/
│  ├─ domain/
│  ├─ engine/
│  ├─ generator/
│  ├─ ingest/
│  ├─ manager/
│  ├─ repository/
│  ├─ server/
│  ├─ storage/
│  └─ version/                 # Build-time version variables (ldflags)
├─ tests/
│  └─ e2e/
├─ .github/workflows/ci.yml    # CI: fmt, vet, staticcheck, test -race, build
├─ Makefile                    # Common tasks (build/run/docker/etc.)
├─ go.mod                      # Module path
├─ .gitignore                  # Sensible ignores
├─ LICENSE                     # MIT
└─ README.md                   # Quick start
```

- `internal/engine`: Now emits `EvalReport` on a channel; manager consumes it.
- `internal/manager`: Updates `alarms` and `alarm_events` on trip/clear; provides Ack/Silence/Clear ops.
- `internal/api`: REST endpoints.
- `internal/auth`: Minimal HS256 JWT validator; roles `viewer|operator|admin`.
- `cmd/jwtgen`: helper to mint demo tokens.
- `tools.go` pins CLI tool dependency versions.
    - Blank imports (`_ "module/path"`) to declare dependencies on CLI tools.
    - The tools build tag `//go:build tools tag` prevents the file from getting built into the app. It generates a durable list so `go.mod` can track the tool versions.
- `Taskfile.yml` defines fast, cross‑platform commands to run locally and in CI.


## Tooling
- **Go** 1.22+
- **golangci‑lint** v2.x (config in `.golangci.yml`)
- **gofumpt** + **goimports**
- **go-task**

    **Install the command runner (**Task**):**
    [Github go-task](https://github.com/go-task/task)
    [Taskfile.dev](https://taskfile.dev/)

    ```zsh
    npm install -g @go-task/cli
    ```

    or

    ```zsh
    go install github.com/go-task/task/v3/cmd/task@latest
    ```

### To add the Go bin directory to the shell PATH run:

```zsh
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

### Install the tool binaries at the pinned versions

```zsh
go install golang.org/x/tools/cmd/goimports@$(go list -f '{{.Version}}' -m golang.org/x/tools)
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(go list -f '{{.Version}}' -m github.com/golangci/golangci-lint/v2)
go install gotest.tools/gotestsum@$(go list -f '{{.Version}}' -m gotest.tools/gotestsum)
go install golang.org/x/vuln/cmd/govulncheck@$(go list -f '{{.Version}}' -m golang.org/x/vuln)
```

## Install toolchain and run:

```bash
go mod tidy
golangci-lint run
go test ./...
```

### Run the Dev Tools

Run any of these commands from the root directory.

#### List all tasks:

```zsh
task --list
```

#### Format (canonical style + imports):

```zsh
task fmt
```

- `gofmt -s -w .` rewrites code with Go’s standard formatting and simplifications.
- `goimports -w .` does gofmt and fixes/organizes imports (adds/removes/sorts).

#### Lint (static analysis):

```zsh
task lint
```

```bash
golangci-lint run
```

- Runs golangci-lint (a fast meta‑linter that aggregates many linters).

#### Test (concise, readable output):

```zsh
task test
```

```bash
go test -v ./tests/e2e -run TestE2E_Httptest_M3
# Optional
go test -race ./...
```

- Unit tests: engine rule logic (threshold, duration, ROC).
- E2E (httptest): seeds DB, starts in‑proc server, ingests points, verifies alarm FSM.
- `gotestsum --format=short-verbose -- ./...` runs all package tests and prints a compact per‑test summary.


#### Vulnerability scan (supply‑chain + code paths):

```zsh
task vuln
```

- `govulncheck` consults Go’s vuln DB and reports reachable vulnerabilities, reducing false positives.

#### Run the app:

```zsh
task run
```

- Executes `go run ./cmd/alarmd`.

#### Build a slim binary:

```zsh
task build
```

- `-trimpath` strips local paths from the binary for reproducibility.
- `-ldflags="-s -w"` drops symbol & DWARF tables for smaller size.
- Output goes to bin/app.


## Helper CLI `./cmd/alarmctl` to exercise the storage layer.

- **Threshold** (lo 23.5V, hysteresis 0.2V, latched):
  ```bash
  go run ./cmd/alarmctl rule create \
    --name BatteryLow \
    --signal Battery.Voltage \
    --type threshold \
    --severity warn \
    --latch \
    --config-json '{"lo":23.5,"hysteresis":0.2,"deadband":0.05,"duration_ms":500}'
  ```

- **ROC** (Temp rising faster than 0.15 °C/s over a 1s window):
  ```bash
  go run ./cmd/alarmctl rule create \
    --name TempRiseFast \
    --signal Temp.C \
    --type roc \
    --severity warn \
    --config-json '{"window_ms":1000,"gt":0.15,"abs":true}'
  ```

- **Duration** (Pressure > 1.2 for at least 3s):
  ```bash
  go run ./cmd/alarmctl rule create \
    --name PressureHighSustain \
    --signal Pressure.Bar \
    --type duration \
    --severity crit \
    --config-json '{"op":">","threshold":1.2,"duration_ms":3000,"hysteresis":0.05}'
  ```


## Notes for contributors
- Structured logs via slog (JSON).
- Strict error handling (check/propagate; avoid swallowing).
- Graceful shutdown and HTTP timeouts are in place.

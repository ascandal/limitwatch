# Multi-stage Dockerfile for LimitWatch alarmd
# The exported console needs Node only in this build stage.
FROM node:24-bookworm-slim AS web-builder
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-fund --no-audit
COPY web/ ./
RUN npm run build

# Build stage
FROM golang:1.25.4 AS builder
WORKDIR /src
COPY . .

# Set defaults for build info (can be overridden in CI)
ARG VERSION=dev
ARG COMMIT=none
ARG BUILDDATE=unknown

# CGO_ENABLED=0: forces a fully static binary (no libc), required for “distroless/static” runtime.
# mkdir: ensures /out exists. If it doesn’t, this RUN step can fail with “no such file or directory.”
# -trimpath: strips local filesystem paths from the binary for reproducible builds.
# -ldflags:
#   -s -w: strip symbol/debug tables to reduce size.
#   -X ...Version/Commit/Date=...: sets variables in internal/version to return accurate metadata.
#   -o /out/alarmd: writes the compiled binary to /out/alarmd.
RUN mkdir -p /out && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w \
    -X 'github.com/ascandal/limitwatch/internal/version.Version=${VERSION}' \
    -X 'github.com/ascandal/limitwatch/internal/version.Commit=${COMMIT}' \
    -X 'github.com/ascandal/limitwatch/internal/version.Date=${BUILDDATE}'" \
    -o /out/alarmd ./cmd/alarmd

# Runtime stage (distroless static)
# :nonroot drops root privileges for security.
FROM gcr.io/distroless/static:nonroot
WORKDIR /app

# Copy the Go service and static console; the runtime needs no Node.js.
COPY --from=builder /out/alarmd /app/alarmd
COPY --from=web-builder /web/out /app/web
ENV WEB_DIR=/app/web

# Listens on port 8080 and runs the process as an unprivileged user.
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/alarmd"]

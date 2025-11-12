# Multi-stage Dockerfile for LimitWatch alarmd
# Build stage
FROM golang:1.25.4 AS builder
WORKDIR /src
COPY . .

# Set defaults for build info (can be overridden in CI)
ARG VERSION=dev
ARG COMMIT=none
ARG BUILDDATE=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64         go build -trimpath -ldflags="-s -w -X 'github.com/ascandal/limitwatch/internal/version.Version=${VERSION}' -X 'github.com/ascandal/limitwatch/internal/version.Commit=${COMMIT}' -X 'github.com/ascandal/limitwatch/internal/version.Date=${BUILDDATE}'"         -o /out/alarmd ./cmd/alarmd

# Runtime stage (distroless static)
FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=builder /out/alarmd /app/alarmd
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/alarmd"]

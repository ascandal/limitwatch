    .PHONY: all fmt vet staticcheck test build run docker

    VERSION ?= dev
    COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
    DATE    ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

    all: fmt vet staticcheck test build

    fmt:
		gofmt -s -w .

    vet:
		go vet ./...

    staticcheck:
		@command -v staticcheck >/dev/null 2>&1 || go install honnef.co/go/tools/cmd/staticcheck@latest
		$(shell go env GOPATH)/bin/staticcheck ./...

    test:
		go test -race ./...

    build:
		CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X 'github.com/ascandal/limitwatch/internal/version.Version=$(VERSION)' -X 'github.com/ascandal/limitwatch/internal/version.Commit=$(COMMIT)' -X 'github.com/ascandal/limitwatch/internal/version.Date=$(DATE)'" -o bin/alarmd ./cmd/alarmd

    run:
		PORT=8080 go run ./cmd/alarmd

    docker:
		docker build -t limitwatch:$(VERSION) --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILDDATE=$(DATE) -f deploy/docker/Dockerfile .

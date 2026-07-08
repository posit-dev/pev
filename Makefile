PROJECT := pev
PKG     := github.com/posit-dev/pev
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: help build test lint sec snapshot clean fmt e2e

help:  ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "%-12s %s\n", $$1, $$2}'

build:  ## Build the static pev binary
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(PROJECT) ./

test:  ## Run unit tests with race + shuffle
	go test ./... -race -shuffle=on -coverprofile=cover.out -covermode=atomic

fmt:  ## Run gofmt -w
	gofmt -w .

lint:  ## Run golangci-lint
	golangci-lint run --timeout=5m

sec:  ## Run gosec + govulncheck
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	gosec -severity medium ./...
	govulncheck ./...

snapshot:  ## Build a goreleaser snapshot
	goreleaser release --snapshot --clean

e2e:  ## Run the docker-based e2e matrix (Ubuntu 22/24, Alma 9/10)
	bash test/e2e/run.sh

clean:  ## Remove build artifacts
	rm -f $(PROJECT) cover.out
	rm -rf dist/

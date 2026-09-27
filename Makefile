.PHONY: check fmt vet lint test build
.NOTPARALLEL:

# Build metadata linked into the binary by `make build`. Each is overridable
# (make build VERSION=v1.2.3) so CI and release tooling can supply their own.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT) -X main.buildDate=$(DATE)

# check is the full gate: format, vet, lint, test, in that order, stopping on
# the first failure. CI runs the same four commands.
check: fmt vet lint test

fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test -race ./...

build:
	go build -ldflags "$(LDFLAGS)" -o astimate ./cmd/astimate

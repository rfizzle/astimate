.PHONY: check actionlint fmt vet lint test build
.NOTPARALLEL:

# Build metadata linked into the binary by `make build`. Each is overridable
# (make build VERSION=v1.2.3) so CI and release tooling can supply their own.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT) -X main.buildDate=$(DATE)

# check is the full gate: workflow lint, format, vet, lint, test, in that
# order, stopping on the first failure. CI runs the same five commands.
check: actionlint fmt vet lint test

# actionlint lints .github/workflows when the binary is on PATH and skips
# with a notice otherwise; CI always runs it from a pinned release.
actionlint:
	@if command -v actionlint >/dev/null 2>&1; then actionlint; else echo "actionlint not installed; skipping"; fi

fmt:
	@out="$$(git ls-files -z '*.go' | xargs -0 gofmt -l)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test -race ./...

build:
	go build -ldflags "$(LDFLAGS)" -o astimate ./cmd/astimate

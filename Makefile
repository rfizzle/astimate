.PHONY: check actionlint tidy fmt vet lint test race-soak build test-subset readme-samples readme-check
.NOTPARALLEL:

# Build metadata linked into the binary by `make build`. Each is overridable
# (make build VERSION=v1.2.3) so CI and release tooling can supply their own.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT) -X main.buildDate=$(DATE)

# BUILD_TAGS makes gotreesitter embed only the grammars the TypeScript
# extractor uses instead of all of them. The names are the grammar_subset
# tags in gotreesitter's grammars package; a misspelled one still compiles
# but leaves the grammar out, which test-subset catches. .goreleaser.yaml
# and action/install.sh repeat this list; action/tags_test.go fails if they
# drift from it.
BUILD_TAGS := grammar_subset grammar_subset_typescript grammar_subset_tsx

# check is the full gate: workflow lint, module tidiness, format, vet, lint,
# test, README samples, in that order, stopping on the first failure. CI
# runs the same seven commands.
check: actionlint tidy fmt vet lint test readme-check

# actionlint lints .github/workflows when the binary is on PATH and skips
# with a notice otherwise; CI always runs it from a pinned release.
actionlint:
	@if command -v actionlint >/dev/null 2>&1; then actionlint; else echo "actionlint not installed; skipping"; fi

# tidy fails when go.mod or go.sum differ from what `go mod tidy` would
# write, printing the diff; it never modifies either file.
tidy:
	go mod tidy -diff

fmt:
	@out="$$(git ls-files -z '*.go' | xargs -0 gofmt -l)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

lint:
	golangci-lint run

# COLLECT_PKG's tests load this whole repository through go/packages, whose
# parallel type-checkers hit an upstream go/types data race under -race
# (golang/go#81122; calibration/notes/go-types-race-2026-09-27.md). go/packages
# sizes its worker pool from GOMAXPROCS at init, so that one package runs
# with GOMAXPROCS=1 and everything else keeps full parallelism. CI's go test
# step repeats both commands.
COLLECT_PKG := ./calibration/collect/

test:
	go test -race $$(go list ./... | grep -v /calibration/collect$$)
	GOMAXPROCS=1 go test -race $(COLLECT_PKG)

# race-soak re-verifies the fix after a toolchain or x/tools bump: 50 runs of
# the test that used to flake, under the same setting test uses. Not part of
# check; it takes several minutes.
race-soak:
	GOMAXPROCS=1 go test -race -run TestCollectModule -count=50 -timeout 25m $(COLLECT_PKG)

build:
	go build -tags '$(BUILD_TAGS)' -ldflags "$(LDFLAGS)" -o astimate ./cmd/astimate

# test-subset runs the TypeScript extractor's tests under BUILD_TAGS, so a
# tag name that drifts from gotreesitter's fails here. Not part of check,
# which stays untagged.
test-subset:
	go test -tags '$(BUILD_TAGS)' ./internal/lang/typescript/...

# readme-samples rewrites the sample outputs in README.md from the fixture
# modules; readme-check regenerates them into a temporary copy and fails
# with a diff when README.md has drifted from what the CLI prints.
readme-samples:
	scripts/readme-samples.sh

readme-check:
	scripts/readme-samples.sh --check

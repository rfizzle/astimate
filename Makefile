.PHONY: check fmt vet lint test build
.NOTPARALLEL:

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
	go build -o astimate ./cmd/astimate

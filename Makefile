GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN := bin

.PHONY: all build test race vet fmt fmt-check fuzz install clean dist

all: fmt-check vet test build

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/clip-patch ./cmd/clip-patch
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/cpatch ./cmd/cpatch

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)

fuzz:
	$(GO) test ./internal/parser -run=^$$ -fuzz=FuzzParse -fuzztime=30s

install:
	$(GO) install -ldflags "$(LDFLAGS)" ./cmd/clip-patch ./cmd/cpatch

clean:
	rm -rf $(BIN) dist

dist:
	./scripts/build-release.sh $(VERSION)

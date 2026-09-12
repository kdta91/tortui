SHELL := /bin/sh
.SHELLFLAGS := -eu -c

BINARY := bin/tortui
PKG := ./...
COVERPROFILE := coverage.out
COVER_THRESHOLD := 0

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build run test lint fmt fmt-check check cover clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/tortui

run: build
	./$(BINARY) --config ./dev-config.toml

test:
	go test $(PKG)

lint:
	golangci-lint run

fmt:
	gofumpt -w .
	goimports -w .

fmt-check:
	@test -z "$$(gofumpt -l .)" || (echo "gofumpt: the following files are not formatted:"; gofumpt -l .; exit 1)
	@test -z "$$(goimports -l .)" || (echo "goimports: the following files are not formatted:"; goimports -l .; exit 1)

check: fmt-check lint test
	go vet $(PKG)

cover:
	go test -coverprofile=$(COVERPROFILE) $(PKG)
	go tool cover -func=$(COVERPROFILE)
	@total=$$(go tool cover -func=$(COVERPROFILE) | tail -1 | awk '{print $$3}' | tr -d '%'); \
	echo "total coverage: $$total percent (threshold $(COVER_THRESHOLD) percent)"; \
	awk -v t="$$total" -v thresh="$(COVER_THRESHOLD)" 'BEGIN { if (t+0 < thresh+0) { exit 1 } }' \
		|| (echo "coverage $$total percent is below threshold $(COVER_THRESHOLD) percent"; exit 1)

clean:
	rm -rf bin dist $(COVERPROFILE)

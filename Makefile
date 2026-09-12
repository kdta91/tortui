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

DIST := dist
# T-005: the six tier-1 combinations from AGENT.md §14's support matrix.
BUILD_TARGETS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
SHELLCHECK_SOURCES := $(wildcard scripts/*)

.PHONY: build run test lint fmt fmt-check check cover clean scan hooks build-all

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/tortui

run: build
	./$(BINARY) --config ./dev-config.toml

test:
	go test $(PKG)

lint:
	golangci-lint run
	@command -v shellcheck >/dev/null 2>&1 || { \
		echo "make lint: shellcheck is not installed."; \
		echo "install it (e.g. 'brew install shellcheck', see https://github.com/koalaman/shellcheck#installing) and re-run 'make check'."; \
		exit 1; \
	}
	shellcheck -s sh $(SHELLCHECK_SOURCES)

fmt:
	gofumpt -w .
	goimports -w .

fmt-check:
	@test -z "$$(gofumpt -l .)" || (echo "gofumpt: the following files are not formatted:"; gofumpt -l .; exit 1)
	@test -z "$$(goimports -l .)" || (echo "goimports: the following files are not formatted:"; goimports -l .; exit 1)

check: fmt-check lint test scan
	go vet $(PKG)

scan:
	@command -v gitleaks >/dev/null 2>&1 || { \
		echo "make scan: gitleaks is not installed."; \
		echo "install it (e.g. 'brew install gitleaks', see https://github.com/gitleaks/gitleaks#installing) and re-run 'make check'."; \
		exit 1; \
	}
	gitleaks dir --no-banner --redact -c .gitleaks.toml .

hooks:
	git config core.hooksPath scripts
	@echo "git hooks now run from ./scripts (core.hooksPath) — scripts/pre-commit is active."

cover:
	go test -coverprofile=$(COVERPROFILE) $(PKG)
	go tool cover -func=$(COVERPROFILE)
	@total=$$(go tool cover -func=$(COVERPROFILE) | tail -1 | awk '{print $$3}' | tr -d '%'); \
	echo "total coverage: $$total percent (threshold $(COVER_THRESHOLD) percent)"; \
	awk -v t="$$total" -v thresh="$(COVER_THRESHOLD)" 'BEGIN { if (t+0 < thresh+0) { exit 1 } }' \
		|| (echo "coverage $$total percent is below threshold $(COVER_THRESHOLD) percent"; exit 1)

build-all:
	@rm -rf $(DIST)
	@mkdir -p $(DIST)
	@for target in $(BUILD_TARGETS); do \
		os=$${target%/*}; \
		arch=$${target#*/}; \
		ext=; \
		if [ "$$os" = "windows" ]; then ext=.exe; fi; \
		out=$(DIST)/tortui-$$os-$$arch$$ext; \
		echo "build-all: $$os/$$arch -> $$out"; \
		if ! CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o $$out ./cmd/tortui; then \
			echo "build-all: FAILED for $$os/$$arch"; \
			exit 1; \
		fi; \
	done
	@echo "build-all: all targets built successfully:"
	@ls -1 $(DIST)

clean:
	rm -rf bin dist $(COVERPROFILE)

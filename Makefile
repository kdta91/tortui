SHELL := /bin/sh
# .SHELLFLAGS itself is a GNU Make 3.82+ feature (added the same release that
# added .ONESHELL): on make 3.81 — which is what macOS ships and what
# /usr/bin/make on this development machine actually is — this variable is
# not recognized at all, so it has zero effect: make still hardcodes "-c"
# when invoking $(SHELL), and neither -e nor -u actually apply. Verified
# directly against this machine's make (`make --version` -> GNU Make 3.81):
# a throwaway recipe with `false` as a non-final line kept running, and a
# reference to an unset shell variable did not error, proving both flags
# were silently ignored.
#
# It is kept anyway, so make 3.82+/4.0+ (e.g. the Linux CI runner) gets the
# extra safety net directly from make itself. But since AGENT.md §14 also
# requires targeting make 3.81, real -e/-u semantics can't come from this
# line alone — every recipe below starts its shell invocation with a literal
# `set -eu;`, which is a plain POSIX shell builtin with no dependency on
# which flags make chose to invoke $(SHELL) with. That achieves the same
# fail-fast/no-unset-var behaviour identically on 3.81 and 3.82+, confirmed
# with the same throwaway recipe (`set -eu; false; echo should-not-print`
# stopped before the echo, and `set -eu; echo $${UNSET}` failed on the unset
# reference) under this machine's make 3.81 (T-005 QA remediation, DEC-034
# addendum).
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

# T-006: license enumeration. `go-licenses` inspects the *import* graph, which
# is GOOS-gated in this repo (internal/platform is split per-OS, and only
# paths_linux.go imports adrg/xdg) — a single-GOOS run under-reports. LICENSE_OSES
# lists the three tier-1 operating systems from AGENT.md §14 (arch does not
# affect Go's import graph, so GOARCH is irrelevant here, unlike BUILD_TARGETS
# above); `licenses` runs go-licenses once per OS and merges the results, the
# same "analyze every platform from one host" idea build-all already uses for
# cross-compilation.
#
# QA remediation (T-006): the merge step below pipes the three per-GOOS reports
# through `sort -u` to dedup and order them before they're written into NOTICE,
# which CI then diffs against the committed file to detect staleness. Plain
# `sort -u` collates by the shell's ambient locale, and `github.com/BurntSushi/toml`
# vs `github.com/adrg/xdg` collate in different relative order under
# `en_US.UTF-8` (macOS default, where the committed NOTICE was first generated)
# than under the `C`/POSIX byte-order collation GitHub's ubuntu runner uses --
# so the exact same dependency set produced a different byte order on CI than
# on the developer's machine, and the staleness gate (`git diff --exit-code --
# NOTICE`) failed on every CI run regardless of whether dependencies had
# actually changed. Confirmed directly: regenerating under `LC_ALL=C` and under
# `LC_ALL=en_US.UTF-8` produced byte-different NOTICE files before this fix,
# and byte-identical ones after. `LC_ALL=C` is pinned on both the `sort -u` and
# the final `awk` reformat below so the output is deterministic on any machine,
# in any locale, matching or not.
MODULE := github.com/kdta91/tortui
LICENSE_OSES := darwin linux windows
ALLOWED_LICENSES := MIT,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC
NOTICE_TMP := .notice.tmp

.PHONY: build run test lint fmt fmt-check check cover clean scan hooks build-all licenses

build:
	set -eu; go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/tortui

run: build
	set -eu; ./$(BINARY) --config ./dev-config.toml

test:
	set -eu; go test $(PKG)

lint:
	set -eu; golangci-lint run
	@set -eu; command -v shellcheck >/dev/null 2>&1 || { \
		echo "make lint: shellcheck is not installed."; \
		echo "install it (e.g. 'brew install shellcheck', see https://github.com/koalaman/shellcheck#installing) and re-run 'make check'."; \
		exit 1; \
	}
	set -eu; shellcheck -s sh $(SHELLCHECK_SOURCES)

fmt:
	set -eu; gofumpt -w .
	set -eu; goimports -w .

fmt-check:
	@set -eu; test -z "$$(gofumpt -l .)" || (echo "gofumpt: the following files are not formatted:"; gofumpt -l .; exit 1)
	@set -eu; test -z "$$(goimports -l .)" || (echo "goimports: the following files are not formatted:"; goimports -l .; exit 1)

check: fmt-check lint test scan
	set -eu; go vet $(PKG)

scan:
	@set -eu; command -v gitleaks >/dev/null 2>&1 || { \
		echo "make scan: gitleaks is not installed."; \
		echo "install it (e.g. 'brew install gitleaks', see https://github.com/gitleaks/gitleaks#installing) and re-run 'make check'."; \
		exit 1; \
	}
	set -eu; gitleaks dir --no-banner --redact -c .gitleaks.toml .

licenses:
	@set -eu; command -v go-licenses >/dev/null 2>&1 || { \
		echo "make licenses: go-licenses is not installed."; \
		echo "install it (e.g. 'go install github.com/google/go-licenses/v2@v2.0.1') and re-run 'make licenses'."; \
		exit 1; \
	}
	@set -eu; for goos in $(LICENSE_OSES); do \
		echo "make licenses: checking allowed licenses for GOOS=$$goos"; \
		GOOS=$$goos go-licenses check ./... --ignore $(MODULE) --allowed_licenses=$(ALLOWED_LICENSES); \
	done
	@set -eu; rm -f $(NOTICE_TMP); \
	for goos in $(LICENSE_OSES); do \
		GOOS=$$goos go-licenses report ./... --ignore $(MODULE) 2>/dev/null >> $(NOTICE_TMP); \
	done; \
	LC_ALL=C sort -u $(NOTICE_TMP) -o $(NOTICE_TMP)
	@set -eu; { \
		echo "NOTICE"; \
		echo "#"; \
		echo "# Generated by 'make licenses' (github.com/google/go-licenses/v2). Do not edit by"; \
		echo "# hand -- regenerate with 'make licenses' after any dependency change; CI fails if"; \
		echo "# this file is stale or lists a disallowed license (AGENT.md section 3, section 16)."; \
		echo "#"; \
		echo "# Lists every direct and transitive Go module dependency that is actually compiled"; \
		echo "# into a tortui binary on at least one tier-1 target platform (darwin, linux,"; \
		echo "# windows -- AGENT.md section 14): 'go-licenses report ./...' run once per GOOS,"; \
		echo "# merged and deduplicated. Modules that appear in go.sum only as test-only"; \
		echo "# dependencies of a dependency's own test suite -- currently"; \
		echo "# github.com/stretchr/testify, github.com/davecgh/go-spew,"; \
		echo "# github.com/pmezard/go-difflib and gopkg.in/yaml.v3, all pulled in transitively by"; \
		echo "# github.com/adrg/xdg's own tests -- are never part of a compiled tortui binary on"; \
		echo "# any platform and are intentionally not listed here."; \
		echo "#"; \
		echo "module,license,license_url"; \
		LC_ALL=C awk -F, '{ print $$1 "," $$3 "," $$2 }' $(NOTICE_TMP); \
	} > NOTICE
	@set -eu; rm -f $(NOTICE_TMP)
	@set -eu; echo "make licenses: NOTICE regenerated."

hooks:
	set -eu; git config core.hooksPath scripts
	@set -eu; echo "git hooks now run from ./scripts (core.hooksPath) — scripts/pre-commit is active."

cover:
	set -eu; go test -coverprofile=$(COVERPROFILE) $(PKG)
	set -eu; go tool cover -func=$(COVERPROFILE)
	@set -eu; total=$$(go tool cover -func=$(COVERPROFILE) | tail -1 | awk '{print $$3}' | tr -d '%'); \
	echo "total coverage: $$total percent (threshold $(COVER_THRESHOLD) percent)"; \
	awk -v t="$$total" -v thresh="$(COVER_THRESHOLD)" 'BEGIN { if (t+0 < thresh+0) { exit 1 } }' \
		|| (echo "coverage $$total percent is below threshold $(COVER_THRESHOLD) percent"; exit 1)

build-all:
	@set -eu; rm -rf $(DIST)
	@set -eu; mkdir -p $(DIST)
	@set -eu; for target in $(BUILD_TARGETS); do \
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
	@set -eu; echo "build-all: all targets built successfully:"
	@set -eu; ls -1 $(DIST)

clean:
	set -eu; rm -rf bin dist $(COVERPROFILE)

.PHONY: all build dev web build-web test dist publish clean help

VERSION ?= $(shell cat VERSION 2>/dev/null | tr -d ' \r\n')
# Support 'make publish 0.2.0' where target argument is passed as next goal
VERSION_ARG := $(filter-out publish,$(MAKECMDGOALS))
ifeq ($(strip $(VERSION_ARG)),)
  TARGET_VERSION := $(VERSION)
else
  TARGET_VERSION := $(strip $(VERSION_ARG))
  # Treat extra goal as no-op so make doesn't attempt to build it
  %:
	@:
endif

# Clean leading 'v' from version string if present
CLEAN_VERSION := $(patsubst v%,%,$(TARGET_VERSION))

LDFLAGS := -s -w

all: build

help:
	@echo "punk0 make targets:"
	@echo "  make build             - build punk0 binary for current platform"
	@echo "  make dev               - build ./punk0 as a dev build: ./punk0 <dir> then runs beside the installed punk0"
	@echo "  make web               - bundle web assets (JS/CSS/themes)"
	@echo "  make test              - run go test suite"
	@echo "  make test-short        - run fast unit tests (short mode)"
	@echo "  make coverage          - run tests and output statement coverage per package"
	@echo "  make verify            - run unified test and micro-benchmark verification (fast mode)"
	@echo "  make verify-all        - run full test, benchmark and coverage verification suite"
	@echo "  make check             - bundle web assets and run fast test suite"
	@echo "  make bench             - run root Go micro-benchmarks"
	@echo "  make benchmark         - run real repository macroscopic benchmarks"
	@echo "  make dist              - compile cross-platform binaries into dist/"
	@echo "  make publish <version> - bump VERSION, commit, tag, and build dist binaries"
	@echo "  make clean             - remove build artifacts"

web:
	@echo "Bundling web assets..."
	@bun ./scripts/build-web.js

build-web: web

build: web
	@echo "Building punk0 for local system..."
	go build -trimpath -ldflags="$(LDFLAGS)" -o punk0 .
	@echo "Built ./punk0 ($$(du -h punk0 | cut -f1))"

# A dev build keeps settings, projects, chats and worktrees in .punk0-dev/ here
# and listens on 7800, so ./punk0 runs beside the installed punk0 (dev.go).
# `make build` makes a normal binary again.
dev: web
	@echo "Building punk0 (dev) for local system..."
	go build -trimpath -ldflags="$(LDFLAGS) -X main.devBuild=1" -o punk0 .
	@echo "Built ./punk0 (dev): run ./punk0 [dir]; it keeps everything in .punk0-dev/"

dist:
	./build.sh

test: web
	go test -v ./...

web-check:
	@bun ./scripts/check-web.js

test-short:
	go test -short ./...

coverage: web
	./scripts/test-bench.sh --test

verify: web
	./scripts/test-bench.sh --fast

verify-all: web
	./scripts/test-bench.sh

check: web-check web test-short

bench:
	go test -bench=. -benchmem -run=^$$ .

benchmark: build
	./benchmark.sh

publish:
	@if [ -z "$(CLEAN_VERSION)" ]; then \
		echo "Error: Version cannot be empty. Usage: make publish <version> (e.g. make publish 0.2.0)"; \
		exit 1; \
	fi
	@echo "==> Preparing release v$(CLEAN_VERSION) (previous: $$(cat VERSION))"
	@echo "$(CLEAN_VERSION)" > VERSION
	@echo "==> Bundling web assets and building dist binaries..."
	@./build.sh
	@echo "==> Updating git repository..."
	@git add VERSION
	@git commit -m "Release v$(CLEAN_VERSION)" || true
	@git tag -fa "v$(CLEAN_VERSION)" -m "Release v$(CLEAN_VERSION)"
	@echo ""
	@echo "✓ Successfully prepared release v$(CLEAN_VERSION) and updated dist/!"
	@echo "Tag v$(CLEAN_VERSION) has been created."
	@echo ""
	@echo "To publish to GitHub and trigger release workflow, run:"
	@echo "  git push origin main --tags"

clean:
	rm -f punk0 web/app.js web/workbench.js
	touch web/app.js web/workbench.js
	rm -rf dist/

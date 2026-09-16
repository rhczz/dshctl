BINARY  := dshctl
PREFIX  ?= $(HOME)/.local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

PKG     := github.com/rhczz/dshctl/internal/version
LDFLAGS := -s -w \
	-X $(PKG).Version=$(VERSION) \
	-X $(PKG).Commit=$(COMMIT) \
	-X $(PKG).BuildDate=$(DATE)

# Platforms the tool is expected to work on. Windows needs no extra tooling:
# the port and process probes use the Windows API directly.
# TEST_TIMEOUT bounds one test binary. A regression that makes a wait loop spin
# (rather than fail) would otherwise sit until Go's ten-minute default, which is
# the difference between a red build in a minute and a red build in ten.
TEST_TIMEOUT ?= 600s

PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build vet test test-race hermetic coverage workflow-check conventions fmt fmt-check check ci cross mutation install uninstall clean help

## build: compile the binary into bin/dshctl
build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/dshctl

## vet: run the Go vet suite for the host platform
vet:
	go vet ./...

## test: run unit tests
test:
	go test -timeout $(TEST_TIMEOUT) ./...

## test-race: run unit tests under the race detector
test-race:
	go test -race -timeout $(TEST_TIMEOUT) ./...

## hermetic: prove the tests create no state outside their temp directories
hermetic:
	@./scripts/hermetic-check.sh

## coverage: the hermetic run plus the coverage gate, as the pipeline runs them
coverage:
	@./scripts/hermetic-check.sh -coverprofile=/tmp/dshctl-coverage.out
	@python3 scripts/check-coverage.py /tmp/dshctl-coverage.out \
		--require internal/nodejs=100 \
		--report internal/config \
		--report internal/service

## workflow-check: validate the CI workflow's shape
workflow-check:
	@python3 scripts/check-workflow.py

## conventions: check the writing, structure, and dependency conventions
conventions:
	@python3 scripts/check-conventions.py

## fmt: format every source file
fmt:
	gofmt -s -w .

## fmt-check: fail when a file needs formatting
fmt-check:
	@unformatted=$$(gofmt -s -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "these files need gofmt -s -w:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

## cross: compile every supported platform into dist/
cross:
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		goos=$${platform%/*}; goarch=$${platform#*/}; \
		suffix=""; [ "$$goos" = "windows" ] && suffix=".exe"; \
		out="dist/$(BINARY)_$${goos}_$${goarch}$$suffix"; \
		echo "building $$out"; \
		GOOS=$$goos GOARCH=$$goarch go build -ldflags "$(LDFLAGS)" -o "$$out" ./cmd/dshctl || exit 1; \
	done

## check: verify formatting, conventions, vet, and tests without rewriting anything
check: fmt-check conventions vet test

## ci: what the pipeline runs on every commit
ci: workflow-check fmt-check conventions vet coverage
	go test -race -count=1 -timeout $(TEST_TIMEOUT) ./...

## mutation: break each Node decision and require the suite to notice
mutation:
	@python3 scripts/mutation-check.py

## install: copy the built binary onto PATH
install: build
	install -d $(PREFIX)
	install -m 0755 bin/$(BINARY) $(PREFIX)/$(BINARY)
	@echo "installed $(PREFIX)/$(BINARY)"

## uninstall: remove the installed binary
uninstall:
	rm -f $(PREFIX)/$(BINARY)

## clean: remove build output
clean:
	rm -rf bin dist

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

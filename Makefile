# ═══════════════════════════════════════════════════════════════
# Kugelblitz — Go Agent Framework
# ═══════════════════════════════════════════════════════════════
#
# A Go agent framework library.  Default target (`make`) runs all tests.
# Binaries under cmd/ are separate modules — they are not part of the
# library and are not compiled unless explicitly requested.
#
# Using the library (external projects):
#   go get github.com/B777B2056-2/kugelblitz
#   import "github.com/B777B2056-2/kugelblitz/runtime"
#   import "github.com/B777B2056-2/kugelblitz/config"
#
# Building executables (local development / release):
#   make build                            # all cmd binaries
#   make build build-cmds=kugelblitz-ui   # specific binary only
#
# ═══════════════════════════════════════════════════════════════

# ── Variables ──

MODULE   := github.com/B777B2056-2/kugelblitz
BIN_DIR  := bin
GO       := go
GOFLAGS  :=
LDFLAGS  := -s -w

# Binaries under cmd/ (exclude cmd/common — it's a shared library).
CMD_DIRS  := $(filter-out cmd/common/main.go,$(wildcard cmd/*/main.go))
CMD_NAMES := $(patsubst cmd/%/main.go,%,$(CMD_DIRS))

# Filter which binaries to build:  make build build-cmds=kugelblitz-ui
build-cmds := $(CMD_NAMES)

PREFIX    ?= $(shell $(GO) env GOPATH)
COVER_OUT := coverage.out

# ── Default: run tests ──

.DEFAULT_GOAL := test

# ═══════════════════════════════════════════
# Library targets
# ═══════════════════════════════════════════

# test — run all tests (framework + cmd modules).
.PHONY: test
test:
	$(GO) test $(GOFLAGS) ./... -count=1
	cd cmd/common && $(GO) test $(GOFLAGS) ./... -count=1
	cd cmd/kugelblitz-ui && $(GO) test $(GOFLAGS) ./... -count=1
	cd cmd/acp_server && $(GO) test $(GOFLAGS) ./... -count=1

# test-race — run all tests with the race detector.
.PHONY: test-race
test-race:
	$(GO) test $(GOFLAGS) -race ./... -count=1
	cd cmd/common && $(GO) test $(GOFLAGS) -race ./... -count=1
	cd cmd/kugelblitz-ui && $(GO) test $(GOFLAGS) -race ./... -count=1
	cd cmd/acp_server && $(GO) test $(GOFLAGS) -race ./... -count=1

# test-cover — run all tests (framework only) with coverage.
.PHONY: test-cover
test-cover:
	$(GO) test $(GOFLAGS) -coverprofile=$(COVER_OUT) -covermode=atomic ./...
	$(GO) tool cover -func=$(COVER_OUT)
	@echo ""
	@echo "HTML report:  make cover-html"

.PHONY: cover-html
cover-html: test-cover
	$(GO) tool cover -html=$(COVER_OUT)

# test-short — skip long-running tests (fast CI gate).
.PHONY: test-short
test-short:
	$(GO) test $(GOFLAGS) -short ./... -count=1
	cd cmd/common && $(GO) test $(GOFLAGS) -short ./... -count=1
	cd cmd/kugelblitz-ui && $(GO) test $(GOFLAGS) -short ./... -count=1
	cd cmd/acp_server && $(GO) test $(GOFLAGS) -short ./... -count=1

# lint — go vet + golangci-lint when available.
GOLANGCI_LINT := $(shell command -v golangci-lint 2>/dev/null)
.PHONY: lint
lint:
ifdef GOLANGCI_LINT
	$(GOLANGCI_LINT) run ./...
	cd cmd/common && $(GOLANGCI_LINT) run ./...
	cd cmd/kugelblitz-ui && $(GOLANGCI_LINT) run ./...
	cd cmd/acp_server && $(GOLANGCI_LINT) run ./...
else
	$(GO) vet ./...
	cd cmd/common && $(GO) vet ./...
	cd cmd/kugelblitz-ui && $(GO) vet ./...
	cd cmd/acp_server && $(GO) vet ./...
	@echo "Tip: install golangci-lint → https://golangci-lint.run"
endif

# fmt — format all Go sources.
.PHONY: fmt
fmt:
	$(GO) fmt ./...
	cd cmd/common && $(GO) fmt ./...
	cd cmd/kugelblitz-ui && $(GO) fmt ./...
	cd cmd/acp_server && $(GO) fmt ./...

# fmt-check — fail if any file is not gofmt'd (CI gate).
.PHONY: fmt-check
fmt-check:
	@test -z "$$($(GO) fmt ./...)" || (echo "Unformatted files found — run make fmt" && exit 1)
	@cd cmd/common && test -z "$$($(GO) fmt ./...)" || (echo "Unformatted files found in cmd/common — run make fmt" && exit 1)
	@cd cmd/kugelblitz-ui && test -z "$$($(GO) fmt ./...)" || (echo "Unformatted files found in cmd/kugelblitz-ui — run make fmt" && exit 1)
	@cd cmd/acp_server && test -z "$$($(GO) fmt ./...)" || (echo "Unformatted files found in cmd/acp_server — run make fmt" && exit 1)

# tidy — sync go.mod / go.sum for all modules.
.PHONY: tidy
tidy:
	$(GO) mod tidy
	cd cmd/common && $(GO) mod tidy
	cd cmd/kugelblitz-ui && $(GO) mod tidy
	cd cmd/acp_server && $(GO) mod tidy

# verify — check go.sum integrity for all modules (CI gate).
.PHONY: verify
verify:
	$(GO) mod verify
	cd cmd/common && $(GO) mod verify
	cd cmd/kugelblitz-ui && $(GO) mod verify
	cd cmd/acp_server && $(GO) mod verify

# vet — alias for go vet.
.PHONY: vet
vet:
	$(GO) vet ./...

# check — fast pre-merge gate: format + vet + short tests.
.PHONY: check
check: fmt-check vet test-short
	@echo "check passed"

# ci — full CI pipeline: format + lint + race + coverage.
.PHONY: ci
ci: fmt-check lint test-race test-cover verify
	@echo "ci passed"

# ═══════════════════════════════════════════
# Executable binaries (optional)
# ═══════════════════════════════════════════

# build — compile selected cmd programs → bin/.
#   make build                                # all discovered binaries
#   make build build-cmds=kugelblitz-ui       # single binary
.PHONY: build
build: $(addprefix $(BIN_DIR)/,$(build-cmds))

$(BIN_DIR)/%: cmd/%/main.go
	@mkdir -p $(BIN_DIR)
	cd cmd/$* && $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ../../$@ .
	@echo "built $(BIN_DIR)/$*"

# install — copy built binaries to $PREFIX/bin (default $GOPATH/bin).
#   make install
#   make install build-cmds=kugelblitz-ui PREFIX=/usr/local
.PHONY: install
install: build
	@for name in $(build-cmds); do \
		cp $(BIN_DIR)/$$name $(PREFIX)/bin/$$name && \
		echo "installed $(PREFIX)/bin/$$name"; \
	done

# ── Cleanup ──

.PHONY: clean
clean:
	rm -rf $(BIN_DIR)
	rm -f $(COVER_OUT)

# ═══════════════════════════════════════════
# Help
# ═══════════════════════════════════════════

.PHONY: help
help:
	@echo ""
	@echo "  Kugelblitz — Go Agent Framework"
	@echo "  ════════════════════════════════"
	@echo ""
	@echo "  Import as library:"
	@echo "    go get $(MODULE)"
	@echo "    import \"$(MODULE)/runtime\""
	@echo "    import \"$(MODULE)/config\""
	@echo ""
	@echo "  Library targets (default: make = make test):"
	@echo "    make test          run all tests"
	@echo "    make test-race     run tests with race detector"
	@echo "    make test-cover    run tests with coverage"
	@echo "    make lint          go vet + golangci-lint"
	@echo "    make fmt           gofmt all sources"
	@echo "    make tidy          sync all go.mod"
	@echo "    make check         fast gate (fmt + vet + short tests)"
	@echo "    make ci            full CI (fmt + lint + race + cover)"
	@echo ""
	@echo "  Executable binaries (optional):"
	@echo "    make build                              compile all cmd/* → bin/"
	@echo "    make build build-cmds=kugelblitz-ui     compile single binary"
	@echo "    make install                            install to \$$GOPATH/bin"
	@echo "    make install build-cmds=... PREFIX=/usr/local"
	@echo ""
	@echo "  Available binaries: $(CMD_NAMES)"
	@echo ""

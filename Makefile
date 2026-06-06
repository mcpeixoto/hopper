# Hopper — build, test, and release helpers.

# Version is derived from git tags (e.g. v0.1.0), falling back to "dev".
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Optional: embed the release signing public key so binaries verify update signatures.
HOPPER_SIGNING_PUBKEY ?=
LDFLAGS := -s -w \
	-X github.com/mcpeixoto/hopper/internal/version.Version=$(VERSION) \
	-X github.com/mcpeixoto/hopper/internal/updater.SigningPublicKey=$(HOPPER_SIGNING_PUBKEY)

GO       ?= go
BIN      := bin
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: all build server agent cli test vet fmt fmt-check lint tidy clean run-server run-agent dist

all: build

## build: compile hopperd, hopper-agent and the hopper CLI for the host platform
build: server agent cli

server:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/hopperd ./cmd/hopperd

agent:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/hopper-agent ./cmd/hopper-agent

cli:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN)/hopper ./cmd/hopper

## test: run the full test suite
test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

## fmt-check: fail if any file is not gofmt-clean (used in CI)
fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

tidy:
	$(GO) mod tidy

## run-server: run the control plane locally
run-server:
	$(GO) run -ldflags "$(LDFLAGS)" ./cmd/hopperd

## run-agent: run a worker agent locally
run-agent:
	$(GO) run -ldflags "$(LDFLAGS)" ./cmd/hopper-agent

## dist: cross-compile release binaries + checksums into dist/
dist:
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o dist/hopperd_$${os}_$${arch} ./cmd/hopperd; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o dist/hopper-agent_$${os}_$${arch} ./cmd/hopper-agent; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o dist/hopper_$${os}_$${arch} ./cmd/hopper; \
	done
	@cd dist && sha256sum * > checksums.txt && echo "wrote dist/checksums.txt"

clean:
	rm -rf $(BIN) dist

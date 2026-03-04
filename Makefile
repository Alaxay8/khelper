GO ?= go
BINARY ?= khelper
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X 'github.com/alexey/khelper/cmd.Version=$(VERSION)' -X 'github.com/alexey/khelper/cmd.Commit=$(COMMIT)' -X 'github.com/alexey/khelper/cmd.BuildDate=$(DATE)'
GOFILES := $(shell find . -type f -name '*.go' -not -path './vendor/*')

.PHONY: build test lint release install

build:
	@mkdir -p bin
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

test:
	$(GO) test ./...

lint:
	@test -z "$$(gofmt -l $(GOFILES))" || (echo "gofmt check failed"; gofmt -l $(GOFILES); exit 1)
	$(GO) vet ./...

release:
	@mkdir -p dist
	@for goos in linux darwin; do \
		for goarch in amd64 arm64; do \
			out="dist/$(BINARY)_$${goos}_$${goarch}"; \
			echo "Building $$out"; \
			GOOS=$${goos} GOARCH=$${goarch} CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $$out .; \
		done; \
	done

install:
	./scripts/install.sh --mode auto

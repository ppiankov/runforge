BINARY    := runforge
MODULE    := github.com/ppiankov/runforge
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VERSION_NUM := $(VERSION:v%=%)
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS   := -X $(MODULE)/internal/cli.Version=$(VERSION_NUM) -X $(MODULE)/internal/cli.Commit=$(COMMIT)

.PHONY: build test lint clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/runforge

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

clean:
	rm -r bin/ 2>/dev/null || true

.DEFAULT_GOAL := build

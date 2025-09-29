.PHONY: build lint test

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

GOBIN ?= $(shell go env GOBIN)
GOPATH_BIN := $(shell go env GOPATH)/bin
GOLANGCI_LINT := $(if $(GOBIN),$(GOBIN)/golangci-lint,$(GOPATH_BIN)/golangci-lint)

build:
	mkdir -p bin
	go build $(LDFLAGS) -o bin/daemon ./cmd/daemon

lint:
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	"$(GOLANGCI_LINT)" run ./...

test:
	go test ./... -v -race

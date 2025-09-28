.PHONY: build lint test

build:
	mkdir -p bin
	go build -o bin/daemon ./cmd/daemon

lint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GOPATH)/bin/golangci-lint run ./...

test:
	go test ./... -v -race

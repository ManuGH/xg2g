.PHONY: lint test

lint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GOPATH)/bin/golangci-lint run ./...

test:
	go test ./... -v -race
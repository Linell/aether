.PHONY: build test lint fmt

build:
	go build -o bin/aether ./cmd/aether

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -l -w .

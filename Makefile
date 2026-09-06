.PHONY: build test lint fmt up

build:
	go build -o bin/aether ./cmd/aether

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -l -w .

up: build
	scripts/up.sh

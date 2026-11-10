.PHONY: build test fmt

build:
	go build -o bin/clusterpilot ./cmd/clusterpilot

test:
	go test ./...

fmt:
	gofmt -w .

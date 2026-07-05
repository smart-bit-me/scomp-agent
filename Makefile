BINARY  := scomp
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: build build-all run test test-race test-cover lint clean

build:
	go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/$(BINARY) ./cmd/scomp

build-all:
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64  go build -trimpath -ldflags="-s -w" -o bin/$(BINARY)-linux-amd64   ./cmd/scomp
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64  go build -trimpath -ldflags="-s -w" -o bin/$(BINARY)-linux-arm64   ./cmd/scomp
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64  go build -trimpath -ldflags="-s -w" -o bin/$(BINARY)-darwin-amd64  ./cmd/scomp
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64  go build -trimpath -ldflags="-s -w" -o bin/$(BINARY)-darwin-arm64  ./cmd/scomp

run:
	go run ./cmd/scomp --server wss://link.scomp.me/agent

test:
	go test ./...

test-race:
	go test -race ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ coverage.out coverage.html

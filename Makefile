.PHONY: build test lint dev ui-build release clean generate

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build: ui-build
	go build $(LDFLAGS) -o bin/hyperax ./cmd/hyperax

test:
	go test ./... -race -count=1

lint:
	golangci-lint run ./...

dev:
	go run ./cmd/hyperax serve

ui-build:
	cd ui && npm ci && npm run build

release:
	goreleaser release --clean

generate:
	cd sql && sqlc generate

clean:
	rm -rf bin/ ui/dist/

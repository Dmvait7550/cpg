.PHONY: all build test lint clean

all: lint test build

build:
	go build -ldflags "-X main.version=$$(git describe --tags --always --dirty)" -o bin/cpg ./cmd/cpg/

test:
	go test ./... -count=1 -race

lint:
	golangci-lint run

clean:
	rm -rf bin/

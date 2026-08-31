.PHONY: all build run test test-coverage clean lint

all: test build

build:
	go build -o bin/api.exe ./cmd/api

run:
	go run ./cmd/api

test:
	go test -v -race ./...

test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf bin/ coverage.out coverage.html

.PHONY: all build run test test-coverage clean lint

all: test build

build:
	go build -o bin/api.exe ./cmd/api

run:
	go run ./cmd/api

test:
	go test -v -race ./...

# Mongo/app integration tests skip without MONGO_TEST_URI; this target starts a throwaway Mongo for them.
test-coverage:
	docker run -d --rm --name base-wealth-test-mongo -p 27018:27017 mongo:7
	until docker exec base-wealth-test-mongo mongosh --quiet --eval 1 >/dev/null 2>&1; do sleep 1; done
	MONGO_TEST_URI=mongodb://localhost:27018 go test -race -coverprofile=coverage.out ./...; s=$$?; docker stop base-wealth-test-mongo; exit $$s
	go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out | awk '/^total:/ { sub("%","",$$3); print "coverage: " $$3 "%"; if ($$3+0 < 85) { print "below 85%"; exit 1 } }'

clean:
	rm -rf bin/ coverage.out coverage.html

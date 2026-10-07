.PHONY: all build run test test-coverage clean lint

all: test build

build:
	go build -o bin/api.exe ./cmd/api

run:
	go run ./cmd/api

test:
	go test -v -race ./...

# Mongo/app integration tests skip without MONGO_TEST_URI; this target starts a throwaway Mongo for them, as a
# single-node replica set (transactions need one). Its data lives in RAM (--tmpfs): every test creates and drops
# its own database, and on disk that's most of their time (~4x slower).
test-coverage:
	docker run -d --rm --name base-wealth-test-mongo --tmpfs /data/db -p 127.0.0.1:27018:27017 mongo:7 --replSet rs0 --bind_ip_all
	until docker exec base-wealth-test-mongo mongosh --quiet --eval "try { rs.status().ok } catch (e) { rs.initiate({ _id: 'rs0', members: [{ _id: 0, host: 'localhost:27017' }] }).ok }" 2>/dev/null | grep -q 1; do sleep 1; done
	until docker exec base-wealth-test-mongo mongosh --quiet --eval "db.hello().isWritablePrimary" 2>/dev/null | grep -q true; do sleep 1; done
	MONGO_TEST_URI="mongodb://127.0.0.1:27018/?directConnection=true" go test -race -coverprofile=coverage.out ./...; s=$$?; docker stop base-wealth-test-mongo; exit $$s
	go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out | awk '/^total:/ { sub("%","",$$3); print "coverage: " $$3 "%"; if ($$3+0 < 85) { print "below 85%"; exit 1 } }'

clean:
	rm -rf bin/ coverage.out coverage.html

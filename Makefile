.PHONY: build test test-race vet fmt tidy download verify list run migrate clean

build:
	go build ./...

test:
	go test -timeout=300s -count=1 ./...

test-race:
	go test -race -timeout=420s -count=1 ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

tidy:
	go mod tidy

download:
	go mod download

verify:
	go mod verify

list:
	go list -mod=readonly -m all

run:
	go run ./cmd/scheduler

migrate:
	go run ./cmd/migrate

clean:
	rm -rf data/*.db data/*.db-wal data/*.db-shm

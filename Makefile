TAGS := sqlite_fts5

.PHONY: build run clean setup test

setup:
	go run -tags "$(TAGS)" ./cmd/main.go setup

build:
	go build -tags "$(TAGS)" -o mmq ./cmd/main.go

run:
	go run -tags "$(TAGS)" ./cmd/main.go

test:
	go test -tags "$(TAGS)" ./...

clean:
	rm -f mmq

.PHONY: build test lint vet run tidy

build:
	go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

test:
	go test -race -timeout 60s ./...

vet:
	go vet ./...

lint: vet

run:
	go run ./cmd/server

tidy:
	go mod tidy

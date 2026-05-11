.PHONY: build test lint vet arch run tidy

build:
	go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

test:
	go test -race -timeout 120s ./...

vet:
	go vet ./...

arch:
	$(shell go env GOPATH)/bin/go-arch-lint check --arch-file .arch.yaml

lint: vet arch

run:
	go run ./cmd/server

tidy:
	go mod tidy

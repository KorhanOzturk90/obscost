.PHONY: build test lint tidy fmt

build:
	go build -o bin/promcost ./cmd/promcost

test:
	go test ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

fmt:
	gofmt -l -w .

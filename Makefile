.PHONY: build test lint tidy fmt release-snapshot

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

# Reproduces release.yml's PR dry run: validate .goreleaser.yaml, then build
# every archive and both image architectures into dist/ without publishing.
# Needs goreleaser v2 and docker with buildx.
release-snapshot:
	goreleaser check
	goreleaser release --snapshot --clean

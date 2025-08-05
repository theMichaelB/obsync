.PHONY: all build test lint fmt clean install

GO := go
BINARY := obsync
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags="-X main.version=$(VERSION) -s -w"

all: lint test build

build:
	$(GO) build $(LDFLAGS) -o $(BINARY) ./cmd/obsync

test:
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

test-integration:
	$(GO) test -v -tags=integration ./test/integration/...

lint:
	golangci-lint run

fmt:
	$(GO) fmt ./...
	goimports -w .

clean:
	rm -f $(BINARY) coverage.out coverage.html

install: build
	cp $(BINARY) $(GOPATH)/bin/

# Development helpers
dev-setup:
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	$(GO) mod download

coverage-html: test
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
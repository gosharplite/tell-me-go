# Copyright (c) 2026 gosharplite@gmail.com
# SPDX-License-Identifier: MIT

VERSION ?= dev

.PHONY: build test test-race tidy fmt help

help:
	@echo "tell-me-go development tasks:"
	@echo "  make build      - Build binary with dynamic version (set VERSION=x.y.z)"
	@echo "  make test       - Run all tests (standard)"
	@echo "  make test-race  - Run tests with race detector (AI-SAFE, package-by-package)"
	@echo "  make test-coverage - Run tests with coverage (excludes mocks/generated)"
	@echo "  make tidy       - Tidy and vendor dependencies"
	@echo "  make fmt        - Format code"

build:
	go build -ldflags="-X 'main.version=$(VERSION)'" -o tell-me-go ./cmd/tell-me-go

test:
	go test ./...

# AI-SAFE RACE TEST: 
# Running 'go test -race ./...' globally times out in AI environments (>60s).
# This target iterates through packages sequentially to ensure full coverage 
# and stability in all environments (including Windows with Winlibs).
test-race:
	@echo "Running race tests package-by-package..."
	@for pkg in $$(go list ./...); do \
		echo "Testing $$pkg..."; \
		go test -race -timeout 60s $$pkg || exit 1; \
	done

tidy:
	go mod tidy

fmt:
	go fmt ./...

# Generate coverage report excluding mocks and generated files
.PHONY: test-coverage
test-coverage:
	go test -coverprofile=coverage.raw ./...
	grep -v -E "mock\.go|generated" coverage.raw > coverage.out
	go tool cover -func=coverage.out

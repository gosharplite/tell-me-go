# Copyright (c) 2026 gosharplite@gmail.com
# SPDX-License-Identifier: MIT

.PHONY: test test-race tidy fmt help

help:
	@echo "tell-me-go development tasks:"
	@echo "  make test       - Run all tests (standard)"
	@echo "  make test-race  - Run tests with race detector (AI-SAFE, package-by-package)"
	@echo "  make tidy       - Tidy and vendor dependencies"
	@echo "  make fmt        - Format code"

test:
	go test ./...

# AI-SAFE RACE TEST: 
# Running 'go test -race ./...' globally times out in AI environments (>60s).
# This target iterates through packages to stay within tool execution limits.
test-race:
	@echo "Running race tests package-by-package..."
	@for pkg in $$(go list ./...); do \
		echo "Testing $$pkg..."; \
		go test -race -timeout 45s $$pkg || exit 1; \
	done

tidy:
	go mod tidy

fmt:
	go fmt ./...

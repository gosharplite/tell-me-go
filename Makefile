# Copyright (c) 2026 gosharplite@gmail.com
# SPDX-License-Identifier: MIT

VERSION ?= dev

# Detect if we are running in Windows CMD vs. a POSIX-compliant shell
ifeq ($(OS),Windows_NT)
    # On Windows, SHELL might be set to sh.exe even if we are in CMD.
    # Check for standard POSIX indicators in the SHELL path.
    ifneq (,$(findstring /sh,$(SHELL)))
        IS_POSIX := true
    else ifneq (,$(findstring /bash,$(SHELL)))
        IS_POSIX := true
    else ifeq ($(SHELL),sh)
        # Fallback for systems where SHELL is just 'sh'
        IS_POSIX := true
    else ifeq ($(SHELL),bash)
        # Fallback for systems where SHELL is just 'bash'
        IS_POSIX := true
    else
        IS_POSIX := false
    endif
else
    IS_POSIX := true
endif

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
# Running 'go test -race ./...' globally can time out in constrained environments.
# This target iterates through packages sequentially for stability.
test-race:
ifeq ($(IS_POSIX),true)
	@echo "Running race tests package-by-package (POSIX mode)..."
	@for pkg in $$(go list ./...); do \
		echo "Testing $$pkg..."; \
		go test -race -timeout 60s $$pkg || exit 1; \
	done
else
	@echo "Running race tests package-by-package (Windows CMD mode)..."
	@for /f "tokens=*" %%p in ('go list ./...') do ( \
		echo Testing %%p... & \
		go test -race -timeout 60s %%p || exit /b 1 \
	)
endif

tidy:
	go mod tidy

fmt:
	go fmt ./...

# Generate coverage report excluding mocks and generated files
.PHONY: test-coverage
test-coverage:
	go test -coverprofile=coverage.raw ./...
ifeq ($(IS_POSIX),true)
	@grep -v -E "mock\.go|generated" coverage.raw > coverage.out
else
	@findstr /V /R "mock\.go generated" coverage.raw > coverage.out
endif
	go tool cover -func=coverage.out

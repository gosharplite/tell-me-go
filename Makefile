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

.PHONY: build test test-race tidy fmt help verify-testutil-convention

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

test: verify-testutil-convention
	go test ./...

# Verify ADR-021: no testutil packages (centralized test-double dump).
# Test doubles must live in <pkg>/<pkg>test/ sub-packages.
verify-testutil-convention:
ifeq ($(IS_POSIX),true)
	@echo "Checking for testutil convention violations (ADR-021)..."
	@VIOLATIONS="$$( ( \
		find . -path '*/internal/*/testutil/*.go' -not -path '*/.git/*' -not -path '*/vendor/*' 2>/dev/null; \
		grep -rn --include='*.go' 'internal/.*/testutil' . --exclude-dir=vendor --exclude-dir=.git 2>/dev/null \
	) | sort -u )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-021 violation: found references to a 'testutil' package."; \
		echo "   Test doubles must live in <pkg>/<pkg>test/ sub-packages per ADR-021."; \
		echo "   See: docs/adr/2026-04-test-doubles-in-pkgtest-subpackages.md"; \
		echo ""; \
		echo "Violating files:"; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		echo "Fix: move test helpers to the appropriate *test sub-package."; \
		exit 1; \
	fi
	@echo "  ✓ No testutil violations found."
else
	@echo "Checking for testutil convention violations (ADR-021)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$violations = @(); \
		Get-ChildItem -Path . -Recurse -Filter '*.go' | Where-Object { \
			$$_.FullName -match '\\internal\\.*\\testutil\\' -and \
			$$_.FullName -notmatch '\\\\\.git\\\\' -and \
			$$_.FullName -notmatch '\\\\vendor\\\\' \
		} | ForEach-Object { $$violations += $$_.FullName }; \
		Get-ChildItem -Path . -Recurse -Filter '*.go' | Where-Object { \
			$$_.FullName -notmatch '\\\\vendor\\\\' -and \
			$$_.FullName -notmatch '\\\\\.git\\\\' \
		} | ForEach-Object { \
			$$matches = Select-String -Path $$_.FullName -Pattern 'internal/.*/testutil' -SimpleMatch:$$false; \
			if ($$matches) { foreach ($$m in $$matches) { $$violations += ('{0}:{1}:{2}' -f $$m.Path, $$m.LineNumber, $$m.Line.Trim()) } } \
		}; \
		if ($$violations.Count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ ADR-021 violation: found references to a testutil package.'; \
			Write-Host '   Test doubles must live in <pkg>/<pkg>test/ sub-packages per ADR-021.'; \
			Write-Host '   See: docs/adr/2026-04-test-doubles-in-pkgtest-subpackages.md'; \
			Write-Host ''; \
			Write-Host 'Violating files:'; \
			$$violations | Sort-Object -Unique | ForEach-Object { Write-Host $$_ }; \
			Write-Host ''; \
			Write-Host 'Fix: move test helpers to the appropriate *test sub-package.'; \
			exit 1 \
		} \
	"
	@echo "  ✓ No testutil violations found."
endif

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

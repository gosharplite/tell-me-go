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

.PHONY: build test test-race tidy fmt help verify-testutil-convention verify-no-testing-import verify-internal-bridge-brand

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

test: verify-testutil-convention verify-no-testing-import verify-internal-bridge-brand
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

# Verify ADR-022: no production file imports the "testing" package.
# Test-only helpers that legitimately need "testing" must live in a
# *test or *internal sibling sub-package whitelisted below. Any other
# non-_test.go file matching '"testing"' fails the build.
#
# Whitelist:
#   internal/domain/events/eventstest/  (CleanupBus helper)
#   internal/agent/agentinternal/       (white-box bridge — but currently
#                                        does not import "testing"; kept
#                                        as a safety net for future
#                                        helpers in that package)
verify-no-testing-import:
ifeq ($(IS_POSIX),true)
	@echo "Checking that no production file imports \"testing\" (ADR-022)..."
	@VIOLATIONS="$$( grep -rln '^[[:space:]]*\"testing\"' --include='*.go' . \
		| grep -v '_test\.go$$' \
		| grep -v '^\./internal/domain/events/eventstest/' \
		| grep -v '^\./internal/agent/agentinternal/' \
		| sort -u )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-022 violation: production file imports \"testing\"."; \
		echo "   Test helpers that need \"testing\" must live in a *test or"; \
		echo "   *internal sibling sub-package. See:"; \
		echo "   docs/adr/2026-04-test-only-access-via-agentinternal-bridge.md"; \
		echo ""; \
		echo "Violating files:"; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		echo "Fix: relocate the helper to a <pkg>test/ sub-package, or update"; \
		echo "the whitelist in this Makefile target if a new such package is"; \
		echo "being added (and document the addition in ADR-022)."; \
		exit 1; \
	fi
	@echo "  ✓ No \"testing\" imports outside whitelisted sub-packages."
else
	@echo "Checking that no production file imports \"testing\" (ADR-022)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$violations = @(); \
		Get-ChildItem -Path . -Recurse -Filter '*.go' | Where-Object { \
			$$_.Name -notlike '*_test.go' -and \
			$$_.FullName -notmatch '\\\\internal\\\\domain\\\\events\\\\eventstest\\\\' -and \
			$$_.FullName -notmatch '\\\\internal\\\\agent\\\\agentinternal\\\\' -and \
			$$_.FullName -notmatch '\\\\\.git\\\\' -and \
			$$_.FullName -notmatch '\\\\vendor\\\\' \
		} | ForEach-Object { \
			$$matches = Select-String -Path $$_.FullName -Pattern '^\s*\"testing\"'; \
			if ($$matches) { foreach ($$m in $$matches) { $$violations += ('{0}:{1}' -f $$m.Path, $$m.LineNumber) } } \
		}; \
		if ($$violations.Count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ ADR-022 violation: production file imports testing.'; \
			Write-Host '   See: docs/adr/2026-04-test-only-access-via-agentinternal-bridge.md'; \
			Write-Host ''; \
			$$violations | Sort-Object -Unique | ForEach-Object { Write-Host $$_ }; \
			exit 1 \
		} \
	"
	@echo "  ✓ No \"testing\" imports outside whitelisted sub-packages."
endif

# Verify ADR-022: the *ForInternalUse brand is consumed only by the
# agentinternal bridge package. The brand exists to make every
# production-side touchpoint of the white-box test bridge trivially
# greppable; this target enforces that contract.
#
# Allowed locations:
#   internal/agent/agent.go            (declares the brand on the interface)
#   internal/agent/internal_bridge.go  (implements the brand on *agent)
#   internal/agent/agentinternal/      (the lone consumer package)
#   any *_test.go file                 (tests directly under the agent
#                                       tree may legitimately use the
#                                       brand for white-box assertions)
verify-internal-bridge-brand:
ifeq ($(IS_POSIX),true)
	@echo "Checking *ForInternalUse brand containment (ADR-022)..."
	@VIOLATIONS="$$( grep -rln 'ForInternalUse' --include='*.go' . \
		| grep -v '_test\.go$$' \
		| grep -v '^\./internal/agent/agent\.go$$' \
		| grep -v '^\./internal/agent/internal_bridge\.go$$' \
		| grep -v '^\./internal/agent/agentinternal/' \
		| sort -u )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-022 violation: *ForInternalUse used outside the bridge."; \
		echo "   Only internal/agent/{agent.go,internal_bridge.go} and the"; \
		echo "   agentinternal sibling package may reference the brand."; \
		echo "   See: docs/adr/2026-04-test-only-access-via-agentinternal-bridge.md"; \
		echo ""; \
		echo "Violating files:"; \
		echo "$$VIOLATIONS"; \
		exit 1; \
	fi
	@echo "  ✓ *ForInternalUse brand contained to bridge package."
else
	@echo "Checking *ForInternalUse brand containment (ADR-022)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$violations = @(); \
		Get-ChildItem -Path . -Recurse -Filter '*.go' | Where-Object { \
			$$_.Name -notlike '*_test.go' -and \
			$$_.FullName -notmatch '\\\\internal\\\\agent\\\\agent\.go$$' -and \
			$$_.FullName -notmatch '\\\\internal\\\\agent\\\\internal_bridge\.go$$' -and \
			$$_.FullName -notmatch '\\\\internal\\\\agent\\\\agentinternal\\\\' -and \
			$$_.FullName -notmatch '\\\\\.git\\\\' -and \
			$$_.FullName -notmatch '\\\\vendor\\\\' \
		} | ForEach-Object { \
			$$matches = Select-String -Path $$_.FullName -Pattern 'ForInternalUse' -SimpleMatch:$$true; \
			if ($$matches) { foreach ($$m in $$matches) { $$violations += ('{0}:{1}' -f $$m.Path, $$m.LineNumber) } } \
		}; \
		if ($$violations.Count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ ADR-022 violation: *ForInternalUse used outside bridge.'; \
			Write-Host '   See: docs/adr/2026-04-test-only-access-via-agentinternal-bridge.md'; \
			Write-Host ''; \
			$$violations | Sort-Object -Unique | ForEach-Object { Write-Host $$_ }; \
			exit 1 \
		} \
	"
	@echo "  ✓ *ForInternalUse brand contained to bridge package."
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

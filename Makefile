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

.PHONY: build test test-race tidy fmt help verify-testutil-convention verify-no-testing-import verify-internal-bridge-brand verify-mock-pattern verify-no-test-sleep verify-architecture verify-adr-index lint vulncheck dead-code check check-full bench

help:
	@echo "tell-me-go development tasks:"
	@echo "  make build      - Build binary with dynamic version (set VERSION=x.y.z)"
	@echo "  make test       - Run all tests (standard)"
	@echo "  make test-race  - Run tests with race detector (AI-SAFE, package-by-package)"
	@echo "  make verify-architecture - Verify Clean/Hexagonal Architecture layer discipline"
	@echo "  make verify-no-test-sleep - Verify no time.Sleep for synchronization in tests"
	@echo "  make test-coverage - Run tests with coverage (excludes mocks/generated)"
	@echo "  make tidy       - Tidy and vendor dependencies"
	@echo "  make fmt        - Format code"
	@echo "  make lint       - Run golangci-lint static analysis"
	@echo "  make dead-code  - Run dead code detection (exports with zero inbound refs)"
	@echo "  make check      - Run full quality pipeline: fmt tidy build lint verify-architecture vulncheck test dead-code test-coverage"
	@echo "  make bench       - Run all benchmarks with memory allocation metrics"
	@echo "  make check-full - Run full quality pipeline: fmt tidy build lint verify-architecture vulncheck test dead-code test-race test-coverage"
	@echo "  make vulncheck  - Run govulncheck for known CVEs in dependencies"

build:
	go build -ldflags="-X 'main.version=$(VERSION)'" -o tell-me-go ./cmd/tell-me-go

test: verify-testutil-convention verify-no-testing-import verify-internal-bridge-brand verify-mock-pattern
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
			($$_.FullName.Replace('\', '/')) -match 'internal/.*/testutil/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch '.git/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'vendor/' \
		} | ForEach-Object { $$violations += $$_.FullName }; \
		Get-ChildItem -Path . -Recurse -Filter '*.go' | Where-Object { \
			($$_.FullName.Replace('\', '/')) -notmatch 'vendor/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch '.git/' \
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
		| grep -v '^\./internal/tools/analysis/analysistest/' \
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
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/domain/events/eventstest/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/agent/agentinternal/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/tools/analysis/analysistest/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch '.git/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'vendor/' \
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
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/agent/agent\.go$$' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/agent/internal_bridge\.go$$' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/agent/agentinternal/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch '.git/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'vendor/' \
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

verify-mock-pattern:
ifeq ($(IS_POSIX),true)
	@echo "Checking for testify/mock imports in *test/ packages (ADR-021 mock pattern)..."
	@FILES="$$( grep -rl '"github.com/stretchr/testify/mock"' --include='*.go' internal/ \
		| grep '/[^/]*test/' \
		| grep -v '_test\.go$$' \
		| grep -v 'agentinternal/' \
		| sort -u )"; \
	COUNT=$$(echo "$$FILES" | grep -c '\.go$$' || true); \
	if [ "$$COUNT" -gt 11 ]; then \
		echo ""; \
		echo "❌ ADR-021 violation: new testify/mock import in a *test/ package."; \
		echo "   Test doubles in *test/ packages must use hand-rolled function-field"; \
		echo "   mocks, not testify/mock. See ADR-021 Mock construction pattern:"; \
		echo "   docs/adr/2026-04-test-doubles-in-pkgtest-subpackages.md"; \
		echo ""; \
		echo "Files importing testify/mock:"; \
		echo "$$FILES"; \
		echo ""; \
		echo "Baseline: 11 allowed (existing tech debt — tracked for elimination)."; \
		echo "New: $$((COUNT - 11)) file(s) above baseline."; \
		exit 1; \
	fi; \
	echo "  ✓ testify/mock count: $$COUNT (baseline: 11, no new violations)"
else
	@echo "Checking for testify/mock imports in *test/ packages (ADR-021 mock pattern)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$files = Get-ChildItem -Path internal -Recurse -Filter '*.go' | Where-Object { \
			$$_.Name -notlike '*_test.go' -and \
			($$_.FullName.Replace('\', '/')) -match '/[^/]*test/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'agentinternal/' \
		} | Where-Object { \
			(Select-String -Path $$_.FullName -Pattern '\"github.com/stretchr/testify/mock\"' -SimpleMatch:$$true) \
		} | ForEach-Object { $$_.FullName.Replace('\', '/') } | Sort-Object -Unique; \
		$$count = ($$files | Measure-Object).Count; \
		if ($$count -gt 11) { \
			Write-Host ''; \
			Write-Host '❌ ADR-021 violation: new testify/mock import in a *test/ package.'; \
			Write-Host '   See: docs/adr/2026-04-test-doubles-in-pkgtest-subpackages.md'; \
			Write-Host ''; \
			Write-Host 'Files importing testify/mock:'; \
			$$files | ForEach-Object { Write-Host $$_ }; \
			Write-Host ''; \
			Write-Host ('Baseline: 11 allowed. New: ' + ($$count - 11) + ' file(s) above baseline.'); \
			exit 1 \
		}; \
		Write-Host ('  ✓ testify/mock count: ' + $$count + ' (baseline: 11, no new violations)') \
	"
endif

# Verify no time.Sleep for synchronization in test files (Issue #580 / ADR-036).
# Legitimate I/O simulation and hardware observation sleeps are explicitly allow-listed.
verify-no-test-sleep:
ifeq ($(IS_POSIX),true)
	@echo "Checking for time.Sleep synchronization in test files..."
	@# ZERO TOLERANCE: internal/ui/ tests must have zero time.Sleep
	@if grep -rn 'time\.Sleep(' --include='*_test.go' internal/ui/; then \
		echo ""; \
		echo "❌ time.Sleep found in internal/ui/ test files."; \
		echo "   Use deterministic primitives: poll loops, ready channels, or"; \
		echo "   test-controlled tick channels. See internal/agent/session/doc.go"; \
		echo "   for canonical patterns."; \
		exit 1; \
	fi
	@# ALLOW-LIST: config/watcher_test.go — only sleeps with 'simulates I/O latency' comment
	@VIOLATIONS=$$(grep -rn 'time\.Sleep(' --include='*_test.go' internal/infrastructure/config/ | grep -v 'simulates I/O latency'); \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ Undocumented time.Sleep in config test files."; \
		echo "   Allowed: only sleeps with '// simulates I/O latency' comment."; \
		echo ""; \
		echo "$$VIOLATIONS"; \
		exit 1; \
	fi
	@# ALLOW-LIST: telemetry/system_metrics_darwin_test.go only (Darwin kernel observation)
	@VIOLATIONS=$$(grep -rn 'time\.Sleep(' --include='*_test.go' internal/infrastructure/telemetry/ | grep -v 'system_metrics_darwin_test.go'); \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ time.Sleep in telemetry test files outside system_metrics_darwin_test.go."; \
		echo "   Darwin kernel tick observation sleeps are allow-listed in that file only."; \
		echo ""; \
		echo "$$VIOLATIONS"; \
		exit 1; \
	fi
	@echo "  ✓ No time.Sleep for synchronization in test files."
else
	@echo "Checking for time.Sleep synchronization in test files (Windows)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$ui = Select-String -Path 'internal/ui/*_test.go' -Pattern 'time\.Sleep' -SimpleMatch:$$false; \
		if ($$ui) { Write-Host '❌ time.Sleep in internal/ui/ test files'; exit 1 }; \
		$$cfg = Select-String -Path 'internal/infrastructure/config/*_test.go' -Pattern 'time\.Sleep' -SimpleMatch:$$false | Where-Object { $$_.Line -notmatch 'simulates I/O latency' }; \
		if ($$cfg) { Write-Host '❌ Undocumented time.Sleep in config test files'; $$cfg | ForEach-Object { Write-Host $$_ }; exit 1 }; \
		$$tel = Select-String -Path 'internal/infrastructure/telemetry/*_test.go' -Pattern 'time\.Sleep' -SimpleMatch:$$false | Where-Object { $$_.Path -notmatch 'system_metrics_darwin_test\.go' }; \
		if ($$tel) { Write-Host '❌ time.Sleep in telemetry test files outside allow-list'; $$tel | ForEach-Object { Write-Host $$_ }; exit 1 }; \
	"
	@echo "  ✓ No time.Sleep for synchronization in test files."
endif

verify-architecture:
ifeq ($(IS_POSIX),true)
	@ARCH_FAIL_ON_VIOLATION=1 go test -run TestVerifyRealArchitecture ./internal/tools/analysis/...
else
	@set ARCH_FAIL_ON_VIOLATION=1 && go test -run TestVerifyRealArchitecture ./internal/tools/analysis/...
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

lint:
	golangci-lint run ./...

# vulncheck scans dependencies for known vulnerabilities (CVEs).
# Requires govulncheck: go install golang.org/x/vuln/cmd/govulncheck@latest
vulncheck:
	govulncheck ./...

dead-code:
	go run ./cmd/deadcode

BENCH_PKGS := ./internal/agent/session/context \
              ./internal/domain/events \
              ./internal/infrastructure/history \
              ./internal/infrastructure/llm \
              ./internal/infrastructure/persistence \
              ./internal/tools/analysis \
              ./internal/tools/workspace

bench:
	go test -bench=. -benchmem -count=1 $(BENCH_PKGS)

# check runs the full quality pipeline in sequence, stopping on first failure.
# Fast/cheap checks run first so problems surface quickly.
check: fmt tidy build
	@echo "=== lint ==="
	@$(MAKE) lint
	@echo "=== verify-architecture ==="
	@$(MAKE) verify-architecture
	@echo "=== verify-mock-pattern ==="
	@$(MAKE) verify-mock-pattern
	@echo "=== verify-no-test-sleep ==="
	@$(MAKE) verify-no-test-sleep
	@echo "=== vulncheck ==="
	@$(MAKE) vulncheck
	@echo "=== test ==="
	@$(MAKE) test
	@echo "=== dead-code ==="
	@$(MAKE) dead-code
	@echo "=== test-coverage ==="
	@$(MAKE) test-coverage
	@echo ""
	@echo "All checks passed."

# check-full runs the full quality pipeline including race detection.
# Use this before pushing or merging.
check-full: fmt tidy build
	@echo "=== lint ==="
	@$(MAKE) lint
	@echo "=== verify-architecture ==="
	@$(MAKE) verify-architecture
	@echo "=== verify-mock-pattern ==="
	@$(MAKE) verify-mock-pattern
	@echo "=== verify-no-test-sleep ==="
	@$(MAKE) verify-no-test-sleep
	@echo "=== verify-adr-index ==="
	@$(MAKE) verify-adr-index
	@echo "=== vulncheck ==="
	@$(MAKE) vulncheck
	@echo "=== test ==="
	@$(MAKE) test
	@echo "=== dead-code ==="
	@$(MAKE) dead-code
	@echo "=== test-race ==="
	@$(MAKE) test-race
	@echo "=== test-coverage ==="
	@$(MAKE) test-coverage
	@echo "=== bench ==="
	@$(MAKE) bench
	@echo ""
	@echo "All checks passed (including race detection)."

# Generate coverage report excluding mocks, generated files, and the
# agentinternal delegation bridge (ADR-022 / issue #138).
.PHONY: test-coverage
test-coverage:
	go test -coverprofile=coverage.raw ./...
ifeq ($(IS_POSIX),true)
	@grep -v -E "(internal/agent/agenttest/|internal/agent/orchestrator/orchestratortest/|internal/domain/config/configtest/|internal/tools/analysis/analysistest/|internal/infrastructure/testing/)" coverage.raw > coverage.out
else
	@findstr /V /R "internal/agent/agenttest/ internal/agent/orchestrator/orchestratortest/ internal/domain/config/configtest/ internal/tools/analysis/analysistest/ internal/infrastructure/testing/" coverage.raw > coverage.out
endif
	go tool cover -func=coverage.out

# Verify every ADR file on disk is indexed in docs/adr/README.md
# and no ADR number is claimed by more than one file.
.PHONY: verify-adr-index
verify-adr-index:
	@echo "Checking ADR index consistency..."
	@errors=0; \
	for f in docs/adr/2*.md; do \
		basename=$$(basename $$f); \
		if ! grep -q "$$basename" docs/adr/README.md; then \
			echo "MISSING from index: $$basename"; \
			errors=$$((errors+1)); \
		fi; \
	done; \
	dupes=$$(grep -h '^# ADR-[0-9]*:' docs/adr/*.md | sed 's/# ADR-//;s/:.*//' | sort -n | uniq -d); \
	if [ -n "$$dupes" ]; then \
		echo "DUPLICATE ADR numbers: $$dupes"; \
		errors=$$((errors+1)); \
	fi; \
	if [ $$errors -gt 0 ]; then \
		echo "ADR index is inconsistent ($$errors errors)."; \
		exit 1; \
	fi; \
	echo "ADR index is consistent."

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

# Suppress GNU Make's sh.exe probe noise on Windows when there is no POSIX shell.
# Without this, every make and sub-make emits "The system cannot find the path specified."
ifeq ($(OS),Windows_NT)
    ifeq ($(IS_POSIX),false)
        SHELL := cmd.exe
    endif
endif

.PHONY: build test test-race tidy fmt help verify-testutil-convention verify-no-testing-import verify-internal-bridge-brand verify-mock-pattern verify-session-provider-mock verify-tools-adapter-import verify-tools-toolchain-import verify-tools-infrastructure-import verify-mcp-sdk-confinement verify-no-test-sleep verify-architecture verify-exit-query verify-transitive-gate verify-nonfix-catalog verify-ports-registry verify-adr-index verify-no-context-window-cache lint vulncheck dead-code check check-full bench fuzz fuzz-smoke modelith-lint modelith-render modelith-check modelith-drift modelith-layers modelith-vocab

help:
	@echo "tell-me-go development tasks:"
	@echo "  make build      - Build binary with dynamic version (set VERSION=x.y.z)"
	@echo "  make test       - Run all tests (standard)"
	@echo "  make test-race  - Run tests with race detector (AI-SAFE, package-by-package)"
	@echo "  make verify-architecture - Verify Clean/Hexagonal Architecture layer discipline"
	@echo "  make verify-transitive-gate - Verify the ADR-056 transitive closure gate (STRICT; part of make check/check-full)"
	@echo "  make verify-exit-query - ADR-056 Decision 1 exit query (report-only; ports realignment lens)"
	@echo "  make verify-nonfix-catalog - Verify the complexity-pin catalog partition (issue #1297)"
	@echo "  make verify-ports-registry - Verify the ports registry bijection, N<=12, stay-key liveness, and Supporting admission (issue #1343)"
	@echo "  make verify-no-test-sleep - Verify no time.Sleep for synchronization in tests"
	@echo "  make verify-tools-adapter-import - Verify tools adapter imports are confined to default_fs.go (ADR-055)"
	@echo "  make verify-tools-toolchain-import - Verify no infrastructure/toolchain imports in tools production files (issue #1325)"
	@echo "  make verify-tools-infrastructure-import - Verify no internal/infrastructure imports in tools production files (ADR-062)"
	@echo "  make verify-mcp-sdk-confinement - Verify MCP Go SDK imports are confined to internal/infrastructure/mcp/ (ADR-067)"
	@echo "  make verify-no-context-window-cache - Verify no context window cache references (ADR-057; part of make check/check-full)"
	@echo "  make test-coverage - Run tests with coverage (excludes mocks/generated)"
	@echo "  make tidy       - Tidy and vendor dependencies"
	@echo "  make fmt        - Format code"
	@echo "  make lint       - Run golangci-lint static analysis"
	@echo "  make dead-code       - Report orphan symbols (DEAD/PRIVATE); ports governance moved to verify-exit-query"
	@echo "  make check      - Run full quality pipeline: fmt tidy build lint verify-architecture vulncheck fuzz-smoke test dead-code test-coverage"
	@echo "  make check-full - Run full quality pipeline including race detection (use before push/merge)"
	@echo "  make bench       - Run all benchmarks with memory allocation metrics"
	@echo "  make fuzz       - Run all fuzz targets for 40s each (developer-invoked)"
	@echo "  make fuzz-smoke - Compile-check fuzz tests are buildable (included in make check)"
	@echo "  make modelith-lint   - Validate the domain model YAML (docs/domain-model/)"
	@echo "  make modelith-render - Regenerate the domain model Markdown from YAML"
	@echo "  make modelith-check  - CI gate: fail if the committed .md is stale"
	@echo "  make modelith-drift  - Check diff for new exports missing from domain model"
	@echo "  make modelith-layers - Verify domain entities are defined in internal/domain/"
	@echo "  make modelith-vocab  - Detect non-canonical terminology in docs"
	@echo "  make vulncheck  - Run govulncheck for known CVEs in dependencies"

build:
	go build -ldflags="-X 'main.version=$(VERSION)'" -o tell-me-go ./cmd/tell-me-go

test: verify-testutil-convention verify-no-testing-import verify-internal-bridge-brand verify-mock-pattern verify-session-provider-mock verify-tools-adapter-import verify-tools-toolchain-import verify-tools-infrastructure-import verify-mcp-sdk-confinement
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

# ADR-021: All test doubles in *test/ packages must use hand-rolled function-field
# mocks. The testify/mock migration was fully completed (see ADR-021). This guard
# enforces zero tolerance — any new testify/mock import in a *test/ package fails
# the build. There is no baseline; the debt has been eliminated.
verify-mock-pattern:
ifeq ($(IS_POSIX),true)
	@echo "Checking for testify/mock imports in *test/ packages (ADR-021 — zero tolerance)..."
	@FILES="$$( grep -rl '"github.com/stretchr/testify/mock"' --include='*.go' internal/ \
		| grep '/[^/]*test/' \
		| grep -v '_test\.go$$' \
		| grep -v 'agentinternal/' \
		| sort -u )"; \
	COUNT=$$(echo "$$FILES" | grep -c '\.go$$' || true); \
	if [ "$$COUNT" -gt 0 ]; then \
		echo ""; \
		echo "❌ ADR-021 violation: testify/mock import in a *test/ package."; \
		echo "   Test doubles in *test/ packages must use hand-rolled function-field"; \
		echo "   mocks, not testify/mock. The migration was fully completed —"; \
		echo "   there is zero tolerance for new testify/mock imports."; \
		echo "   See: docs/adr/2026-04-test-doubles-in-pkgtest-subpackages.md"; \
		echo ""; \
		echo "Violating files:"; \
		echo "$$FILES"; \
		echo ""; \
		echo "Fix: convert to hand-rolled function-field mocks per ADR-021."; \
		exit 1; \
	fi; \
	echo "  ✓ No testify/mock imports in *test/ packages (debt eliminated per ADR-021)."
else
	@echo "Checking for testify/mock imports in *test/ packages (ADR-021 — zero tolerance)..."
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
		if ($$count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ ADR-021 violation: testify/mock import in a *test/ package.'; \
			Write-Host '   The testify/mock migration was fully completed — zero tolerance.'; \
			Write-Host '   See: docs/adr/2026-04-test-doubles-in-pkgtest-subpackages.md'; \
			Write-Host ''; \
			Write-Host 'Violating files:'; \
			$$files | ForEach-Object { Write-Host $$_ }; \
			Write-Host ''; \
			Write-Host 'Fix: convert to hand-rolled function-field mocks per ADR-021.'; \
			exit 1 \
		}; \
		Write-Host '  ✓ No testify/mock imports in *test/ packages (debt eliminated per ADR-021).' \
	"
endif

# All ports.SessionProvider / ports.SessionStateProvider test doubles
# must use testfixtures.MockSessionProvider — the single canonical mock.
# Hand-rolled mocks outside agenttest/ are forbidden. This guard catches
# any new mock added to a _test.go file outside the canonical location.
verify-session-provider-mock:
ifeq ($(IS_POSIX),true)
	@echo "Checking for hand-rolled SessionProvider mocks outside agenttest/ ..."
	@VIOLATIONS="$$( grep -rn 'type.*[Ss]ession[Pp]rovider.*struct' --include='*_test.go' internal/ \
		| grep -v 'internal/agent/agenttest/' \
		| sort -u )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ hand-rolled SessionProvider mock outside agenttest/."; \
		echo "   Use testfixtures.MockSessionProvider — the single canonical mock."; \
		echo "   All other SessionProvider mocks were eliminated; new ones are forbidden."; \
		echo ""; \
		echo "Violating files:"; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		echo "Fix: replace with &testfixtures.MockSessionProvider{}."; \
		exit 1; \
	fi
	@echo "  ✓ No hand-rolled SessionProvider mocks outside agenttest/."
else
	@echo "Checking for hand-rolled SessionProvider mocks outside agenttest/ ..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$violations = @(); \
		Get-ChildItem -Path internal -Recurse -Filter '*_test.go' | Where-Object { \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/agent/agenttest/' \
		} | ForEach-Object { \
			$$matches = Select-String -Path $$_.FullName -Pattern 'type.*[Ss]ession[Pp]rovider.*struct'; \
			if ($$matches) { foreach ($$m in $$matches) { $$violations += ('{0}:{1}:{2}' -f $$m.Path, $$m.LineNumber, $$m.Line.Trim()) } } \
		}; \
		if ($$violations.Count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ hand-rolled SessionProvider mock outside agenttest/.'; \
			Write-Host '   Use testfixtures.MockSessionProvider — the single canonical mock.'; \
			Write-Host ''; \
			$$violations | Sort-Object -Unique | ForEach-Object { Write-Host $$_ }; \
			Write-Host ''; \
			Write-Host 'Fix: replace with &testfixtures.MockSessionProvider{}.'; \
			exit 1 \
		}; \
		Write-Host '  ✓ No hand-rolled SessionProvider mocks outside agenttest/.' \
	"
endif

# Verify ADR-055: the internal/infrastructure/persistence adapter import is
# confined to the two sanctioned default_fs.go files in internal/tools/
# (analysis, workspace). Every other tools-layer production file must use
# the injected domain port (persistence.FileSystem).
verify-tools-adapter-import:
ifeq ($(IS_POSIX),true)
	@echo "Checking for infrastructure/persistence adapter imports in tools production files (ADR-055)..."
	@VIOLATIONS="$$( grep -rn 'github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"' internal/tools/ --include='*.go' \
		| grep -v '_test\.go:' \
		| grep -v '^internal/tools/analysis/default_fs\.go:' \
		| grep -v '^internal/tools/workspace/default_fs\.go:' )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-055 violation: adapter import outside the sanctioned default_fs.go files."; \
		echo "   internal/tools production files may import internal/infrastructure/persistence"; \
		echo "   only in internal/tools/analysis/default_fs.go and"; \
		echo "   internal/tools/workspace/default_fs.go (each holds its package's defaultFS fallback)."; \
		echo "   Every live tool path must use the injected domain port (persistence.FileSystem)."; \
		echo "   See: docs/adr/2026-08-tools-filesystem-injection.md"; \
		echo ""; \
		echo "Violating files:"; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		echo "Fix: route through the injected FileSystem (ToolRegistrationParams.FileSystem →"; \
		echo "workspace.Register → registerSystem → newshellTool) and construct the adapter"; \
		echo "only in the package's default_fs.go."; \
		exit 1; \
	fi
	@echo "  ✓ Adapter imports confined to the two default_fs.go files."
else
	@echo "Checking for infrastructure/persistence adapter imports in tools production files (ADR-055)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$violations = @(); \
		Get-ChildItem -Path internal/tools -Recurse -Filter '*.go' | Where-Object { \
			$$_.Name -notlike '*_test.go' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/tools/analysis/default_fs\.go$$' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/tools/workspace/default_fs\.go$$' -and \
			($$_.FullName.Replace('\', '/')) -notmatch '.git/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'vendor/' \
		} | ForEach-Object { \
			$$matches = Select-String -Path $$_.FullName -Pattern 'github\.com/gosharplite/tell-me-go/internal/infrastructure/persistence"'; \
			if ($$matches) { foreach ($$m in $$matches) { $$violations += ('{0}:{1}' -f $$m.Path, $$m.LineNumber) } } \
		}; \
		if ($$violations.Count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ ADR-055 violation: adapter import outside the sanctioned default_fs.go files.'; \
			Write-Host '   See: docs/adr/2026-08-tools-filesystem-injection.md'; \
			Write-Host ''; \
			$$violations | Sort-Object -Unique | ForEach-Object { Write-Host $$_ }; \
			Write-Host ''; \
			Write-Host 'Fix: route through the injected FileSystem and construct the adapter'; \
			Write-Host 'only in the package default_fs.go.'; \
			exit 1 \
		} \
	"
	@echo "  ✓ Adapter imports confined to the two default_fs.go files."
endif

# Verify issue #1325: no internal/tools production file imports the
# internal/infrastructure/toolchain adapter. The Go toolchain runner is
# injected via the domain port (tools.ToolchainRunner) with a single
# construction in internal/infrastructure/di/toolchain_factory.go.
#
# Predicate: "production files" = non-_test.go files under internal/tools/
# OUTSIDE analysistest/ and toolstest/ (explicit path exclusions mirroring
# verify-no-testing-import). Test-layer files are deliberately exempt: the 22
# surviving toolchain.NewGoRunner constructions across coverage_parser_test.go,
# health_test.go, real_nonfix_catalog_test.go, and architecture_bench_test.go
# are the real-adapter-over-mock verification surface the e2e deferral depends
# on. ANTI-EXTENSION DECISION (ADR-060): this gate must never be extended to
# _test.go files — doing so would break those sites and destroy the
# verification surface.
verify-tools-toolchain-import:
ifeq ($(IS_POSIX),true)
	@echo "Checking for infrastructure/toolchain adapter imports in tools production files (issue #1325)..."
	@VIOLATIONS="$$( grep -rn 'github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"' internal/tools/ --include='*.go' \
		| grep -v '_test\.go:' \
		| grep -v '^internal/tools/analysis/analysistest/' \
		| grep -v '^internal/tools/toolstest/' )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ issue #1325 violation: infrastructure/toolchain import in a tools production file."; \
		echo "   The Go toolchain runner must be injected via tools.ToolchainRunner (domain port,"; \
		echo "   single construction in internal/infrastructure/di/toolchain_factory.go)."; \
		echo "   See: docs/adr/2026-08-toolchain-runner-injection.md (ADR-060)"; \
		echo ""; \
		echo "Violating files:"; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		echo "Fix: delete the direct construction and thread the injected runner through"; \
		echo "RegisterAll -> analysis.Register / developer.Register."; \
		exit 1; \
	fi
	@echo "  ✓ No infrastructure/toolchain imports in tools production files."
else
	@echo "Checking for infrastructure/toolchain adapter imports in tools production files (issue #1325)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$violations = @(); \
		Get-ChildItem -Path internal/tools -Recurse -Filter '*.go' | Where-Object { \
			$$_.Name -notlike '*_test.go' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/tools/analysis/analysistest/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/tools/toolstest/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch '.git/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'vendor/' \
		} | ForEach-Object { \
			$$matches = Select-String -Path $$_.FullName -Pattern 'github\.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"'; \
			if ($$matches) { foreach ($$m in $$matches) { $$violations += ('{0}:{1}' -f $$m.Path, $$m.LineNumber) } } \
		}; \
		if ($$violations.Count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ issue #1325 violation: infrastructure/toolchain import in a tools production file.'; \
			Write-Host '   See: docs/adr/2026-08-toolchain-runner-injection.md (ADR-060)'; \
			Write-Host ''; \
			$$violations | Sort-Object -Unique | ForEach-Object { Write-Host $$_ }; \
			Write-Host ''; \
			exit 1 \
		}; \
		Write-Host '  ✓ No infrastructure/toolchain imports in tools production files.' \
	"
endif

# Verify issue #1336 (ADR-062): no internal/tools production file imports an
# internal/infrastructure package. The ADR-055 (persistence) and ADR-060
# (toolchain) gates run before this one (6th and 7th test: prerequisites) and
# remain primary; this is the generalized recurrence backstop for the whole
# tools→infrastructure class. Predicate is module-prefixed so the bare
# composition-root string literals (architecture.go:296-297,
# exit_query.go:235-236) cannot false-positive.
#
# Predicate: "production files" = non-_test.go files under internal/tools/
# OUTSIDE the two sanctioned default_fs.go paths (ADR-055) and
# analysistest//toolstest/ (explicit path exclusions mirroring
# verify-tools-toolchain-import). Test-layer files are deliberately exempt:
# ~35 tools test files legitimately import infrastructure.
verify-tools-infrastructure-import:
ifeq ($(IS_POSIX),true)
	@echo "Checking for infrastructure imports in tools production files (ADR-062)..."
	@VIOLATIONS="$$( grep -rn 'github.com/gosharplite/tell-me-go/internal/infrastructure/' internal/tools/ --include='*.go' \
		| grep -v '_test\.go:' \
		| grep -v '^internal/tools/analysis/default_fs\.go:' \
		| grep -v '^internal/tools/workspace/default_fs\.go:' \
		| grep -v '^internal/tools/analysis/analysistest/' \
		| grep -v '^internal/tools/toolstest/' )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-062 violation: a tools production file imports an infrastructure package."; \
		echo "   Tools-layer production code may not import internal/infrastructure/ directly."; \
		echo "   This is the general tools→infrastructure gate (ADR-062). The ADR-055 and"; \
		echo "   ADR-060 gates for persistence and toolchain run before this one and remain"; \
		echo "   primary: an edge this gate reports is, by construction, neither persistence"; \
		echo "   nor toolchain."; \
		echo ""; \
		echo "   Triage:"; \
		echo "   1. Dependency-free utility? Move it to internal/pkg/ and import it from"; \
		echo "      there. internal/pkg may depend only on internal/domain and other"; \
		echo "      internal/pkg (ADR-062, Decision 2)."; \
		echo "   2. Adapter for a domain port? Follow the ADR-055/060 injection pattern:"; \
		echo "      define the port in internal/domain, construct the adapter once at the"; \
		echo "      composition root (internal/infrastructure/di), inject it through"; \
		echo "      registration. See docs/adr/2026-08-tools-filesystem-injection.md"; \
		echo "      (ADR-055) and docs/adr/2026-08-toolchain-runner-injection.md (ADR-060)."; \
		echo "   3. Otherwise: a gate exception is required — architect-sanctioned,"; \
		echo "      ADR-cited, whitelist-edit, per ADR-062 Decision 5 (Gate-Exception"; \
		echo "      Contract). Edit the exclusion list in the verify-tools-infrastructure-import"; \
		echo "      target of the Makefile AND record a NEW ADR adjudicating the edge (the"; \
		echo "      R1 pattern: persistence → ADR-055, toolchain → ADR-060), in the same"; \
		echo "      change. See docs/adr/2026-08-encoding-relocation-and-tools-infrastructure-gate.md"; \
		echo "      (ADR-062), Decision 5."; \
		echo ""; \
		echo "   Violating files:"; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		echo "   Fix: apply triage 1 or 2; if neither applies, follow triage 3 (ADR-062"; \
		echo "   Decision 5 — Gate-Exception Contract: docs/adr/2026-08-encoding-relocation-and-tools-infrastructure-gate.md)."; \
		exit 1; \
	fi
	@echo "  ✓ No infrastructure imports in tools production files."
else
	@echo "Checking for infrastructure imports in tools production files (ADR-062)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$violations = @(); \
		Get-ChildItem -Path internal/tools -Recurse -Filter '*.go' | Where-Object { \
			$$_.Name -notlike '*_test.go' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/tools/analysis/default_fs\.go$$' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/tools/workspace/default_fs\.go$$' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/tools/analysis/analysistest/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/tools/toolstest/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch '.git/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'vendor/' \
		} | ForEach-Object { \
			$$matches = Select-String -Path $$_.FullName -Pattern 'github\.com/gosharplite/tell-me-go/internal/infrastructure/'; \
			if ($$matches) { foreach ($$m in $$matches) { $$violations += ('{0}:{1}' -f $$m.Path, $$m.LineNumber) } } \
		}; \
		if ($$violations.Count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ ADR-062 violation: a tools production file imports an infrastructure package.'; \
			Write-Host '   Tools-layer production code may not import internal/infrastructure/ directly.'; \
			Write-Host '   This is the general tools→infrastructure gate (ADR-062). The ADR-055 and'; \
			Write-Host '   ADR-060 gates for persistence and toolchain run before this one and remain'; \
			Write-Host '   primary: an edge this gate reports is, by construction, neither persistence'; \
			Write-Host '   nor toolchain.'; \
			Write-Host ''; \
			Write-Host '   Triage:'; \
			Write-Host '   1. Dependency-free utility? Move it to internal/pkg/ and import it from'; \
			Write-Host '      there. internal/pkg may depend only on internal/domain and other'; \
			Write-Host '      internal/pkg (ADR-062, Decision 2).'; \
			Write-Host '   2. Adapter for a domain port? Follow the ADR-055/060 injection pattern:'; \
			Write-Host '      define the port in internal/domain, construct the adapter once at the'; \
			Write-Host '      composition root (internal/infrastructure/di), inject it through'; \
			Write-Host '      registration. See docs/adr/2026-08-tools-filesystem-injection.md'; \
			Write-Host '      (ADR-055) and docs/adr/2026-08-toolchain-runner-injection.md (ADR-060).'; \
			Write-Host '   3. Otherwise: a gate exception is required — architect-sanctioned,'; \
			Write-Host '      ADR-cited, whitelist-edit, per ADR-062 Decision 5 (Gate-Exception'; \
			Write-Host '      Contract). Edit the exclusion list in the verify-tools-infrastructure-import'; \
			Write-Host '      target of the Makefile AND record a NEW ADR adjudicating the edge (the'; \
			Write-Host '      R1 pattern: persistence → ADR-055, toolchain → ADR-060), in the same'; \
			Write-Host '      change. See docs/adr/2026-08-encoding-relocation-and-tools-infrastructure-gate.md'; \
			Write-Host '      (ADR-062), Decision 5.'; \
			Write-Host ''; \
			Write-Host '   Violating files:'; \
			$$violations | Sort-Object -Unique | ForEach-Object { Write-Host $$_ }; \
			Write-Host ''; \
			Write-Host '   Fix: apply triage 1 or 2; if neither applies, follow triage 3 (ADR-062'; \
			Write-Host '   Decision 5 — Gate-Exception Contract: docs/adr/2026-08-encoding-relocation-and-tools-infrastructure-gate.md).'; \
			exit 1 \
		} \
	"
	@echo "  ✓ No infrastructure imports in tools production files."
endif

# Verify ADR-067: the github.com/modelcontextprotocol/go-sdk import is
# strictly confined to internal/infrastructure/mcp/ (production and test
# files). The MCP adapter is the only package allowed to know the wire
# protocol; every other layer consumes the tools.MCPClient domain port
# (internal/domain/tools/mcp_client.go) with zero third-party dependencies.
# This is the ADR-055/060 injection pattern applied to MCP.
verify-mcp-sdk-confinement:
ifeq ($(IS_POSIX),true)
	@echo "Checking MCP Go SDK import confinement (ADR-067)..."
	@VIOLATIONS="$$( grep -rn 'github.com/modelcontextprotocol/go-sdk' --include='*.go' --exclude-dir=vendor --exclude-dir=.git . \
		| grep -v '^\./internal/infrastructure/mcp/' )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-067 violation: MCP Go SDK import outside internal/infrastructure/mcp/."; \
		echo "   The github.com/modelcontextprotocol/go-sdk import is strictly confined"; \
		echo "   to internal/infrastructure/mcp/ (production and test files)."; \
		echo "   No other package may import the SDK — consume tools.MCPClient instead."; \
		echo "   See: docs/adr/2026-08-mcp-client-architecture.md"; \
		echo ""; \
		echo "Violating files:"; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		echo "Fix: move the MCP SDK adapter code into internal/infrastructure/mcp/ and"; \
		echo "have other layers consume the tools.MCPClient domain port"; \
		echo "(internal/domain/tools/mcp_client.go)."; \
		exit 1; \
	fi
	@echo "  ✓ MCP Go SDK imports strictly confined to internal/infrastructure/mcp/ (ADR-067)."
else
	@echo "Checking MCP Go SDK import confinement (ADR-067)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$violations = @(); \
		Get-ChildItem -Path . -Recurse -Filter '*.go' | Where-Object { \
			($$_.FullName.Replace('\', '/')) -notmatch 'internal/infrastructure/mcp/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch '.git/' -and \
			($$_.FullName.Replace('\', '/')) -notmatch 'vendor/' \
		} | ForEach-Object { \
			$$matches = Select-String -Path $$_.FullName -Pattern 'github\.com/modelcontextprotocol/go-sdk' -SimpleMatch:$$false; \
			if ($$matches) { foreach ($$m in $$matches) { $$violations += ('{0}:{1}' -f $$m.Path, $$m.LineNumber) } } \
		}; \
		if ($$violations.Count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ ADR-067 violation: MCP Go SDK import outside internal/infrastructure/mcp/.'; \
			Write-Host '   The github.com/modelcontextprotocol/go-sdk import is strictly confined'; \
			Write-Host '   to internal/infrastructure/mcp/ (production and test files).'; \
			Write-Host '   See: docs/adr/2026-08-mcp-client-architecture.md'; \
			Write-Host ''; \
			$$violations | Sort-Object -Unique | ForEach-Object { Write-Host $$_ }; \
			Write-Host ''; \
			Write-Host 'Fix: move the MCP SDK adapter code into internal/infrastructure/mcp/ and'; \
			Write-Host 'have other layers consume the tools.MCPClient domain port.'; \
			exit 1 \
		}; \
		Write-Host '  ✓ MCP Go SDK imports strictly confined to internal/infrastructure/mcp/ (ADR-067).' \
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
		$$ui = Select-String -Path 'internal/ui/*_test.go' -Pattern 'time\.Sleep\(' -SimpleMatch:$$false; \
		if ($$ui) { Write-Host '❌ time.Sleep in internal/ui/ test files'; exit 1 }; \
		$$cfg = Select-String -Path 'internal/infrastructure/config/*_test.go' -Pattern 'time\.Sleep\(' -SimpleMatch:$$false | Where-Object { $$_.Line -notmatch 'simulates I/O latency' }; \
		if ($$cfg) { Write-Host '❌ Undocumented time.Sleep in config test files'; $$cfg | ForEach-Object { Write-Host $$_ }; exit 1 }; \
		$$tel = Select-String -Path 'internal/infrastructure/telemetry/*_test.go' -Pattern 'time\.Sleep\(' -SimpleMatch:$$false | Where-Object { $$_.Path -notmatch 'system_metrics_darwin_test\.go' }; \
		if ($$tel) { Write-Host '❌ time.Sleep in telemetry test files outside allow-list'; $$tel | ForEach-Object { Write-Host $$_ }; exit 1 }; \
	"
	@echo "  ✓ No time.Sleep for synchronization in test files."
endif

verify-architecture:
ifeq ($(IS_POSIX),true)
	@go test -tags=arch -run TestVerifyRealArchitecture ./internal/tools/analysis -args -strict-arch=true
	@echo "=== modelith-layers ==="
	@$(MAKE) modelith-layers
else
	@go test -tags=arch -run TestVerifyRealArchitecture ./internal/tools/analysis -args -strict-arch=true
	@echo "=== modelith-layers ==="
	@$(MAKE) modelith-layers
endif

# Verify the ADR-056 transitive closure gate (issue #1300): prints the
# report separating "decision required" rows from "approved constant" rows.
# -v is required so go test surfaces the report's stdout on a passing run.
# STRICT since the 2026-08 ratification (39/39 whitelist entries accepted):
# any consumer whose closure exceeds its whitelist or direct imports now
# FAILS the gate — new closure growth must be adjudicated. Flip back to
# report-only (-transitive-gate-report-only=true) only for diagnosis.
# Wired into check/check-full (ADR-056 enforcement is mandatory); kept
# separate from verify-architecture so the two gates fail independently.
verify-transitive-gate:
ifeq ($(IS_POSIX),true)
	@go test -v -tags=arch -run TestVerifyTransitiveClosureGate ./internal/tools/analysis -args -transitive-gate-report-only=false
else
	@go test -v -tags=arch -run TestVerifyTransitiveClosureGate ./internal/tools/analysis -args -transitive-gate-report-only=false
endif

# ADR-056 Decision 1 exit query (report-only — never fails the build).
# Surfaces ports seams eligible for realignment adjudication. Quiet by
# default: one governance line when all candidates are documented stays;
# the full table prints when a NEW candidate exists or with
# -exit-query-verbose. Wired into check/check-full so ports governance
# drift is always observed.
verify-exit-query:
	@go run ./cmd/deadcode -exit-query

# Verify the complexity-pin catalog partition (issue #1297) PLUS the
# coverage-pin and detailed-coverage-report gates: runs the real
# GatherComplexities against the live INTENTIONAL_NON_FIXES.md catalog
# and asserts the cataloged/alert partition, then runs the live coverage
# matcher against the catalog and the end-to-end detailed-coverage
# reports for every cataloged-gap package. The -run filter uses DOUBLE
# quotes because the | alternation must survive both sh and cmd.exe
# (single quotes are literal in cmd.exe and would match nothing — a
# vacuous pass). -v is required so transcripts surface the seven gate
# PASS lines. RED-first: the gate must fail against a drifted catalog —
# never weaken it to land green.
verify-nonfix-catalog:
ifeq ($(IS_POSIX),true)
	@go test -v -tags=arch -run "TestVerifyNonFixCatalog|TestVerifyCoveragePinsMatchLiveCatalog|TestDetailedCoverageReport" ./internal/tools/analysis
else
	@go test -v -tags=arch -run "TestVerifyNonFixCatalog|TestVerifyCoveragePinsMatchLiveCatalog|TestDetailedCoverageReport" ./internal/tools/analysis
endif

# Verify the ports registry (issue #1343, ADR-064): runs the live
# arch-tagged gate against internal/domain/ports/doc.go, enforcing the
# registry bijection, the N ≤ 12 family bound, the ADR-056 stay-key liveness,
# and the 5-clause Supporting admission rule. Zero violations = pass.
verify-ports-registry:
ifeq ($(IS_POSIX),true)
	@go test -tags=arch -run TestVerifyPortsRegistry ./internal/tools/analysis
else
	@go test -tags=arch -run TestVerifyPortsRegistry ./internal/tools/analysis
endif

# AI-SAFE RACE TEST: 
# Running 'go test -race ./...' globally can time out in constrained environments.
# This target iterates through packages sequentially for stability.
test-race:
ifeq ($(IS_POSIX),true)
	@echo "Running race tests package-by-package (POSIX mode)..."
	@for pkg in $$(go list ./...); do \
		echo "Testing $$pkg..."; \
		go test -race -timeout 180s $$pkg || exit 1; \
	done
else
	@echo "Running race tests package-by-package (Windows CMD mode)..."
	@go clean -testcache
	@for /f "tokens=*" %%p in ('go list ./...') do ( \
		echo Testing %%p... & \
		go test -race -timeout 600s %%p || exit /b 1 \
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

# dead-code reports the orphan scan only (DEAD/PRIVATE rows + the "No dead
# code found." case). The ADR-056 Decision 1 exit query moved to
# verify-exit-query.
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

# fuzz runs all fuzz targets for 40 seconds each, iterating per-package
# because go test -fuzz does not support ./... wildcard expansion.
# This is developer-invoked only — not part of the check pipeline.
# Use make fuzz-smoke for the fast PR-gating compile check.
fuzz:
ifeq ($(IS_POSIX),true)
	@for pkg in $$(go list ./...); do \
		targets=$$(go test -list='Fuzz' $$pkg 2>/dev/null | grep '^Fuzz'); \
		if [ -z "$$targets" ]; then \
			echo "  ✓ $$pkg (no fuzz targets, skipping)"; \
		else \
			for target in $$targets; do \
				echo "  fuzz $$pkg/$$target..."; \
				go test -fuzz=^$$target$$ -fuzztime=40s -run=NONEXISTENT $$pkg || exit 1; \
			done; \
		fi; \
	done
else
	@for /f "tokens=*" %%p in ('go list ./...') do ( \
		for /f "tokens=*" %%t in ('go test -list=Fuzz %%p 2^>nul ^| findstr /R "^Fuzz"') do ( \
			echo fuzz %%p/%%t... & \
			go test -fuzz=^%%t$$ -fuzztime=40s -run=NONEXISTENT %%p || exit /b 1 \
		) \
	)
endif

# Domain model validation (modelith).
# Requires modelith built from the gosharplite fork at feat/self-domain-model
# (a fork of stacklok/modelith — upstream may behave differently):
#   go install github.com/gosharplite/modelith/cmd/modelith@feat/self-domain-model
# Falls back to 'go run' if the binary is not on PATH.
ifeq ($(OS),Windows_NT)
    MODELITH := $(shell where modelith 2>NUL)
else
    MODELITH := $(shell command -v modelith 2>/dev/null)
endif
MODELITH_CMD := $(if $(MODELITH),$(MODELITH),go run github.com/gosharplite/modelith/cmd/modelith@feat/self-domain-model)
# All canonical models in this repo — lint, render, and the CI drift gate cover
# every one of them so none can rot silently.
MODELITH_MODELS := docs/domain-model/tell-me-go.modelith.yaml \
                   docs/architect/environments/domain-model/environment-management.modelith.yaml \
                   docs/domain-model/quality.modelith.yaml
# The model with Go type correspondence — the only one the code<->model
# alignment gates (drift, layers) apply to. The process/ops models
# (environment-management, quality) model non-Go concepts and would produce
# false flags.
MODELITH_CODE_MODEL := docs/domain-model/tell-me-go.modelith.yaml

modelith-lint:
ifeq ($(IS_POSIX),true)
	@for m in $(MODELITH_MODELS); do echo "  modelith-lint $$m"; $(MODELITH_CMD) lint $$m || exit 1; done
else
	@for %%m in ($(MODELITH_MODELS)) do (echo modelith-lint %%m & $(MODELITH_CMD) lint %%m || exit /b 1)
endif

modelith-render:
ifeq ($(IS_POSIX),true)
	@for m in $(MODELITH_MODELS); do echo "  modelith-render $$m"; $(MODELITH_CMD) render $$m || exit 1; done
else
	@for %%m in ($(MODELITH_MODELS)) do (echo modelith-render %%m & $(MODELITH_CMD) render %%m || exit /b 1)
endif

modelith-check:
ifeq ($(IS_POSIX),true)
	@for m in $(MODELITH_MODELS); do echo "  modelith-check $$m"; $(MODELITH_CMD) render --check $$m || exit 1; done
else
	@for %%m in ($(MODELITH_MODELS)) do (echo modelith-check %%m & $(MODELITH_CMD) render --check %%m || exit /b 1)
endif

# modelith-drift scans the current diff for new exported Go identifiers
# that look like domain concepts but have no entry in the domain model.
# Advisory only — never fails the build. Run manually or in CI as a PR nudge.
# POSIX-only; skipped on Windows (requires grep/sed/git).
modelith-drift:
ifeq ($(IS_POSIX),true)
	@scripts/modelith-drift.sh $(MODELITH_CODE_MODEL) origin/main
else
	@echo "  modelith-drift: skipped (POSIX required — run in WSL or macOS/Linux CI)"
endif

# modelith-layers checks that modeled domain entities have their primary
# type definition in internal/domain/. Flags entities whose canonical
# definition is in application, infrastructure, or tools — indicating
# the model and code disagree about what belongs in the domain layer.
# Advisory only; POSIX-only.
modelith-layers:
ifeq ($(IS_POSIX),true)
	@scripts/modelith-layers.sh $(MODELITH_CODE_MODEL)
else
	@echo "  modelith-layers: skipped (POSIX required — run in WSL or macOS/Linux CI)"
endif

# modelith-vocab scans docs/ and README.md for non-canonical synonyms of
# domain model terms (e.g., "authorized paths" instead of SafePath).
# Advisory only; POSIX-only.
modelith-vocab:
ifeq ($(IS_POSIX),true)
	@scripts/modelith-vocab.sh
else
	@echo "  modelith-vocab: skipped (POSIX required — run in WSL or macOS/Linux CI)"
endif
# Compile-check fuzz tests without running any tests. This gates PRs by
# ensuring fuzz tests stay buildable.
# Uses -run=NONEXISTENT to skip all tests while still verifying compilation.
fuzz-smoke:
	go test -run=NONEXISTENT ./...

# check runs the full quality pipeline in sequence, stopping on first failure.
# Fast/cheap checks run first so problems surface quickly.
check: fmt tidy build
	@echo "=== lint ==="
	@$(MAKE) lint
	@echo "=== verify-architecture ==="
	@$(MAKE) verify-architecture
	@echo "=== verify-transitive-gate ==="
	@$(MAKE) verify-transitive-gate
	@echo "=== verify-exit-query ==="
	@$(MAKE) verify-exit-query
	@echo "=== verify-nonfix-catalog ==="
	@$(MAKE) verify-nonfix-catalog
	@echo "=== verify-ports-registry ==="
	@$(MAKE) verify-ports-registry
	@echo "=== verify-mock-pattern ==="
	@$(MAKE) verify-mock-pattern
	@echo "=== verify-session-provider-mock ==="
	@$(MAKE) verify-session-provider-mock
	@echo "=== verify-tools-adapter-import ==="
	@$(MAKE) verify-tools-adapter-import
	@echo "=== verify-tools-toolchain-import ==="
	@$(MAKE) verify-tools-toolchain-import
	@echo "=== verify-tools-infrastructure-import ==="
	@$(MAKE) verify-tools-infrastructure-import
	@echo "=== verify-mcp-sdk-confinement ==="
	@$(MAKE) verify-mcp-sdk-confinement
	@echo "=== verify-no-test-sleep ==="
	@$(MAKE) verify-no-test-sleep
	@echo "=== verify-adr-index ==="
	@$(MAKE) verify-adr-index
	@echo "=== verify-no-context-window-cache ==="
	@$(MAKE) verify-no-context-window-cache
	@echo "=== modelith-check ==="
	@$(MAKE) modelith-check
	@echo "=== fuzz-smoke ==="
	@$(MAKE) fuzz-smoke
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
	@echo "=== verify-transitive-gate ==="
	@$(MAKE) verify-transitive-gate
	@echo "=== verify-exit-query ==="
	@$(MAKE) verify-exit-query
	@echo "=== verify-nonfix-catalog ==="
	@$(MAKE) verify-nonfix-catalog
	@echo "=== verify-ports-registry ==="
	@$(MAKE) verify-ports-registry
	@echo "=== verify-mock-pattern ==="
	@$(MAKE) verify-mock-pattern
	@echo "=== verify-session-provider-mock ==="
	@$(MAKE) verify-session-provider-mock
	@echo "=== verify-tools-adapter-import ==="
	@$(MAKE) verify-tools-adapter-import
	@echo "=== verify-tools-toolchain-import ==="
	@$(MAKE) verify-tools-toolchain-import
	@echo "=== verify-tools-infrastructure-import ==="
	@$(MAKE) verify-tools-infrastructure-import
	@echo "=== verify-mcp-sdk-confinement ==="
	@$(MAKE) verify-mcp-sdk-confinement
	@echo "=== verify-no-test-sleep ==="
	@$(MAKE) verify-no-test-sleep
	@echo "=== verify-adr-index ==="
	@$(MAKE) verify-adr-index
	@echo "=== verify-no-context-window-cache ==="
	@$(MAKE) verify-no-context-window-cache
	@echo "=== modelith-check ==="
	@$(MAKE) modelith-check
	@echo "=== fuzz-smoke ==="
	@$(MAKE) fuzz-smoke
	@echo "=== vulncheck ==="
	@$(MAKE) vulncheck
	@echo "=== test ==="
	@$(MAKE) test
	@echo "=== dead-code ==="
	@$(MAKE) dead-code
	@echo "=== test-coverage ==="
	@$(MAKE) test-coverage
	@echo "=== test-race ==="
	@$(MAKE) test-race
	@echo ""
	@echo "All checks passed (including race detection)."

# Generate coverage report excluding mocks, generated files, and the
# agentinternal delegation bridge (ADR-022 / issue #138). Excludes the
# complete documented test-double set — all nine directories named by the
# coverage-exclusions-explicit invariant in
# docs/domain-model/quality.modelith.yaml.
.PHONY: test-coverage
test-coverage:
	go test -coverprofile=coverage.raw ./...
ifeq ($(IS_POSIX),true)
	@grep -v -E "(internal/agent/agenttest/|internal/agent/orchestrator/orchestratortest/|internal/domain/config/configtest/|internal/tools/analysis/analysistest/|internal/cli/clitest/|internal/domain/events/eventstest/|internal/infrastructure/persistence/persistencetest/|internal/tools/toolstest/|internal/infrastructure/testing/)" coverage.raw > coverage.out
else
	@findstr /V /R "internal/agent/agenttest/ internal/agent/orchestrator/orchestratortest/ internal/domain/config/configtest/ internal/tools/analysis/analysistest/ internal/cli/clitest/ internal/domain/events/eventstest/ internal/infrastructure/persistence/persistencetest/ internal/tools/toolstest/ internal/infrastructure/testing/" coverage.raw > coverage.out
endif
	go tool cover -func=coverage.out

# Verify every ADR file on disk is indexed in docs/adr/README.md
# and no ADR number is claimed by more than one file.
.PHONY: verify-adr-index
verify-adr-index:
ifeq ($(IS_POSIX),true)
	@echo "Checking ADR index consistency..."
	@errors=0; \
	for f in docs/adr/[0-9]*.md; do \
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
else
	@echo "Checking ADR index consistency..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$errors = 0; \
		$$indexContent = Get-Content 'docs/adr/README.md' -Raw; \
		Get-ChildItem 'docs/adr/[0-9]*.md' | ForEach-Object { \
			$$basename = $$_.Name; \
			if ($$indexContent -notmatch [regex]::Escape($$basename)) { \
				Write-Host \"MISSING from index: $$basename\"; \
				$$errors++ \
			} \
		}; \
		$$dupes = Get-ChildItem 'docs/adr/[0-9]*.md' | ForEach-Object { \
			Select-String -Path $$_.FullName -Pattern '^# ADR-(\d+):' | ForEach-Object { $$_.Matches.Groups[1].Value } \
		} | Group-Object | Where-Object { $$_.Count -gt 1 } | ForEach-Object { $$_.Name }; \
		if ($$dupes) { \
			Write-Host \"DUPLICATE ADR numbers: $$($$dupes -join ' ')\"; \
			$$errors++ \
		}; \
		if ($$errors -gt 0) { \
			Write-Host \"ADR index is inconsistent ($$errors errors).\"; \
			exit 1 \
		}; \
		Write-Host 'ADR index is consistent.' \
	"
endif

# Verify ADR-057: no context window cache references remain.
# The context window cache was removed in #1319
# (see docs/adr/2026-08-remove-context-window-cache.md). Six checks, each
# exit-1-on-match; cloneContentSlice survives only as a test helper.
.PHONY: verify-no-context-window-cache
verify-no-context-window-cache:
ifeq ($(IS_POSIX),true)
	@echo "Checking for context window cache references (ADR-057)..."
	@# Tier 1: die tokens repo-wide in .go files (ADR-057; .go-scoped per Flag-1 ruling)
	@VIOLATIONS="$$( grep -rnE 'cachedVersion|cachedWindow|cachedMetadata|tryCache|getCachedView|commitToCache|prewarmCache|versionBumpingTransformer' --include='*.go' --exclude-dir=.git . )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-057 violation: context window cache die tokens found in .go files."; \
		echo "   See: docs/adr/2026-08-remove-context-window-cache.md"; \
		echo ""; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		exit 1; \
	fi
	@# Tier 2: updateCache in session context
	@VIOLATIONS="$$( grep -rnE '\bupdateCache\b' internal/agent/session/context/ )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-057 violation: updateCache references found."; \
		echo "   See: docs/adr/2026-08-remove-context-window-cache.md"; \
		echo ""; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		exit 1; \
	fi
	@# Tier 2: ContextMetadata.clone in session context
	@VIOLATIONS="$$( grep -rn 'func (m *ContextMetadata) clone' internal/agent/session/context/ )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-057 violation: ContextMetadata.clone references found."; \
		echo "   See: docs/adr/2026-08-remove-context-window-cache.md"; \
		echo ""; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		exit 1; \
	fi
	@# Tier 2: cache_hit in orchestrator
	@VIOLATIONS="$$( grep -rnE '\bcache_hit\b' internal/agent/orchestrator/ )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-057 violation: cache_hit references found in orchestrator."; \
		echo "   See: docs/adr/2026-08-remove-context-window-cache.md"; \
		echo ""; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		exit 1; \
	fi
	@# Tier 2: 'persisted' in session context
	@VIOLATIONS="$$( grep -rnw 'persisted' internal/agent/session/context/ )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-057 violation: 'persisted' references found in session context."; \
		echo "   See: docs/adr/2026-08-remove-context-window-cache.md"; \
		echo ""; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		exit 1; \
	fi
	@# Survival: cloneContentSlice must not appear in production code
	@VIOLATIONS="$$( grep -rn 'cloneContentSlice' internal/agent/session/context/ --include='*.go' | grep -v '_test\.go' )"; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo ""; \
		echo "❌ ADR-057 violation: cloneContentSlice in production code."; \
		echo "   See: docs/adr/2026-08-remove-context-window-cache.md"; \
		echo ""; \
		echo "$$VIOLATIONS"; \
		echo ""; \
		exit 1; \
	fi
	@echo "  ✓ No context window cache references."
else
	@echo "Checking for context window cache references (ADR-057)..."
	@powershell -Command " \
		$$ErrorActionPreference = 'Stop'; \
		$$violations = @(); \
		$$t1 = Get-ChildItem -Path . -Recurse -Filter '*.go' | Where-Object { ($$_.FullName.Replace('\', '/')) -notmatch '\.git/' } | Select-String -Pattern 'cachedVersion|cachedWindow|cachedMetadata|tryCache|getCachedView|commitToCache|prewarmCache|versionBumpingTransformer'; \
		if ($$t1) { $$violations += ('Tier-1 die tokens in .go files:', ($$t1 | Out-String).Trim()) }; \
		$$t2 = Select-String -Path 'internal/agent/session/context/*.go' -Pattern '\bupdateCache\b'; \
		if ($$t2) { $$violations += ('updateCache in session context:', ($$t2 | Out-String).Trim()) }; \
		$$t3 = Select-String -Path 'internal/agent/session/context/*.go' -Pattern 'func \(m \*ContextMetadata\) clone'; \
		if ($$t3) { $$violations += ('ContextMetadata.clone in session context:', ($$t3 | Out-String).Trim()) }; \
		$$t4 = Select-String -Path 'internal/agent/orchestrator/*.go' -Pattern '\bcache_hit\b'; \
		if ($$t4) { $$violations += ('cache_hit in orchestrator:', ($$t4 | Out-String).Trim()) }; \
		$$t5 = Select-String -Path 'internal/agent/session/context/*.go' -Pattern '\bpersisted\b'; \
		if ($$t5) { $$violations += ('persisted in session context:', ($$t5 | Out-String).Trim()) }; \
		$$t6 = Get-ChildItem -Path 'internal/agent/session/context' -Filter '*.go' | Where-Object { $$_.Name -notlike '*_test.go' } | Select-String -Pattern 'cloneContentSlice'; \
		if ($$t6) { $$violations += ('cloneContentSlice in production code:', ($$t6 | Out-String).Trim()) }; \
		if ($$violations.Count -gt 0) { \
			Write-Host ''; \
			Write-Host '❌ ADR-057 violation: context window cache references found.'; \
			Write-Host '   See: docs/adr/2026-08-remove-context-window-cache.md'; \
			Write-Host ''; \
			$$violations | ForEach-Object { Write-Host $$_ }; \
			Write-Host ''; \
			exit 1 \
		}; \
		Write-Host '  ✓ No context window cache references.' \
	"
endif
